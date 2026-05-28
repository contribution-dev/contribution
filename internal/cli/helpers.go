package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/contribution-dev/contribution/internal/config"
	gitrepo "github.com/contribution-dev/contribution/internal/git"
	"github.com/contribution-dev/contribution/internal/github"
	"github.com/contribution-dev/contribution/internal/report"
	"github.com/contribution-dev/contribution/internal/scoring"
	"github.com/contribution-dev/contribution/internal/signals"
	"github.com/contribution-dev/contribution/internal/tools"
)

type analyzeOptions struct {
	repo            string
	since           string
	maxPRs          int
	githubToken     string
	output          string
	format          string
	publicSafe      bool
	noExternalTools bool
	verbose         bool
}

func runAnalyze(ctx context.Context, out io.Writer, opts analyzeOptions) (string, error) {
	start := time.Now().UTC()
	if opts.format == "" {
		opts.format = "all"
	}
	if err := validateFormat(opts.format, true); err != nil {
		return "", err
	}
	fmt.Fprintln(out, "Analyzing repo...")
	repo, err := gitrepo.Resolve(ctx, opts.repo)
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
	sinceDays, err := effectiveSinceDays(opts.since, cfg.Analysis.SinceDays)
	if err != nil {
		return "", err
	}
	maxPRs := cfg.Analysis.MaxPRs
	if opts.maxPRs > 0 {
		maxPRs = opts.maxPRs
	}
	outputRoot, err := analysisOutputRoot(opts.output, repo, cfg)
	if err != nil {
		return "", err
	}
	outputDir := filepath.Join(outputRoot, timestamp(start))

	tooling := tools.Discover(ctx, !opts.noExternalTools, start)
	fmt.Fprintln(out, "Git history: collecting")
	inventory, inventorySignals, err := gitrepo.Inventory(repo.Path, repo.ID, start)
	if err != nil {
		return "", err
	}
	history, historySignals, historyLimitations, err := gitrepo.CollectHistory(ctx, repo.Path, repo.ID, start.AddDate(0, 0, -sinceDays), maxPRs, start)
	if err != nil {
		return "", err
	}

	token, tokenAvailable := github.ResolveToken(opts.githubToken)
	metadata := github.Metadata{Reason: "GitHub metadata was not requested; continuing local-only."}
	if tokenAvailable && repo.GitHubOwner != "" && repo.GitHubRepo != "" {
		fmt.Fprintln(out, "GitHub metadata: requested")
		ghCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		var ghErr error
		metadata, ghErr = github.FetchMergedPRs(ghCtx, repo.GitHubOwner, repo.GitHubRepo, token, maxPRs)
		if ghErr != nil {
			metadata = github.Metadata{Reason: "GitHub metadata failed: " + ghErr.Error()}
		}
	} else {
		fmt.Fprintln(out, "GitHub metadata: unavailable, continuing local-only")
	}

	allSignals := append([]signals.Signal{}, inventorySignals...)
	allSignals = append(allSignals, historySignals...)
	allSignals = append(allSignals, tools.Signals(repo.ID, tooling, start)...)
	limitations := append([]string{}, cfgWarnings...)
	limitations = append(limitations, historyLimitations...)
	limitations = append(limitations, tooling.Limitations...)
	if metadata.Reason != "" {
		limitations = append(limitations, metadata.Reason)
	}
	limitations = append(limitations, "No coverage report was imported, so test conclusions use file-touch evidence only.")
	if !metadata.Available {
		limitations = append(limitations, "Review burden is unavailable without imported PR review metadata.")
	}

	score := scoring.Build(scoring.Input{
		Repo:        repo.Metadata(opts.publicSafe),
		History:     history,
		GitHub:      metadata,
		Inventory:   inventory,
		Signals:     allSignals,
		SinceDays:   sinceDays,
		MaxCards:    maxPRs,
		DisplayName: cfg.Project.Name,
		AITools:     cfg.AIUsage.SelfReportedTools,
		AIModes:     cfg.AIUsage.SelfReportedModes,
	})
	limitations = append(limitations, score.Limitations...)
	analysis := signals.AnalysisReport{
		Version:     1,
		GeneratedAt: start,
		Repo:        repo.Metadata(opts.publicSafe),
		Config: signals.AnalysisConfigSnapshot{
			SinceDays:                sinceDays,
			MaxPRs:                   maxPRs,
			IncludeUnmergedBranches:  cfg.Analysis.IncludeUnmergedBranches,
			PublicSafe:               opts.publicSafe,
			NoExternalTools:          opts.noExternalTools,
			SelfReportedAITools:      cfg.AIUsage.SelfReportedTools,
			SelfReportedAIModes:      cfg.AIUsage.SelfReportedModes,
			AllowManualAIPRTags:      cfg.AIUsage.AllowManualPRTags,
			OutputDirectory:          outputDir,
			GitHubMetadataConfigured: tokenAvailable,
		},
		Tooling:     tooling,
		Inventory:   inventory,
		Signals:     allSignals,
		PRCards:     score.Cards,
		WeaknessMap: score.WeaknessMap,
		Profile:     score.Profile,
		Limitations: uniqueStrings(limitations),
		Privacy: signals.PrivacySummary{
			PublicSafe:                         opts.publicSafe,
			RawCodeIncluded:                    false,
			RawDiffsIncluded:                   false,
			PrivatePathsIncludedInPublicExport: false,
			AuthorEmailsIncluded:               false,
			UploadEnabled:                      false,
		},
	}
	if analysis.Profile.Headline == "" {
		analysis.Profile.Headline = "AI-native contribution profile"
	}
	if err := report.WriteAnalysisBundle(outputDir, analysis, opts.format); err != nil {
		return "", err
	}
	fmt.Fprintf(out, "Report written to %s\n", filepath.Join(outputDir, "report.md"))
	return outputDir, nil
}

func currentRepo(ctx context.Context) (gitrepo.Repo, error) {
	return gitrepo.Resolve(ctx, ".")
}

func loadConfigBestEffort(repoPath string) (config.Config, []string, error) {
	cfg, warnings, err := config.Load(repoPath)
	if err != nil {
		return cfg, warnings, err
	}
	return cfg, warnings, nil
}

func validateFormat(format string, allowAll bool) error {
	switch format {
	case "json", "markdown":
		return nil
	case "all":
		if allowAll {
			return nil
		}
	}
	return fmt.Errorf("unsupported format %q", format)
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

func analysisOutputRoot(flag string, repo gitrepo.Repo, cfg config.Config) (string, error) {
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

func outputRootForCurrent(flag string, repo gitrepo.Repo, cfg config.Config) (string, error) {
	if flag != "" {
		return filepath.Abs(flag)
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

func latestAnalysisPath(root string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(root, "*", "analysis.json"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", errors.New("no analysis.json found; run contribution analyze first")
	}
	sort.Strings(matches)
	return matches[len(matches)-1], nil
}
