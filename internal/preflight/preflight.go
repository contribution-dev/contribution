// Package preflight evaluates current-diff review readiness.
package preflight

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/contribution-dev/contribution/internal/config"
	coveragepkg "github.com/contribution-dev/contribution/internal/coverage"
	"github.com/contribution-dev/contribution/internal/fileclass"
	gitrepo "github.com/contribution-dev/contribution/internal/git"
	"github.com/contribution-dev/contribution/internal/signals"
)

// BuildInput is the complete deterministic evidence for a preflight report.
type BuildInput struct {
	Repo             signals.RepoMetadata
	Base             string
	Head             string
	Diff             gitrepo.DiffSummary
	Coverage         signals.PreflightCoverage
	Policy           config.PreflightConfig
	Personal         signals.PersonalPreflightContext
	AnalyzerFindings []signals.AnalyzerFinding
	Tooling          signals.ToolingReport
	Limitations      []string
	Now              time.Time
}

// Build creates a V2 preflight report.
func Build(input BuildInput) signals.PreflightReport {
	summary := input.Diff.FileSummary
	changed := changedFiles(input.Diff.Files, input.Policy.RiskyPaths)
	summary.RiskyFiles = riskyFileCount(changed)
	analyzerFindings := nonNilAnalyzerFindings(input.AnalyzerFindings)
	totalLines := gitrepo.TotalChangedLines(input.Diff.Files)
	risk := "low"
	var why []string
	var focus []string
	var rubric []signals.PreflightRubricItem
	if summary.TotalFiles == 0 {
		risk = "unknown"
		why = append(why, "No changed files were found between base and head.")
	}
	sizeItem := sizeRubric(summary.TotalFiles, totalLines, input.Policy)
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
		if input.Policy.RequireTestsForSource {
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
	coverageItem := coverageRubric(input.Coverage, input.Policy)
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
	analyzerItem := analyzerRubric(analyzerFindings)
	rubric = append(rubric, analyzerItem)
	switch analyzerItem.Status {
	case "fail":
		risk = maxRisk(risk, "high")
		why = append(why, analyzerItem.Evidence)
		focus = append(focus, "static, secret, dependency, or vulnerability analyzer findings")
	case "warn":
		risk = maxRisk(risk, "medium")
		why = append(why, analyzerItem.Evidence)
		focus = append(focus, "static, secret, dependency, or vulnerability analyzer findings")
	}
	personalItems, personalWhy, personalFocus := personalRubric(changed, summary, totalLines, input.Personal)
	for _, item := range personalItems {
		rubric = append(rubric, item)
		if item.Status == "warn" || item.Status == "fail" {
			risk = maxRisk(risk, "medium")
		}
	}
	why = append(why, personalWhy...)
	focus = append(focus, personalFocus...)
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
	if input.Coverage.Status == "available" {
		testEvidence += fmt.Sprintf(" Changed-line coverage is %.1f%% (%d/%d executable changed lines).", input.Coverage.Percent, input.Coverage.CoveredLines, input.Coverage.TotalLines)
	} else if input.Coverage.Reason != "" {
		testEvidence += " " + input.Coverage.Reason
	}
	limitations := append(input.Limitations, "Optional analyzer findings depend on installed external tools and bounded scan time.")
	return signals.PreflightReport{
		Version:           2,
		GeneratedAt:       input.Now,
		Repo:              input.Repo,
		Base:              input.Base,
		Head:              input.Head,
		RiskLevel:         risk,
		Why:               uniqueStrings(why),
		ChangedFiles:      changed,
		FileSummary:       summary,
		TotalChangedLines: totalLines,
		Coverage:          input.Coverage,
		AnalyzerFindings:  analyzerFindings,
		Rubric:            rubric,
		TestEvidence:      testEvidence,
		Tooling:           input.Tooling,
		ReviewerFocus:     uniqueStrings(focus),
		PersonalContext:   nonEmptyPersonalContext(input.Personal),
		Limitations:       uniqueStrings(limitations),
		Privacy: signals.PrivacySummary{
			PublicSafe:       false,
			RawCodeIncluded:  false,
			RawDiffsIncluded: false,
		},
	}
}

// AnalyzerFindingsForChangedFiles keeps preflight focused on findings tied to the current diff.
func AnalyzerFindingsForChangedFiles(findings []signals.AnalyzerFinding, files []gitrepo.ChangedFile) ([]signals.AnalyzerFinding, int) {
	changed := map[string]bool{}
	for _, file := range files {
		path := normalizedRelPath(file.Path)
		if path != "" {
			changed[path] = true
		}
	}
	var out []signals.AnalyzerFinding
	var omitted int
	for _, finding := range findings {
		filePath := normalizedRelPath(finding.FilePath)
		if filePath != "" && !changed[filePath] {
			omitted++
			continue
		}
		item := finding
		item.FilePath = filePath
		if filePath != "" {
			item.Scope = "changed_file"
		} else if item.Scope == "" {
			item.Scope = "repo_existing_or_unknown"
		}
		out = append(out, item)
	}
	if out == nil {
		out = []signals.AnalyzerFinding{}
	}
	return out, omitted
}

// PersonalContextFromHistory summarizes recent local patterns for current-diff checks.
func PersonalContextFromHistory(history gitrepo.History) signals.PersonalPreflightContext {
	context := signals.PersonalPreflightContext{
		HighChurnFiles:    append([]string{}, history.HighChurnFiles...),
		ArtifactsAnalyzed: len(history.Commits),
	}
	var fileCounts []int
	var lineCounts []int
	for _, commit := range history.Commits {
		if commit.SourceTouched && !commit.TestsTouched {
			context.RecentSourceWithoutTests++
		}
		if len(commit.Files) == 0 {
			continue
		}
		fileCounts = append(fileCounts, len(commit.Files))
		lineCounts = append(lineCounts, gitrepo.TotalChangedLines(commit.Files))
	}
	context.TypicalFiles = median(fileCounts)
	context.TypicalLines = median(lineCounts)
	return context
}

// Coverage imports optional coverage reports and intersects them with changed lines.
func Coverage(paths []string, format string, repoPath string, files []gitrepo.ChangedFile) (signals.PreflightCoverage, error) {
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
	return coveragepkg.ComputeChangedLineCoverage(report, input), nil
}

func changedFiles(files []gitrepo.ChangedFile, riskyPatterns []string) []signals.PreflightChangedFile {
	out := make([]signals.PreflightChangedFile, 0, len(files))
	for _, file := range files {
		class := fileclass.ClassifyPath(file.Path)
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

func analyzerRubric(findings []signals.AnalyzerFinding) signals.PreflightRubricItem {
	item := signals.PreflightRubricItem{
		ID:       "analyzer_findings",
		Label:    "Analyzer findings",
		Status:   "unknown",
		Severity: "info",
		Evidence: "No optional analyzer finding was imported for changed files.",
	}
	if len(findings) == 0 {
		return item
	}
	counts := analyzerSeverityCounts(findings)
	item.Status = "warn"
	item.Severity = "medium"
	if counts["critical"]+counts["high"] > 0 {
		item.Status = "fail"
		item.Severity = "high"
	} else if counts["medium"] == 0 {
		item.Severity = "low"
	}
	item.Evidence = fmt.Sprintf("%d optional analyzer finding(s) matched changed files or repo-level checks%s.", len(findings), analyzerCountSummary(counts))
	item.Recommendation = "Triage analyzer findings before review; note false positives explicitly."
	return item
}

func analyzerSeverityCounts(findings []signals.AnalyzerFinding) map[string]int {
	counts := map[string]int{}
	for _, finding := range findings {
		severity := strings.TrimSpace(string(finding.Severity))
		if severity == "" {
			severity = "low"
		}
		counts[severity]++
	}
	return counts
}

func analyzerCountSummary(counts map[string]int) string {
	parts := []string{}
	for _, severity := range []string{"critical", "high", "medium", "low", "info"} {
		if counts[severity] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[severity], severity))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

func nonNilAnalyzerFindings(findings []signals.AnalyzerFinding) []signals.AnalyzerFinding {
	if findings == nil {
		return []signals.AnalyzerFinding{}
	}
	return findings
}

func personalRubric(files []signals.PreflightChangedFile, summary signals.FileSummary, totalLines int, personal signals.PersonalPreflightContext) ([]signals.PreflightRubricItem, []string, []string) {
	if personal.ArtifactsAnalyzed == 0 {
		return nil, nil, nil
	}
	var items []signals.PreflightRubricItem
	var why []string
	var focus []string
	if matches := changedHighChurnFiles(files, personal.HighChurnFiles); len(matches) > 0 {
		evidence := fmt.Sprintf("Diff touches recently high-churn file(s): %s.", strings.Join(matches, ", "))
		items = append(items, signals.PreflightRubricItem{
			ID:             "personal_high_churn",
			Label:          "Personal high-churn pattern",
			Status:         "warn",
			Severity:       "medium",
			Evidence:       evidence,
			Recommendation: "Add regression coverage and inspect recent edits before changing these files again.",
		})
		why = append(why, evidence)
		focus = append(focus, "regression risk in recently high-churn files")
	}
	if personal.RecentSourceWithoutTests > 0 && summary.SourceFiles > 0 && summary.TestFiles == 0 {
		evidence := fmt.Sprintf("This repeats a recent pattern: %d source-changing artifact(s) in the analysis window lacked test-file evidence.", personal.RecentSourceWithoutTests)
		items = append(items, signals.PreflightRubricItem{
			ID:             "personal_no_test_repeat",
			Label:          "Personal no-test pattern",
			Status:         "warn",
			Severity:       "medium",
			Evidence:       evidence,
			Recommendation: "Add nearby tests or explicitly document why this source-only change is safe.",
		})
		why = append(why, evidence)
		focus = append(focus, "avoiding another source-only change without test evidence")
	}
	if personal.ArtifactsAnalyzed >= 3 && exceedsPersonalScope(summary.TotalFiles, totalLines, personal) {
		evidence := fmt.Sprintf("Diff changes %d file(s) and %d line(s), above your recent typical scope of %d file(s) and %d line(s).", summary.TotalFiles, totalLines, personal.TypicalFiles, personal.TypicalLines)
		items = append(items, signals.PreflightRubricItem{
			ID:             "personal_scope",
			Label:          "Personal review scope",
			Status:         "warn",
			Severity:       "medium",
			Evidence:       evidence,
			Recommendation: "Split unrelated behavior or call out review boundaries before opening review.",
		})
		why = append(why, evidence)
		focus = append(focus, "scope compared with your recent reviewable changes")
	}
	return items, why, focus
}

func changedHighChurnFiles(files []signals.PreflightChangedFile, highChurn []string) []string {
	if len(files) == 0 || len(highChurn) == 0 {
		return nil
	}
	high := map[string]bool{}
	for _, file := range highChurn {
		high[file] = true
	}
	var matches []string
	for _, file := range files {
		if high[file.Path] {
			matches = append(matches, file.Path)
		}
	}
	return matches
}

func exceedsPersonalScope(files int, lines int, personal signals.PersonalPreflightContext) bool {
	fileThreshold := maxInt(8, personal.TypicalFiles*2)
	lineThreshold := maxInt(300, personal.TypicalLines*2)
	if personal.TypicalFiles == 0 {
		fileThreshold = 8
	}
	if personal.TypicalLines == 0 {
		lineThreshold = 300
	}
	return files > fileThreshold || lines > lineThreshold
}

func nonEmptyPersonalContext(personal signals.PersonalPreflightContext) *signals.PersonalPreflightContext {
	if personal.ArtifactsAnalyzed == 0 && len(personal.HighChurnFiles) == 0 && personal.RecentSourceWithoutTests == 0 && personal.TypicalFiles == 0 && personal.TypicalLines == 0 {
		return nil
	}
	return &personal
}

func normalizedRelPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = filepath.ToSlash(filepath.Clean(value))
	if value == "." || strings.HasPrefix(value, "../") || filepath.IsAbs(value) {
		return ""
	}
	return value
}

func median(values []int) int {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int{}, values...)
	sort.Ints(sorted)
	return sorted[len(sorted)/2]
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

// ValidateCoverageFormat checks the supported coverage format names.
func ValidateCoverageFormat(format string) error {
	return coveragepkg.ValidateFormat(format)
}

// ValidateFailOnRisk checks the supported fail-on-risk threshold names.
func ValidateFailOnRisk(value string) error {
	switch value {
	case "", "never", "medium", "high":
		return nil
	default:
		return fmt.Errorf("unsupported fail-on-risk %q", value)
	}
}

// ShouldFailForRisk reports whether risk meets a configured failure threshold.
func ShouldFailForRisk(risk string, threshold string) bool {
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

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func riskRank(value string) int {
	rank := map[string]int{"unknown": 0, "low": 1, "medium": 2, "high": 3}
	return rank[value]
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
