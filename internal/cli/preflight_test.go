package cli

import (
	"testing"
	"time"

	"github.com/contribution-dev/contribution/internal/config"
	gitrepo "github.com/contribution-dev/contribution/internal/git"
	"github.com/contribution-dev/contribution/internal/signals"
)

func TestBuildPreflightAppliesPolicyAndCoverage(t *testing.T) {
	diff := gitrepo.DiffSummary{
		Files: []gitrepo.ChangedFile{{
			Path:       "internal/auth/session.go",
			Additions:  10,
			Deletions:  2,
			LineRanges: []signals.LineRange{{Start: 10, End: 19}},
		}},
		FileSummary: signals.FileSummary{
			TotalFiles:  1,
			ByClass:     map[string]int{"source": 1},
			ByLanguage:  map[string]int{"Go": 1},
			SourceFiles: 1,
		},
	}

	got := buildPreflight(
		signals.RepoMetadata{ID: "local:test"},
		"main",
		"HEAD",
		diff,
		signals.PreflightCoverage{Status: "available", CoveredLines: 3, TotalLines: 10, Percent: 30},
		config.PreflightConfig{MaxFiles: 20, MaxLines: 800, RequireTestsForSource: true, ChangedLineCoverageMin: 80},
		signals.ToolingReport{},
		nil,
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	)

	if got.Version != 2 {
		t.Fatalf("Version = %d, want 2", got.Version)
	}
	if got.RiskLevel != "high" {
		t.Fatalf("RiskLevel = %q, want high", got.RiskLevel)
	}
	if got.TotalChangedLines != 12 {
		t.Fatalf("TotalChangedLines = %d, want 12", got.TotalChangedLines)
	}
	if len(got.ChangedFiles) != 1 || len(got.ChangedFiles[0].LineRanges) != 1 {
		t.Fatalf("ChangedFiles missing structured line ranges: %+v", got.ChangedFiles)
	}
	if !hasRubricStatus(got.Rubric, "test_evidence", "fail") {
		t.Fatalf("test rubric did not fail: %+v", got.Rubric)
	}
	if !hasRubricStatus(got.Rubric, "changed_line_coverage", "fail") {
		t.Fatalf("coverage rubric did not fail: %+v", got.Rubric)
	}
}

func TestShouldFailForRisk(t *testing.T) {
	if shouldFailForRisk("medium", "high") {
		t.Fatal("medium risk failed high threshold")
	}
	if !shouldFailForRisk("high", "medium") {
		t.Fatal("high risk did not fail medium threshold")
	}
	if shouldFailForRisk("high", "never") {
		t.Fatal("never threshold failed")
	}
}

func hasRubricStatus(items []signals.PreflightRubricItem, id string, status string) bool {
	for _, item := range items {
		if item.ID == id && item.Status == status {
			return true
		}
	}
	return false
}
