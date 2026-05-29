package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/contribution-dev/contribution/internal/config"
	coveragepkg "github.com/contribution-dev/contribution/internal/coverage"
	gitrepo "github.com/contribution-dev/contribution/internal/git"
	preflightpkg "github.com/contribution-dev/contribution/internal/preflight"
	"github.com/contribution-dev/contribution/internal/report"
	"github.com/contribution-dev/contribution/internal/signals"
	"github.com/contribution-dev/contribution/internal/tools"
	"github.com/spf13/cobra"
)

func newPreflightCommand(out io.Writer) *cobra.Command {
	var base string
	var head string
	var output string
	var format string
	var coveragePaths []string
	var coverageFormat string
	var failOnRisk string
	var worktree bool
	var noExternalTools bool
	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Analyze the current diff before review.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := report.ValidateFormat(format, true); err != nil {
				return err
			}
			if err := preflightpkg.ValidateCoverageFormat(coverageFormat); err != nil {
				return err
			}
			if err := preflightpkg.ValidateFailOnRisk(failOnRisk); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
			defer cancel()
			repo, err := currentRepo(ctx)
			if err != nil {
				return err
			}
			defer func() {
				_ = repo.Close()
			}()
			cfg, cfgWarnings, err := config.Load(repo.Path)
			if err != nil {
				return err
			}
			if base == "" {
				base = cfg.Project.DefaultBranch
			}
			if head == "" {
				head = "HEAD"
			}
			start := time.Now().UTC()
			var diff gitrepo.DiffSummary
			if worktree {
				diff, err = gitrepo.DiffWorktree(ctx, repo.Path, base)
				head = "WORKTREE"
			} else {
				diff, err = gitrepo.Diff(ctx, repo.Path, base, head)
			}
			if err != nil {
				return err
			}
			effectiveCoveragePaths, effectiveCoverageFormat, coverageInputLimitations := coveragepkg.ResolveInputs(coveragePaths, coverageFormat, repo.Path, cfg.Coverage.Path, cfg.Coverage.Format)
			coverage, err := preflightpkg.Coverage(effectiveCoveragePaths, effectiveCoverageFormat, repo.Path, diff.Files)
			if err != nil {
				return err
			}
			tooling := tools.Discover(ctx, !noExternalTools, start)
			limitations := append([]string{}, cfgWarnings...)
			limitations = append(limitations, coverageInputLimitations...)
			limitations = append(limitations, tooling.Limitations...)
			analyzerFindings := []signals.AnalyzerFinding{}
			if !noExternalTools {
				allAnalyzerFindings, _, analyzerLimitations := tools.RunAnalyzers(ctx, repo.Path, repo.ID, tooling, start)
				var omitted int
				analyzerFindings, omitted = preflightpkg.AnalyzerFindingsForChangedFiles(allAnalyzerFindings, diff.Files)
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
				personal = preflightpkg.PersonalContextFromHistory(history)
				limitations = append(limitations, historyLimitations...)
			}
			preflight := preflightpkg.BuildWithPersonalAndAnalyzers(repo.Metadata(false), base, head, diff, coverage, cfg.Preflight, personal, analyzerFindings, tooling, limitations, start)
			root, err := outputRootForCurrent(output, repo, cfg)
			if err != nil {
				return err
			}
			outputDir := filepath.Join(root, timestamp(start))
			if err := report.WritePreflight(outputDir, preflight, format); err != nil {
				return err
			}
			if preflightpkg.ShouldFailForRisk(preflight.RiskLevel, failOnRisk) {
				return fmt.Errorf("preflight risk %s meets --fail-on-risk=%s", preflight.RiskLevel, failOnRisk)
			}
			return writef(out, "Preflight written to %s\n", outputDir)
		},
	}
	cmd.Flags().StringVar(&base, "base", "", "Base branch or SHA.")
	cmd.Flags().StringVar(&head, "head", "HEAD", "Head branch or SHA.")
	cmd.Flags().StringVar(&output, "output", "", "Output directory root.")
	cmd.Flags().StringVar(&format, "format", "all", "Output format: json, markdown, or all.")
	cmd.Flags().StringArrayVar(&coveragePaths, "coverage", nil, "Coverage artifact path. May be repeated.")
	cmd.Flags().StringVar(&coverageFormat, "coverage-format", "auto", "Coverage format: auto, go, or lcov.")
	cmd.Flags().StringVar(&failOnRisk, "fail-on-risk", "never", "Exit nonzero for risk: never, medium, or high.")
	cmd.Flags().BoolVar(&worktree, "worktree", false, "Compare base against current tracked and untracked worktree changes.")
	cmd.Flags().BoolVar(&noExternalTools, "no-external-tools", false, "Skip optional external analyzer checks.")
	return cmd
}
