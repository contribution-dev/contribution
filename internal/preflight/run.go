package preflight

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/contribution-dev/contribution/internal/config"
	coveragepkg "github.com/contribution-dev/contribution/internal/coverage"
	gitrepo "github.com/contribution-dev/contribution/internal/git"
	"github.com/contribution-dev/contribution/internal/report"
	"github.com/contribution-dev/contribution/internal/signals"
	"github.com/contribution-dev/contribution/internal/tools"
)

// Options are the effective preflight command options.
type Options struct {
	Base            string
	Head            string
	Output          string
	Format          string
	CoveragePaths   []string
	CoverageFormat  string
	FailOnRisk      string
	Worktree        bool
	NoExternalTools bool
	RunCoverage     bool
}

// Run evaluates current-diff review readiness and writes preflight artifacts.
func Run(ctx context.Context, out io.Writer, opts Options) (string, error) {
	if opts.Format == "" {
		opts.Format = "all"
	}
	if opts.CoverageFormat == "" {
		opts.CoverageFormat = "auto"
	}
	if opts.FailOnRisk == "" {
		opts.FailOnRisk = "never"
	}
	if err := report.ValidateFormat(opts.Format, true); err != nil {
		return "", err
	}
	if err := ValidateCoverageFormat(opts.CoverageFormat); err != nil {
		return "", err
	}
	if err := ValidateFailOnRisk(opts.FailOnRisk); err != nil {
		return "", err
	}

	repo, err := gitrepo.Resolve(ctx, ".")
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
	base := opts.Base
	if base == "" {
		base = cfg.Project.DefaultBranch
	}
	head := opts.Head
	if head == "" {
		head = "HEAD"
	}
	start := time.Now().UTC()
	var diff gitrepo.DiffSummary
	if opts.Worktree {
		diff, err = gitrepo.DiffWorktree(ctx, repo.Path, base)
		head = "WORKTREE"
	} else {
		diff, err = gitrepo.Diff(ctx, repo.Path, base, head)
	}
	if err != nil {
		return "", err
	}
	if opts.RunCoverage {
		if cfg.Coverage.Command == "" {
			return "", fmt.Errorf("--run-coverage requires coverage.command in %s", config.FileName)
		}
		if err := coveragepkg.RunCommand(ctx, repo.Path, cfg.Coverage.Command); err != nil {
			return "", err
		}
	}

	effectiveCoveragePaths, effectiveCoverageFormat, coverageInputLimitations := coveragepkg.ResolveInputs(opts.CoveragePaths, opts.CoverageFormat, repo.Path, cfg.Coverage.Path, cfg.Coverage.Format)
	coverage, err := Coverage(effectiveCoveragePaths, effectiveCoverageFormat, repo.Path, diff.Files)
	if err != nil {
		return "", err
	}
	tooling := tools.DiscoverForRepo(ctx, !opts.NoExternalTools, start, repo.Path)
	limitations := append([]string{}, cfgWarnings...)
	limitations = append(limitations, coverageInputLimitations...)
	limitations = append(limitations, tooling.Limitations...)
	analyzerFindings := []signals.AnalyzerFinding{}
	if !opts.NoExternalTools {
		allAnalyzerFindings, _, analyzerLimitations := tools.RunAnalyzers(ctx, repo.Path, repo.ID, tooling, start)
		var omitted int
		analyzerFindings, omitted = AnalyzerFindingsForChangedFiles(allAnalyzerFindings, diff.Files)
		limitations = append(limitations, analyzerLimitations...)
		if omitted > 0 {
			limitations = append(limitations, fmt.Sprintf("%d optional analyzer finding(s) were omitted because they did not match changed files.", omitted))
		}
	}
	personal := signals.PersonalPreflightContext{}
	history, _, historyLimitations, historyErr := gitrepo.CollectHistory(ctx, repo.Path, repo.ID, start.AddDate(0, 0, -cfg.Analysis.SinceDays), cfg.Analysis.MaxPRs, start)
	if historyErr != nil {
		limitations = append(limitations, "Recent personal pattern checks were unavailable: "+historyErr.Error())
	} else {
		personal = PersonalContextFromHistory(history)
		limitations = append(limitations, historyLimitations...)
	}
	preflight := Build(BuildInput{
		Repo:             repo.Metadata(false),
		Base:             base,
		Head:             head,
		Diff:             diff,
		Coverage:         coverage,
		Policy:           cfg.Preflight,
		Personal:         personal,
		AnalyzerFindings: analyzerFindings,
		Tooling:          tooling,
		Limitations:      limitations,
		Now:              start,
	})
	root, err := outputRoot(opts.Output, repo, cfg)
	if err != nil {
		return "", err
	}
	outputDir := filepath.Join(root, timestamp(start))
	if err := report.WritePreflight(outputDir, preflight, opts.Format); err != nil {
		return "", err
	}
	if ShouldFailForRisk(preflight.RiskLevel, opts.FailOnRisk) {
		return outputDir, fmt.Errorf("preflight risk %s meets --fail-on-risk=%s", preflight.RiskLevel, opts.FailOnRisk)
	}
	if _, err := fmt.Fprintf(out, "Preflight written to %s\n", outputDir); err != nil {
		return "", err
	}
	return outputDir, nil
}

func outputRoot(flag string, repo gitrepo.Repo, cfg config.Config) (string, error) {
	if flag != "" {
		return filepath.Abs(flag)
	}
	return filepath.Abs(filepath.Join(repo.Path, cfg.Reports.OutputDir))
}

func timestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T150405Z")
}
