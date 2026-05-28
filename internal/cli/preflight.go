package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	gitrepo "github.com/contribution-dev/contribution/internal/git"
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
	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Analyze the current diff before review.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateFormat(format, true); err != nil {
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
			cfg, cfgWarnings, err := loadConfigBestEffort(repo.Path)
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
			diff, err := gitrepo.Diff(ctx, repo.Path, base, head)
			if err != nil {
				return err
			}
			tooling := tools.Discover(ctx, true, start)
			preflight := buildPreflight(repo.Metadata(false), base, head, diff, tooling, append(cfgWarnings, tooling.Limitations...), start)
			root, err := outputRootForCurrent(output, repo, cfg)
			if err != nil {
				return err
			}
			outputDir := filepath.Join(root, timestamp(start))
			if err := report.WritePreflight(outputDir, preflight, format); err != nil {
				return err
			}
			fmt.Fprintf(out, "Preflight written to %s\n", outputDir)
			return nil
		},
	}
	cmd.Flags().StringVar(&base, "base", "", "Base branch or SHA.")
	cmd.Flags().StringVar(&head, "head", "HEAD", "Head branch or SHA.")
	cmd.Flags().StringVar(&output, "output", "", "Output directory root.")
	cmd.Flags().StringVar(&format, "format", "all", "Output format: json, markdown, or all.")
	return cmd
}

func buildPreflight(repo signals.RepoMetadata, base string, head string, diff gitrepo.DiffSummary, tooling signals.ToolingReport, limitations []string, now time.Time) signals.PreflightReport {
	summary := diff.FileSummary
	risk := "low"
	var why []string
	var focus []string
	if summary.TotalFiles == 0 {
		risk = "unknown"
		why = append(why, "No changed files were found between base and head.")
	}
	if summary.TotalFiles > 20 {
		risk = maxRisk(risk, "high")
		why = append(why, fmt.Sprintf("Diff is large: %d files changed.", summary.TotalFiles))
		focus = append(focus, "split opportunities and reviewer load")
	} else if summary.TotalFiles > 8 {
		risk = maxRisk(risk, "medium")
		why = append(why, fmt.Sprintf("Diff is moderate: %d files changed.", summary.TotalFiles))
	}
	if summary.RiskyFiles > 0 {
		if summary.TestFiles == 0 {
			risk = maxRisk(risk, "high")
			why = append(why, "Security-sensitive paths changed without test-file evidence.")
		} else {
			risk = maxRisk(risk, "medium")
			why = append(why, "Security-sensitive paths changed.")
		}
		focus = append(focus, "authorization, billing, session, token, or permission edge cases")
	}
	if summary.SourceFiles > 0 && summary.TestFiles == 0 {
		risk = maxRisk(risk, "medium")
		why = append(why, "Source files changed but no test files changed.")
		focus = append(focus, "missing tests around changed behavior")
	}
	if summary.DependencyFiles > 0 {
		risk = maxRisk(risk, "medium")
		why = append(why, "Dependency or lock files changed.")
		focus = append(focus, "dependency and lockfile consistency")
	}
	if summary.GeneratedFiles+summary.VendorFiles > 0 {
		risk = maxRisk(risk, "medium")
		why = append(why, "Generated or vendor files changed.")
		focus = append(focus, "whether generated or vendor changes are intentional")
	}
	if len(why) == 0 {
		why = append(why, "Diff is small and no risky path pattern was detected.")
	}
	if len(focus) == 0 {
		focus = append(focus, "changed behavior and edge cases")
	}
	changed := make([]string, 0, len(diff.Files))
	for _, file := range diff.Files {
		changed = append(changed, file.Path)
	}
	testEvidence := "No test files changed."
	if summary.TestFiles > 0 {
		testEvidence = fmt.Sprintf("%d test files changed.", summary.TestFiles)
	}
	limitations = append(limitations, "Secret and static scan findings are limited to optional tool availability in V1 preflight.")
	return signals.PreflightReport{
		Version:       1,
		GeneratedAt:   now,
		Repo:          repo,
		Base:          base,
		Head:          head,
		RiskLevel:     risk,
		Why:           uniqueStrings(why),
		ChangedFiles:  changed,
		FileSummary:   summary,
		TestEvidence:  testEvidence,
		Tooling:       tooling,
		ReviewerFocus: uniqueStrings(focus),
		Limitations:   uniqueStrings(limitations),
		Privacy: signals.PrivacySummary{
			PublicSafe:       false,
			RawCodeIncluded:  false,
			RawDiffsIncluded: false,
			UploadEnabled:    false,
		},
	}
}

func maxRisk(current string, next string) string {
	rank := map[string]int{"unknown": 0, "low": 1, "medium": 2, "high": 3}
	if rank[next] > rank[current] {
		return next
	}
	return current
}
