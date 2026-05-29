// Package analysis assembles the local contribution analysis workflow.
package analysis

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/contribution-dev/contribution/internal/config"
	coveragepkg "github.com/contribution-dev/contribution/internal/coverage"
	gitrepo "github.com/contribution-dev/contribution/internal/git"
	"github.com/contribution-dev/contribution/internal/github"
	"github.com/contribution-dev/contribution/internal/report"
	"github.com/contribution-dev/contribution/internal/scoring"
	"github.com/contribution-dev/contribution/internal/signals"
	"github.com/contribution-dev/contribution/internal/tools"
)

var (
	nowUTC         = func() time.Time { return time.Now().UTC() }
	fetchMergedPRs = github.FetchMergedPRs
)

// Options are the effective analyze command options.
type Options struct {
	Repo            string
	Since           string
	MaxPRs          int
	GitHubToken     string
	Output          string
	Format          string
	PublicSafe      bool
	NoExternalTools bool
	CoveragePaths   []string
	CoverageFormat  string
}

// Run analyzes a repository and writes the configured report artifacts.
func Run(ctx context.Context, out io.Writer, opts Options) (string, error) {
	start := nowUTC()
	if opts.Format == "" {
		opts.Format = "all"
	}
	if err := report.ValidateFormat(opts.Format, true); err != nil {
		return "", err
	}
	if err := coveragepkg.ValidateFormat(opts.CoverageFormat); err != nil {
		return "", err
	}
	if err := writeLine(out, "Analyzing repo..."); err != nil {
		return "", err
	}
	repo, err := gitrepo.Resolve(ctx, opts.Repo)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = repo.Close()
	}()
	cfg, cfgWarnings, err := config.Load(repo.Path)
	if err != nil {
		return "", err
	}
	sinceDays, err := effectiveSinceDays(opts.Since, cfg.Analysis.SinceDays)
	if err != nil {
		return "", err
	}
	maxPRs := cfg.Analysis.MaxPRs
	if opts.MaxPRs > 0 {
		maxPRs = opts.MaxPRs
	}
	outputRoot, err := outputRoot(opts.Output, repo, cfg)
	if err != nil {
		return "", err
	}
	outputDir := filepath.Join(outputRoot, timestamp(start))

	tooling := tools.Discover(ctx, !opts.NoExternalTools, start)
	if err := writeLine(out, "Git history: collecting"); err != nil {
		return "", err
	}
	inventory, inventorySignals, err := gitrepo.Inventory(ctx, repo.Path, repo.ID, start)
	if err != nil {
		return "", err
	}
	currentWindowStart := start.AddDate(0, 0, -sinceDays)
	priorWindowStart := start.AddDate(0, 0, -sinceDays*2)
	priorWindowEnd := currentWindowStart
	history, historySignals, historyLimitations, err := gitrepo.CollectHistoryWindow(ctx, repo.Path, repo.ID, currentWindowStart, time.Time{}, maxPRs, start)
	if err != nil {
		return "", err
	}
	priorHistory, _, _, err := gitrepo.CollectHistoryWindow(ctx, repo.Path, repo.ID, priorWindowStart, priorWindowEnd, maxPRs, start)
	if err != nil {
		return "", err
	}
	analyzerFindings, analyzerSignals, analyzerLimitations := tools.RunAnalyzers(ctx, repo.Path, repo.ID, tooling, start)
	analyzerFindings = classifyAnalyzerFindingScopes(analyzerFindings, history)
	if analyzerFindings == nil {
		analyzerFindings = []signals.AnalyzerFinding{}
	}

	token, tokenAvailable := github.ResolveToken(opts.GitHubToken)
	metadata := github.Metadata{Reason: "GitHub metadata was not requested; continuing local-only."}
	if tokenAvailable && repo.GitHubOwner != "" && repo.GitHubRepo != "" {
		if err := writeLine(out, "GitHub metadata: requested"); err != nil {
			return "", err
		}
		ghCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		var ghErr error
		metadata, ghErr = fetchMergedPRs(ghCtx, repo.GitHubOwner, repo.GitHubRepo, token, maxPRs)
		if ghErr != nil {
			metadata = github.Metadata{Reason: "GitHub metadata failed: " + ghErr.Error()}
		}
	} else {
		if err := writeLine(out, "GitHub metadata: unavailable, continuing local-only"); err != nil {
			return "", err
		}
	}

	allSignals := append([]signals.Signal{}, inventorySignals...)
	allSignals = append(allSignals, historySignals...)
	allSignals = append(allSignals, tools.Signals(repo.ID, tooling, start)...)
	allSignals = append(allSignals, analyzerSignals...)
	coveragePaths, coverageFormat, coverageInputLimitations := coveragepkg.ResolveInputs(opts.CoveragePaths, opts.CoverageFormat, repo.Path, cfg.Coverage.Path, cfg.Coverage.Format)
	coverageSummary, coverageSignals, coverageLimitations, err := analyzeCoverage(coveragePaths, coverageFormat, repo.Path, repo.ID, start)
	if err != nil {
		return "", err
	}
	allSignals = append(allSignals, coverageSignals...)
	limitations := append([]string{}, cfgWarnings...)
	limitations = append(limitations, historyLimitations...)
	limitations = append(limitations, tooling.Limitations...)
	limitations = append(limitations, analyzerLimitations...)
	limitations = append(limitations, coverageInputLimitations...)
	limitations = append(limitations, coverageLimitations...)
	if metadata.Reason != "" {
		limitations = append(limitations, metadata.Reason)
	}
	limitations = append(limitations, metadata.Limitations...)
	if !metadata.Available {
		limitations = append(limitations, "Review burden is unavailable without imported PR review metadata.")
	}

	score := scoring.Build(scoring.Input{
		Repo:               repo.Metadata(opts.PublicSafe),
		History:            history,
		PriorHistory:       priorHistory,
		GitHub:             metadata,
		Inventory:          inventory,
		Coverage:           coverageSummary,
		AnalyzerFindings:   analyzerFindings,
		Signals:            allSignals,
		CurrentWindowStart: currentWindowStart,
		CurrentWindowEnd:   start,
		PriorWindowStart:   priorWindowStart,
		PriorWindowEnd:     priorWindowEnd,
		SinceDays:          sinceDays,
		MaxCards:           maxPRs,
		DisplayName:        cfg.Project.Name,
		AITools:            cfg.AIUsage.SelfReportedTools,
		AIModes:            cfg.AIUsage.SelfReportedModes,
	})
	limitations = append(limitations, score.Limitations...)
	analysis := signals.AnalysisReport{
		Version:     1,
		GeneratedAt: start,
		Repo:        repo.Metadata(opts.PublicSafe),
		Config: signals.AnalysisConfigSnapshot{
			SinceDays:                sinceDays,
			MaxPRs:                   maxPRs,
			PublicSafe:               opts.PublicSafe,
			NoExternalTools:          opts.NoExternalTools,
			SelfReportedAITools:      cfg.AIUsage.SelfReportedTools,
			SelfReportedAIModes:      cfg.AIUsage.SelfReportedModes,
			OutputDirectory:          outputDir,
			GitHubMetadataConfigured: tokenAvailable,
		},
		Tooling:          tooling,
		Inventory:        inventory,
		Coverage:         coverageSummary,
		AnalyzerFindings: analyzerFindings,
		Signals:          allSignals,
		PRCards:          score.Cards,
		WeaknessMap:      score.WeaknessMap,
		Trends:           score.Trends,
		DeepDives:        score.DeepDives,
		Profile:          score.Profile,
		SetupActions:     buildSetupActions(repo, cfgWarnings, metadata, coverageSummary, cfg.Coverage, tooling, tokenAvailable, !opts.NoExternalTools),
		Limitations:      uniqueStrings(limitations),
		Privacy: signals.PrivacySummary{
			PublicSafe:                         opts.PublicSafe,
			RawCodeIncluded:                    false,
			RawDiffsIncluded:                   false,
			PrivatePathsIncludedInPublicExport: false,
			AuthorEmailsIncluded:               false,
		},
	}
	if analysis.Profile.Headline == "" {
		analysis.Profile.Headline = "AI-native contribution profile"
	}
	if opts.PublicSafe {
		analysis = report.PublicSafeAnalysis(analysis)
	}
	if err := report.WriteAnalysisBundle(outputDir, analysis, opts.Format); err != nil {
		return "", err
	}
	switch opts.Format {
	case "json":
		if err := writef(out, "Analysis artifacts written to %s\n", filepath.Join(outputDir, "analysis.json")); err != nil {
			return "", err
		}
	case "markdown":
		if err := writef(out, "Report written to %s\n", filepath.Join(outputDir, "report.md")); err != nil {
			return "", err
		}
	default:
		if err := writef(out, "Report artifacts written to %s\n", outputDir); err != nil {
			return "", err
		}
	}
	return outputDir, nil
}

func analyzeCoverage(paths []string, format string, repoPath string, repoID string, createdAt time.Time) (signals.CoverageSummary, []signals.Signal, []string, error) {
	if len(paths) == 0 {
		summary := signals.CoverageSummary{Status: "unknown", Reason: "No coverage report was imported."}
		return summary, nil, []string{"No coverage report was imported, so test conclusions use file-touch evidence only."}, nil
	}
	report, err := coveragepkg.ParseFiles(paths, coveragepkg.Format(format), repoPath)
	if err != nil {
		return signals.CoverageSummary{}, nil, nil, err
	}
	summary := coveragepkg.Summarize(report)
	if summary.Status != "available" {
		return summary, nil, []string{summary.Reason}, nil
	}
	sig := signals.New(repoID, "coverage", "coverage_line_percent", "repo", repoID, signals.SeverityInfo, signals.DirectionPositive, signals.ConfidenceMedium, summary.Percent, "percent", fmt.Sprintf("Imported coverage covers %.1f%% of executable lines in provided reports.", summary.Percent), true, createdAt)
	return summary, []signals.Signal{sig}, nil, nil
}

func classifyAnalyzerFindingScopes(findings []signals.AnalyzerFinding, history gitrepo.History) []signals.AnalyzerFinding {
	if len(findings) == 0 {
		return findings
	}
	for i := range findings {
		if findings[i].FilePath != "" && history.FileTouchCount[findings[i].FilePath] > 0 {
			findings[i].Scope = "recently_touched"
		} else if findings[i].Scope == "" {
			findings[i].Scope = "repo_existing_or_unknown"
		}
	}
	return findings
}

func buildSetupActions(repo gitrepo.Repo, cfgWarnings []string, metadata github.Metadata, coverage signals.CoverageSummary, coverageConfig config.CoverageConfig, tooling signals.ToolingReport, tokenAvailable bool, allowExternalToolChecks bool) []signals.SetupAction {
	var actions []signals.SetupAction
	for _, warning := range cfgWarnings {
		if strings.Contains(warning, "No .contribution.yml") {
			actions = append(actions, signals.SetupAction{
				ID:               "add_config",
				Label:            "Add repo-local configuration",
				Command:          "contribution init",
				Why:              "A config file lets the report use your default branch, analysis window, preflight limits, risky paths, and self-reported AI workflow context.",
				ConfidenceImpact: "medium",
			})
			break
		}
	}
	if !tokenAvailable && repo.GitHubOwner != "" && repo.GitHubRepo != "" {
		command := "contribution analyze --repo . --github-token env:GITHUB_TOKEN --format all"
		if allowExternalToolChecks && github.GHTokenAvailable() {
			command = "contribution analyze --repo . --github-token gh --format all"
		}
		actions = append(actions, signals.SetupAction{
			ID:               "enable_github_metadata",
			Label:            "Enable PR review metadata",
			Command:          command,
			Why:              "GitHub metadata adds PR file lists, review burden, requested changes, and check-run evidence. That raises confidence beyond local commit heuristics.",
			ConfidenceImpact: "high",
		})
	} else if tokenAvailable && !metadata.Available && repo.GitHubOwner != "" && repo.GitHubRepo != "" {
		actions = append(actions, signals.SetupAction{
			ID:               "fix_github_metadata",
			Label:            "Fix GitHub metadata access",
			Command:          "contribution doctor",
			Why:              "A token was configured, but metadata was not available. Checking token scope and repo access would restore review-burden evidence.",
			ConfidenceImpact: "high",
		})
	}
	if coverage.Status != "available" {
		command := "go test ./... -coverprofile=coverage.out && contribution analyze --repo . --coverage coverage.out --coverage-format go --format all"
		if coverageConfig.Command != "" && coverageConfig.Path != "" {
			format := coverageConfig.Format
			if format == "" {
				format = "auto"
			}
			command = fmt.Sprintf("%s && contribution analyze --repo . --coverage %s --coverage-format %s --format all", coverageConfig.Command, coverageConfig.Path, format)
		}
		actions = append(actions, signals.SetupAction{
			ID:               "import_coverage",
			Label:            "Import coverage evidence",
			Command:          command,
			Why:              "Coverage import lets the report distinguish test-file presence from executable-line coverage. Reuse the same coverage artifact with preflight for changed-line coverage on behavior diffs.",
			ConfidenceImpact: "medium",
		})
	}
	if missingOptionalTools(tooling) > 0 {
		actions = append(actions, signals.SetupAction{
			ID:               "install_optional_tools",
			Label:            "Install optional analyzers",
			Command:          "contribution doctor",
			Why:              "Optional static, secret, dependency, and vulnerability tools add safety evidence without becoming hard requirements.",
			ConfidenceImpact: "medium",
		})
	}
	return actions
}

func missingOptionalTools(tooling signals.ToolingReport) int {
	var total int
	for _, tool := range tooling.Tools {
		if !tool.Required && !tool.Available {
			total++
		}
	}
	return total
}

func writeLine(out io.Writer, args ...any) error {
	_, err := fmt.Fprintln(out, args...)
	return err
}

func writef(out io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(out, format, args...)
	return err
}

func effectiveSinceDays(value string, fallback int) (int, error) {
	if fallback <= 0 {
		fallback = 90
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err != nil || days <= 0 {
			return 0, fmt.Errorf("invalid --since %q", value)
		}
		return days, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid --since %q", value)
	}
	days := int(duration.Hours() / 24)
	if days <= 0 {
		days = 1
	}
	return days, nil
}

func outputRoot(flag string, repo gitrepo.Repo, cfg config.Config) (string, error) {
	if flag != "" {
		return filepath.Abs(flag)
	}
	if repo.IsRemoteClone {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return filepath.Abs(filepath.Join(cwd, cfg.Reports.OutputDir))
	}
	return filepath.Abs(filepath.Join(repo.Path, cfg.Reports.OutputDir))
}

func timestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T150405Z")
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
