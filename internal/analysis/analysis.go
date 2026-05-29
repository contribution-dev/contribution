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
	gitrepo "github.com/contribution-dev/contribution/internal/git"
	"github.com/contribution-dev/contribution/internal/github"
	"github.com/contribution-dev/contribution/internal/report"
	"github.com/contribution-dev/contribution/internal/scoring"
	"github.com/contribution-dev/contribution/internal/signals"
	"github.com/contribution-dev/contribution/internal/tools"
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
}

// Run analyzes a repository and writes the configured report artifacts.
func Run(ctx context.Context, out io.Writer, opts Options) (string, error) {
	start := time.Now().UTC()
	if opts.Format == "" {
		opts.Format = "all"
	}
	if err := report.ValidateFormat(opts.Format, true); err != nil {
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
	history, historySignals, historyLimitations, err := gitrepo.CollectHistory(ctx, repo.Path, repo.ID, start.AddDate(0, 0, -sinceDays), maxPRs, start)
	if err != nil {
		return "", err
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
		metadata, ghErr = github.FetchMergedPRs(ghCtx, repo.GitHubOwner, repo.GitHubRepo, token, maxPRs)
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
		Repo:        repo.Metadata(opts.PublicSafe),
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
		Tooling:     tooling,
		Inventory:   inventory,
		Signals:     allSignals,
		PRCards:     score.Cards,
		WeaknessMap: score.WeaknessMap,
		Profile:     score.Profile,
		Limitations: uniqueStrings(limitations),
		Privacy: signals.PrivacySummary{
			PublicSafe:                         opts.PublicSafe,
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
