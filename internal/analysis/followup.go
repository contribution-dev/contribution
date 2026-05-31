package analysis

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"

	"github.com/contribution-dev/contribution/internal/report"
	"github.com/contribution-dev/contribution/internal/signals"
)

type priorAnalysisResult struct {
	analysis signals.AnalysisReport
	found    bool
	err      error
}

type followUpMetricRule struct {
	id             string
	improvedLabel  string
	regressedLabel string
	higherIsBetter bool
	threshold      float64
}

var followUpMetricRules = []followUpMetricRule{
	{id: "source_test_evidence_rate", improvedLabel: "Test evidence improved", regressedLabel: "Test evidence slipped", higherIsBetter: true, threshold: 10},
	{id: "large_change_rate", improvedLabel: "Large-change rate came down", regressedLabel: "Large changes increased", threshold: 10},
	{id: "fix_like_rate", improvedLabel: "Fix/revert-like churn came down", regressedLabel: "Fix/revert-like churn increased", threshold: 10},
	{id: "risky_without_tests_count", improvedLabel: "Risky untested changes came down", regressedLabel: "Risky untested changes increased", threshold: 1},
	{id: "high_churn_files", improvedLabel: "Churn concentration came down", regressedLabel: "Churn concentration increased", threshold: 1},
}

func latestPriorAnalysis(outputRoot string, outputDir string) priorAnalysisResult {
	matches, err := filepath.Glob(filepath.Join(outputRoot, "*", "analysis.json"))
	if err != nil {
		return priorAnalysisResult{err: err}
	}
	sort.Strings(matches)
	var readErr error
	for i := len(matches) - 1; i >= 0; i-- {
		if filepath.Clean(filepath.Dir(matches[i])) == filepath.Clean(outputDir) {
			continue
		}
		analysis, err := report.ReadAnalysis(matches[i])
		if err != nil {
			readErr = err
			continue
		}
		return priorAnalysisResult{analysis: analysis, found: true}
	}
	return priorAnalysisResult{err: readErr}
}

func buildFollowUpComparison(current signals.AnalysisReport, previous signals.AnalysisReport, previousFound bool, previousErr error) signals.FollowUpComparison {
	comparison := signals.FollowUpComparison{
		CurrentGeneratedAt: current.GeneratedAt,
		Improved:           []signals.Finding{},
		Regressed:          []signals.Finding{},
		Resolved:           []signals.Finding{},
		Persistent:         []signals.Finding{},
		Confidence:         signals.ConfidenceLow,
	}
	if previousErr != nil && !previousFound {
		comparison.Status = "unavailable"
		comparison.Reason = "The latest prior local report could not be read, so this run cannot compare against it."
		comparison.Summary = comparison.Reason
		comparison.NextAction = "Keep this report and run analysis again after the next meaningful changes."
		return comparison
	}
	if !previousFound {
		comparison.Status = "baseline"
		comparison.Reason = "No previous local analysis report was found under this output root."
		comparison.Summary = "No previous local report found; this run establishes the comparison baseline."
		comparison.NextAction = "Run `contribution analyze --repo . --format all` again after the next meaningful changes."
		return comparison
	}
	comparison.Status = "available"
	comparison.PreviousGeneratedAt = previous.GeneratedAt
	comparison.Confidence = followUpConfidence(current, previous)
	comparison.Improved, comparison.Regressed = compareFollowUpMetrics(current.Trends.Metrics, previous.Trends.Metrics, comparison.Confidence)
	comparison.Resolved, comparison.Persistent = compareWeaknessLabels(current.WeaknessMap.Weaknesses, previous.WeaknessMap.Weaknesses)
	comparison.Summary = followUpSummary(comparison)
	comparison.NextAction = followUpNextAction(comparison, current)
	return comparison
}

func compareFollowUpMetrics(currentMetrics []signals.TrendMetric, previousMetrics []signals.TrendMetric, confidence signals.Confidence) ([]signals.Finding, []signals.Finding) {
	currentByID := metricsByID(currentMetrics)
	previousByID := metricsByID(previousMetrics)
	var improved []signals.Finding
	var regressed []signals.Finding
	for _, rule := range followUpMetricRules {
		current, ok := currentByID[rule.id]
		if !ok {
			continue
		}
		previous, ok := previousByID[rule.id]
		if !ok {
			continue
		}
		delta := current.CurrentValue - previous.CurrentValue
		if math.Abs(delta) < rule.threshold {
			continue
		}
		finding := signals.Finding{
			Label:        rule.improvedLabel,
			Evidence:     followUpMetricEvidence(current, previous),
			Confidence:   confidence,
			WhyItMatters: current.WhyItMatters,
			NextAction:   current.NextAction,
		}
		improvedDirection := delta > 0 && rule.higherIsBetter || delta < 0 && !rule.higherIsBetter
		if improvedDirection {
			improved = append(improved, finding)
		} else {
			finding.Label = rule.regressedLabel
			regressed = append(regressed, finding)
			if len(regressed) >= 3 {
				continue
			}
		}
	}
	return firstFindings(improved, 3), firstFindings(regressed, 3)
}

func metricsByID(metrics []signals.TrendMetric) map[string]signals.TrendMetric {
	out := make(map[string]signals.TrendMetric, len(metrics))
	for _, metric := range metrics {
		if metric.ID != "" {
			out[metric.ID] = metric
		}
	}
	return out
}

func followUpMetricEvidence(current signals.TrendMetric, previous signals.TrendMetric) string {
	return fmt.Sprintf("%s changed from %s in the last report to %s now.", current.Label, formatFollowUpMetricValue(previous.CurrentValue, previous.Unit), formatFollowUpMetricValue(current.CurrentValue, current.Unit))
}

func formatFollowUpMetricValue(value float64, unit string) string {
	if unit == "percent" {
		return fmt.Sprintf("%.1f%%", value)
	}
	return fmt.Sprintf("%.0f", value)
}

func compareWeaknessLabels(current []signals.Finding, previous []signals.Finding) ([]signals.Finding, []signals.Finding) {
	currentByLabel := findingsByLabel(current)
	var resolved []signals.Finding
	var persistent []signals.Finding
	for _, prior := range previous {
		label := strings.TrimSpace(prior.Label)
		if label == "" {
			continue
		}
		if now, ok := currentByLabel[label]; ok {
			persistent = append(persistent, signals.Finding{
				Label:        label,
				Evidence:     "This weakness appears in both the previous and current reports.",
				Confidence:   lowerConfidence(now.Confidence, prior.Confidence),
				WhyItMatters: now.WhyItMatters,
				NextAction:   now.NextAction,
			})
			continue
		}
		resolved = append(resolved, signals.Finding{
			Label:      label,
			Evidence:   "This weakness appeared in the previous report but is not present in the current top weakness map.",
			Confidence: signals.ConfidenceLow,
		})
	}
	return firstFindings(resolved, 3), firstFindings(persistent, 3)
}

func findingsByLabel(findings []signals.Finding) map[string]signals.Finding {
	out := make(map[string]signals.Finding, len(findings))
	for _, finding := range findings {
		label := strings.TrimSpace(finding.Label)
		if label != "" {
			out[label] = finding
		}
	}
	return out
}

func followUpSummary(comparison signals.FollowUpComparison) string {
	if comparison.Status != "available" {
		if comparison.Summary != "" {
			return comparison.Summary
		}
		return comparison.Reason
	}
	var parts []string
	if len(comparison.Improved) > 0 {
		parts = append(parts, fmt.Sprintf("%d improved", len(comparison.Improved)))
	}
	if len(comparison.Regressed) > 0 {
		parts = append(parts, fmt.Sprintf("%d got worse", len(comparison.Regressed)))
	}
	if len(comparison.Resolved) > 0 {
		parts = append(parts, fmt.Sprintf("%d resolved", len(comparison.Resolved)))
	}
	if len(comparison.Persistent) > 0 {
		parts = append(parts, fmt.Sprintf("%d still true", len(comparison.Persistent)))
	}
	if len(parts) == 0 {
		return "Since the last report, tracked quality metrics stayed broadly steady."
	}
	return "Since the last report, " + strings.Join(parts, ", ") + "."
}

func followUpNextAction(comparison signals.FollowUpComparison, current signals.AnalysisReport) string {
	for _, finding := range comparison.Regressed {
		if finding.NextAction != "" {
			return finding.NextAction
		}
	}
	for _, finding := range comparison.Persistent {
		if finding.NextAction != "" {
			return finding.NextAction
		}
	}
	if len(comparison.Improved) > 0 {
		return "Keep the improved pattern and use preflight to avoid regressing on the next behavior change."
	}
	if len(current.WeaknessMap.NextActions) > 0 {
		return current.WeaknessMap.NextActions[0]
	}
	return "Run preflight before the next meaningful change to create a tighter improvement loop."
}

func followUpConfidence(current signals.AnalysisReport, previous signals.AnalysisReport) signals.Confidence {
	return lowerConfidence(current.Profile.Confidence, previous.Profile.Confidence)
}

func lowerConfidence(a signals.Confidence, b signals.Confidence) signals.Confidence {
	if confidenceRank(a) < confidenceRank(b) {
		return a
	}
	return b
}

func confidenceRank(confidence signals.Confidence) int {
	switch confidence {
	case signals.ConfidenceHigh:
		return 3
	case signals.ConfidenceMedium:
		return 2
	default:
		return 1
	}
}

func firstFindings(values []signals.Finding, limit int) []signals.Finding {
	if len(values) < limit {
		limit = len(values)
	}
	return append([]signals.Finding{}, values[:limit]...)
}
