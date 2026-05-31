package analysis

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/contribution-dev/contribution/internal/report"
	"github.com/contribution-dev/contribution/internal/signals"
)

func TestBuildFollowUpComparisonComparesPriorReport(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	previous := signals.AnalysisReport{
		GeneratedAt: now.AddDate(0, 0, -7),
		Profile:     signals.ProfileSummary{Confidence: signals.ConfidenceMedium},
		Trends: signals.TrendComparison{Metrics: []signals.TrendMetric{
			followUpMetric("source_test_evidence_rate", "Source changes with test evidence", 25, "percent", "Pair source changes with nearby tests."),
			followUpMetric("large_change_rate", "Large change rate", 35, "percent", "Split broad refactors from behavior changes."),
			followUpMetric("fix_like_rate", "Fix/revert-like churn rate", 0, "percent", "Inspect repeat fix patterns."),
			followUpMetric("high_churn_files", "High-churn file concentration", 3, "count", "Inspect recent touches."),
		}},
		WeaknessMap: signals.WeaknessMap{Weaknesses: []signals.Finding{
			{Label: "Large changes create review risk", Confidence: signals.ConfidenceMedium},
			{Label: "No-test pattern", Confidence: signals.ConfidenceMedium},
		}},
	}
	current := signals.AnalysisReport{
		GeneratedAt: now,
		Profile:     signals.ProfileSummary{Confidence: signals.ConfidenceMedium},
		Trends: signals.TrendComparison{Metrics: []signals.TrendMetric{
			followUpMetric("source_test_evidence_rate", "Source changes with test evidence", 80, "percent", "Pair source changes with nearby tests."),
			followUpMetric("large_change_rate", "Large change rate", 5, "percent", "Split broad refactors from behavior changes."),
			followUpMetric("fix_like_rate", "Fix/revert-like churn rate", 20, "percent", "Inspect repeat fix patterns."),
			followUpMetric("high_churn_files", "High-churn file concentration", 0, "count", "Inspect recent touches."),
		}},
		WeaknessMap: signals.WeaknessMap{Weaknesses: []signals.Finding{{
			Label:      "No-test pattern",
			Confidence: signals.ConfidenceMedium,
			NextAction: "Add nearby regression tests.",
		}}},
	}

	got := buildFollowUpComparison(current, previous, true, nil)
	if got.Status != "available" {
		t.Fatalf("Status = %q, want available", got.Status)
	}
	if !hasFollowUpFinding(got.Improved, "Test evidence improved") || !hasFollowUpFinding(got.Improved, "Large-change rate came down") {
		t.Fatalf("Improved = %+v, want test evidence and large-change improvements", got.Improved)
	}
	if !hasFollowUpFinding(got.Regressed, "Fix/revert-like churn increased") {
		t.Fatalf("Regressed = %+v, want fix-like churn regression", got.Regressed)
	}
	if !hasFollowUpFinding(got.Resolved, "Large changes create review risk") {
		t.Fatalf("Resolved = %+v, want resolved large-change weakness", got.Resolved)
	}
	if !hasFollowUpFinding(got.Persistent, "No-test pattern") {
		t.Fatalf("Persistent = %+v, want persistent no-test weakness", got.Persistent)
	}
	if got.NextAction != "Inspect repeat fix patterns." {
		t.Fatalf("NextAction = %q, want regression next action", got.NextAction)
	}
}

func TestLatestPriorAnalysisFindsNewestReadableReport(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "2026-01-01T000000Z")
	newDir := filepath.Join(root, "2026-01-02T000000Z")
	currentDir := filepath.Join(root, "2026-01-03T000000Z")
	if err := report.WriteAnalysisBundle(oldDir, signals.AnalysisReport{GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, "json"); err != nil {
		t.Fatalf("write old analysis: %v", err)
	}
	if err := report.WriteAnalysisBundle(newDir, signals.AnalysisReport{GeneratedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}, "json"); err != nil {
		t.Fatalf("write new analysis: %v", err)
	}
	if err := report.WriteAnalysisBundle(currentDir, signals.AnalysisReport{GeneratedAt: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)}, "json"); err != nil {
		t.Fatalf("write current analysis: %v", err)
	}

	got := latestPriorAnalysis(root, currentDir)
	if !got.found {
		t.Fatalf("found = false, err = %v", got.err)
	}
	if !got.analysis.GeneratedAt.Equal(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("GeneratedAt = %s, want newest prior report", got.analysis.GeneratedAt)
	}
}

func followUpMetric(id string, label string, currentValue float64, unit string, next string) signals.TrendMetric {
	return signals.TrendMetric{
		ID:           id,
		Label:        label,
		CurrentValue: currentValue,
		Unit:         unit,
		WhyItMatters: label + " matters.",
		NextAction:   next,
	}
}

func hasFollowUpFinding(findings []signals.Finding, label string) bool {
	for _, finding := range findings {
		if finding.Label == label {
			return true
		}
	}
	return false
}
