package cli

import (
	"context"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/contribution-dev/contribution/internal/config"
	coveragepkg "github.com/contribution-dev/contribution/internal/coverage"
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
	var coveragePaths []string
	var coverageFormat string
	var failOnRisk string
	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Analyze the current diff before review.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateFormat(format, true); err != nil {
				return err
			}
			if err := validateCoverageFormat(coverageFormat); err != nil {
				return err
			}
			if err := validateFailOnRisk(failOnRisk); err != nil {
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
			coverage, err := preflightCoverage(coveragePaths, coverageFormat, repo.Path, diff.Files)
			if err != nil {
				return err
			}
			tooling := tools.Discover(ctx, true, start)
			preflight := buildPreflight(repo.Metadata(false), base, head, diff, coverage, cfg.Preflight, tooling, append(cfgWarnings, tooling.Limitations...), start)
			root, err := outputRootForCurrent(output, repo, cfg)
			if err != nil {
				return err
			}
			outputDir := filepath.Join(root, timestamp(start))
			if err := report.WritePreflight(outputDir, preflight, format); err != nil {
				return err
			}
			if shouldFailForRisk(preflight.RiskLevel, failOnRisk) {
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
	return cmd
}

func buildPreflight(repo signals.RepoMetadata, base string, head string, diff gitrepo.DiffSummary, coverage signals.PreflightCoverage, policy config.PreflightConfig, tooling signals.ToolingReport, limitations []string, now time.Time) signals.PreflightReport {
	summary := diff.FileSummary
	changed := preflightChangedFiles(diff.Files, policy.RiskyPaths)
	summary.RiskyFiles = riskyFileCount(changed)
	totalLines := changedLineCount(diff.Files)
	risk := "low"
	var why []string
	var focus []string
	var rubric []signals.PreflightRubricItem
	if summary.TotalFiles == 0 {
		risk = "unknown"
		why = append(why, "No changed files were found between base and head.")
	}
	sizeItem := sizeRubric(summary.TotalFiles, totalLines, policy)
	rubric = append(rubric, sizeItem)
	switch sizeItem.Status {
	case "fail":
		risk = maxRisk(risk, "high")
		why = append(why, sizeItem.Evidence)
		focus = append(focus, "split opportunities and reviewer load")
	case "warn":
		risk = maxRisk(risk, "medium")
		why = append(why, sizeItem.Evidence)
	}
	if summary.RiskyFiles > 0 {
		status := "warn"
		severity := "medium"
		if summary.TestFiles == 0 {
			status = "fail"
			severity = "high"
			risk = maxRisk(risk, "high")
			why = append(why, "Security-sensitive paths changed without test-file evidence.")
		} else {
			risk = maxRisk(risk, "medium")
			why = append(why, "Security-sensitive paths changed.")
		}
		focus = append(focus, "authorization, billing, session, token, or permission edge cases")
		rubric = append(rubric, signals.PreflightRubricItem{
			ID:             "risky_paths",
			Label:          "Risky paths",
			Status:         status,
			Severity:       severity,
			Evidence:       fmt.Sprintf("%d risky path(s) changed.", summary.RiskyFiles),
			Recommendation: "Review authorization, billing, session, token, and permission edge cases.",
		})
	} else {
		rubric = append(rubric, signals.PreflightRubricItem{ID: "risky_paths", Label: "Risky paths", Status: "pass", Severity: "info", Evidence: "No risky path pattern was detected."})
	}
	switch {
	case summary.SourceFiles > 0 && summary.TestFiles == 0:
		status := "warn"
		severity := "medium"
		if policy.RequireTestsForSource {
			status = "fail"
			severity = "high"
			risk = maxRisk(risk, "high")
		} else {
			risk = maxRisk(risk, "medium")
		}
		why = append(why, "Source files changed but no test files changed.")
		focus = append(focus, "missing tests around changed behavior")
		rubric = append(rubric, signals.PreflightRubricItem{
			ID:             "test_evidence",
			Label:          "Test evidence",
			Status:         status,
			Severity:       severity,
			Evidence:       "Source files changed but no test files changed.",
			Recommendation: "Add nearby regression tests or document why tests are not practical.",
		})
	case summary.SourceFiles > 0:
		rubric = append(rubric, signals.PreflightRubricItem{
			ID:       "test_evidence",
			Label:    "Test evidence",
			Status:   "pass",
			Severity: "info",
			Evidence: fmt.Sprintf("%d test file(s) changed with %d source file(s).", summary.TestFiles, summary.SourceFiles),
		})
	default:
		rubric = append(rubric, signals.PreflightRubricItem{ID: "test_evidence", Label: "Test evidence", Status: "unknown", Severity: "info", Evidence: "No source files changed."})
	}
	if summary.DependencyFiles > 0 {
		risk = maxRisk(risk, "medium")
		why = append(why, "Dependency or lock files changed.")
		focus = append(focus, "dependency and lockfile consistency")
		rubric = append(rubric, signals.PreflightRubricItem{
			ID:             "dependency_changes",
			Label:          "Dependency changes",
			Status:         "warn",
			Severity:       "medium",
			Evidence:       fmt.Sprintf("%d dependency file(s) changed.", summary.DependencyFiles),
			Recommendation: "Check dependency and lockfile consistency.",
		})
	}
	if summary.GeneratedFiles+summary.VendorFiles > 0 {
		risk = maxRisk(risk, "medium")
		why = append(why, "Generated or vendor files changed.")
		focus = append(focus, "whether generated or vendor changes are intentional")
		rubric = append(rubric, signals.PreflightRubricItem{
			ID:             "generated_vendor",
			Label:          "Generated or vendor files",
			Status:         "warn",
			Severity:       "medium",
			Evidence:       fmt.Sprintf("%d generated/vendor file(s) changed.", summary.GeneratedFiles+summary.VendorFiles),
			Recommendation: "Confirm generated or vendor changes are intentional.",
		})
	}
	coverageItem := coverageRubric(coverage, policy)
	rubric = append(rubric, coverageItem)
	switch coverageItem.Status {
	case "fail":
		risk = maxRisk(risk, "high")
		why = append(why, coverageItem.Evidence)
		focus = append(focus, "changed-line coverage for touched behavior")
	case "warn":
		risk = maxRisk(risk, "medium")
		why = append(why, coverageItem.Evidence)
	}
	if len(why) == 0 {
		why = append(why, "Diff is small and no risky path pattern was detected.")
	}
	if len(focus) == 0 {
		focus = append(focus, "changed behavior and edge cases")
	}
	testEvidence := "No test files changed."
	if summary.TestFiles > 0 {
		testEvidence = fmt.Sprintf("%d test files changed.", summary.TestFiles)
	}
	if coverage.Status == "available" {
		testEvidence += fmt.Sprintf(" Changed-line coverage is %.1f%% (%d/%d executable changed lines).", coverage.Percent, coverage.CoveredLines, coverage.TotalLines)
	} else if coverage.Reason != "" {
		testEvidence += " " + coverage.Reason
	}
	limitations = append(limitations, "Secret and static scan findings are limited to optional tool availability in preflight.")
	return signals.PreflightReport{
		Version:           2,
		GeneratedAt:       now,
		Repo:              repo,
		Base:              base,
		Head:              head,
		RiskLevel:         risk,
		Why:               uniqueStrings(why),
		ChangedFiles:      changed,
		FileSummary:       summary,
		TotalChangedLines: totalLines,
		Coverage:          coverage,
		Rubric:            rubric,
		TestEvidence:      testEvidence,
		Tooling:           tooling,
		ReviewerFocus:     uniqueStrings(focus),
		Limitations:       uniqueStrings(limitations),
		Privacy: signals.PrivacySummary{
			PublicSafe:       false,
			RawCodeIncluded:  false,
			RawDiffsIncluded: false,
			UploadEnabled:    false,
		},
	}
}

func preflightCoverage(paths []string, format string, repoPath string, files []gitrepo.ChangedFile) (signals.PreflightCoverage, error) {
	if len(paths) == 0 {
		return signals.PreflightCoverage{Status: "unknown", Reason: "No coverage report was imported."}, nil
	}
	report, err := coveragepkg.ParseFiles(paths, coveragepkg.Format(format), repoPath)
	if err != nil {
		return signals.PreflightCoverage{}, err
	}
	input := make([]coveragepkg.ChangedFileInput, 0, len(files))
	for _, file := range files {
		input = append(input, coveragepkg.ChangedFileInput{Path: file.Path, LineRanges: file.LineRanges})
	}
	coverage := coveragepkg.ComputeChangedLineCoverage(report, input)
	return signals.PreflightCoverage{
		Status:       coverage.Status,
		CoveredLines: coverage.CoveredLines,
		TotalLines:   coverage.TotalLines,
		Percent:      coverage.Percent,
		Files:        preflightCoverageFiles(coverage.Files),
		Sources:      coverage.Sources,
		Reason:       coverage.Reason,
	}, nil
}

func preflightCoverageFiles(files []coveragepkg.ChangedFileCoverage) []signals.PreflightFileCoverage {
	out := make([]signals.PreflightFileCoverage, 0, len(files))
	for _, file := range files {
		out = append(out, signals.PreflightFileCoverage{
			Path:         file.Path,
			CoveredLines: file.CoveredLines,
			TotalLines:   file.TotalLines,
			Percent:      file.Percent,
		})
	}
	return out
}

func preflightChangedFiles(files []gitrepo.ChangedFile, riskyPatterns []string) []signals.PreflightChangedFile {
	out := make([]signals.PreflightChangedFile, 0, len(files))
	for _, file := range files {
		class := gitrepo.ClassifyPath(file.Path)
		out = append(out, signals.PreflightChangedFile{
			Path:       file.Path,
			Additions:  file.Additions,
			Deletions:  file.Deletions,
			LineRanges: file.LineRanges,
			Class:      class.Class,
			Language:   class.Language,
			Risky:      class.IsSecurityRelated || matchesAnyPathPattern(file.Path, riskyPatterns),
		})
	}
	return out
}

func riskyFileCount(files []signals.PreflightChangedFile) int {
	var total int
	for _, file := range files {
		if file.Risky {
			total++
		}
	}
	return total
}

func changedLineCount(files []gitrepo.ChangedFile) int {
	var total int
	for _, file := range files {
		total += file.Additions + file.Deletions
	}
	return total
}

func sizeRubric(files int, lines int, policy config.PreflightConfig) signals.PreflightRubricItem {
	maxFiles := policy.MaxFiles
	maxLines := policy.MaxLines
	if files == 0 {
		return signals.PreflightRubricItem{ID: "review_scope", Label: "Review scope", Status: "unknown", Severity: "info", Evidence: "No changed files were found."}
	}
	item := signals.PreflightRubricItem{
		ID:       "review_scope",
		Label:    "Review scope",
		Status:   "pass",
		Severity: "info",
		Evidence: fmt.Sprintf("Diff changes %d file(s) and %d line(s).", files, lines),
	}
	if (maxFiles > 0 && files > maxFiles) || (maxLines > 0 && lines > maxLines) {
		item.Status = "fail"
		item.Severity = "high"
		item.Evidence = fmt.Sprintf("Diff changes %d file(s) and %d line(s), exceeding configured limits of %d file(s) and %d line(s).", files, lines, maxFiles, maxLines)
		item.Recommendation = "Split this change before review if possible."
		return item
	}
	if files > 8 || lines > 300 {
		item.Status = "warn"
		item.Severity = "medium"
		item.Evidence = fmt.Sprintf("Diff is moderate: %d file(s) and %d line(s) changed.", files, lines)
		item.Recommendation = "Keep reviewer focus tight and call out behavior boundaries."
	}
	return item
}

func coverageRubric(coverage signals.PreflightCoverage, policy config.PreflightConfig) signals.PreflightRubricItem {
	minimum := policy.ChangedLineCoverageMin
	if minimum < 0 || minimum > 100 {
		minimum = 0
	}
	item := signals.PreflightRubricItem{
		ID:       "changed_line_coverage",
		Label:    "Changed-line coverage",
		Status:   "unknown",
		Severity: "info",
		Evidence: "Changed-line coverage is unknown.",
	}
	if coverage.Status != "available" {
		if coverage.Reason != "" {
			item.Evidence = coverage.Reason
		}
		if minimum > 0 {
			item.Status = "warn"
			item.Severity = "medium"
			item.Recommendation = "Import coverage or lower the repository coverage policy."
		}
		return item
	}
	item.Status = "pass"
	item.Evidence = fmt.Sprintf("Changed-line coverage is %.1f%% (%d/%d executable changed lines).", coverage.Percent, coverage.CoveredLines, coverage.TotalLines)
	if minimum > 0 && coverage.Percent < minimum {
		item.Status = "fail"
		item.Severity = "high"
		item.Evidence = fmt.Sprintf("Changed-line coverage is %.1f%%, below configured minimum %.1f%%.", coverage.Percent, minimum)
		item.Recommendation = "Add tests for uncovered changed executable lines."
	}
	return item
}

func matchesAnyPathPattern(filePath string, patterns []string) bool {
	filePath = filepath.ToSlash(filepath.Clean(filePath))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(filepath.ToSlash(pattern))
		if pattern == "" {
			continue
		}
		if strings.HasSuffix(pattern, "/") && strings.HasPrefix(filePath, pattern) {
			return true
		}
		if strings.ContainsAny(pattern, "*?[") {
			if ok, err := path.Match(pattern, filePath); err == nil && ok {
				return true
			}
			continue
		}
		if filePath == pattern || strings.HasPrefix(filePath, strings.TrimSuffix(pattern, "/")+"/") {
			return true
		}
	}
	return false
}

func validateCoverageFormat(format string) error {
	switch format {
	case "", "auto", "go", "lcov":
		return nil
	default:
		return fmt.Errorf("unsupported coverage format %q", format)
	}
}

func validateFailOnRisk(value string) error {
	switch value {
	case "", "never", "medium", "high":
		return nil
	default:
		return fmt.Errorf("unsupported fail-on-risk %q", value)
	}
}

func shouldFailForRisk(risk string, threshold string) bool {
	if threshold == "" || threshold == "never" {
		return false
	}
	return riskRank(risk) >= riskRank(threshold)
}

func maxRisk(current string, next string) string {
	if riskRank(next) > riskRank(current) {
		return next
	}
	return current
}

func riskRank(value string) int {
	rank := map[string]int{"unknown": 0, "low": 1, "medium": 2, "high": 3}
	return rank[value]
}
