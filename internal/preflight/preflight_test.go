package preflight

import (
	"encoding/json"
	"reflect"
	"sort"
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

	got := Build(
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

func TestBuildPreflightUsesPersonalPatterns(t *testing.T) {
	diff := gitrepo.DiffSummary{
		Files: []gitrepo.ChangedFile{{
			Path:      "internal/report/report.go",
			Additions: 50,
			Deletions: 10,
		}},
		FileSummary: signals.FileSummary{
			TotalFiles:  1,
			ByClass:     map[string]int{"source": 1},
			ByLanguage:  map[string]int{"Go": 1},
			SourceFiles: 1,
		},
	}

	got := BuildWithPersonal(
		signals.RepoMetadata{ID: "local:test"},
		"main",
		"HEAD",
		diff,
		signals.PreflightCoverage{Status: "unknown", Reason: "No coverage report was imported."},
		config.PreflightConfig{MaxFiles: 20, MaxLines: 800},
		signals.PersonalPreflightContext{
			HighChurnFiles:           []string{"internal/report/report.go"},
			RecentSourceWithoutTests: 3,
			TypicalFiles:             2,
			TypicalLines:             50,
			ArtifactsAnalyzed:        8,
		},
		signals.ToolingReport{},
		nil,
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	)

	if got.PersonalContext == nil {
		t.Fatal("PersonalContext = nil, want populated context")
	}
	if got.RiskLevel != "medium" {
		t.Fatalf("RiskLevel = %q, want medium", got.RiskLevel)
	}
	for _, id := range []string{"personal_high_churn", "personal_no_test_repeat"} {
		if !hasRubricStatus(got.Rubric, id, "warn") {
			t.Fatalf("missing personal rubric %q: %+v", id, got.Rubric)
		}
	}
}

func TestPersonalContextFromHistory(t *testing.T) {
	history := gitrepo.History{
		Commits: []gitrepo.Commit{{
			SHA:           "a",
			Files:         []gitrepo.ChangedFile{{Path: "internal/app.go", Additions: 10}},
			SourceTouched: true,
		}, {
			SHA:           "b",
			Files:         []gitrepo.ChangedFile{{Path: "internal/app.go", Additions: 2}, {Path: "internal/app_test.go", Additions: 3}},
			SourceTouched: true,
			TestsTouched:  true,
		}},
		HighChurnFiles: []string{"internal/app.go"},
	}

	got := PersonalContextFromHistory(history)
	if got.ArtifactsAnalyzed != 2 || got.RecentSourceWithoutTests != 1 || got.TypicalFiles != 2 || got.TypicalLines != 10 {
		t.Fatalf("PersonalContextFromHistory() = %+v", got)
	}
}

func TestPreflightReportJSONContract(t *testing.T) {
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
	got := Build(
		signals.RepoMetadata{ID: "local:test", Name: "test"},
		"main",
		"HEAD",
		diff,
		signals.PreflightCoverage{Status: "available", CoveredLines: 3, TotalLines: 10, Percent: 30},
		config.PreflightConfig{MaxFiles: 20, MaxLines: 800, RequireTestsForSource: true, ChangedLineCoverageMin: 80},
		signals.ToolingReport{},
		nil,
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	)
	object := marshalPreflightContractObject(t, got)
	assertPreflightContractKeys(t, object, []string{
		"version",
		"generated_at",
		"repo",
		"base",
		"head",
		"risk_level",
		"why",
		"changed_files",
		"file_summary",
		"total_changed_lines",
		"coverage",
		"rubric",
		"test_evidence",
		"tooling",
		"reviewer_focus",
		"limitations",
		"privacy",
	})
	assertPreflightContractKeys(t, preflightContractNestedObject(t, object, "privacy"), []string{
		"public_safe",
		"raw_code_included",
		"raw_diffs_included",
		"private_paths_included_in_public_export",
		"author_emails_included",
	})
}

func TestShouldFailForRisk(t *testing.T) {
	if ShouldFailForRisk("medium", "high") {
		t.Fatal("medium risk failed high threshold")
	}
	if !ShouldFailForRisk("high", "medium") {
		t.Fatal("high risk did not fail medium threshold")
	}
	if ShouldFailForRisk("high", "never") {
		t.Fatal("never threshold failed")
	}
}

func TestBuildPreflightPolicyMatrix(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		files      []gitrepo.ChangedFile
		summary    signals.FileSummary
		coverage   signals.PreflightCoverage
		policy     config.PreflightConfig
		wantRisk   string
		wantRubric map[string]string
	}{
		{
			name:     "source with tests passes test evidence",
			files:    []gitrepo.ChangedFile{{Path: "internal/app.go", Additions: 3}, {Path: "internal/app_test.go", Additions: 5}},
			summary:  signals.FileSummary{TotalFiles: 2, SourceFiles: 1, TestFiles: 1, ByClass: map[string]int{}, ByLanguage: map[string]int{}},
			coverage: signals.PreflightCoverage{Status: "unknown", Reason: "No coverage report was imported."},
			policy:   config.PreflightConfig{MaxFiles: 20, MaxLines: 800},
			wantRisk: "low",
			wantRubric: map[string]string{
				"test_evidence": "pass",
			},
		},
		{
			name:     "source without tests warns by default",
			files:    []gitrepo.ChangedFile{{Path: "internal/app.go", Additions: 3}},
			summary:  signals.FileSummary{TotalFiles: 1, SourceFiles: 1, ByClass: map[string]int{}, ByLanguage: map[string]int{}},
			coverage: signals.PreflightCoverage{Status: "unknown", Reason: "No coverage report was imported."},
			policy:   config.PreflightConfig{MaxFiles: 20, MaxLines: 800},
			wantRisk: "medium",
			wantRubric: map[string]string{
				"test_evidence": "warn",
			},
		},
		{
			name:     "source without tests fails when required",
			files:    []gitrepo.ChangedFile{{Path: "internal/app.go", Additions: 3}},
			summary:  signals.FileSummary{TotalFiles: 1, SourceFiles: 1, ByClass: map[string]int{}, ByLanguage: map[string]int{}},
			coverage: signals.PreflightCoverage{Status: "unknown", Reason: "No coverage report was imported."},
			policy:   config.PreflightConfig{MaxFiles: 20, MaxLines: 800, RequireTestsForSource: true},
			wantRisk: "high",
			wantRubric: map[string]string{
				"test_evidence": "fail",
			},
		},
		{
			name:     "custom risky path without tests fails",
			files:    []gitrepo.ChangedFile{{Path: "internal/safe/config.go", Additions: 3}},
			summary:  signals.FileSummary{TotalFiles: 1, SourceFiles: 1, ByClass: map[string]int{}, ByLanguage: map[string]int{}},
			coverage: signals.PreflightCoverage{Status: "unknown", Reason: "No coverage report was imported."},
			policy:   config.PreflightConfig{MaxFiles: 20, MaxLines: 800, RiskyPaths: []string{"internal/safe/"}},
			wantRisk: "high",
			wantRubric: map[string]string{
				"risky_paths": "fail",
			},
		},
		{
			name:     "missing coverage warns when threshold configured",
			files:    []gitrepo.ChangedFile{{Path: "internal/app.go", Additions: 3}, {Path: "internal/app_test.go", Additions: 5}},
			summary:  signals.FileSummary{TotalFiles: 2, SourceFiles: 1, TestFiles: 1, ByClass: map[string]int{}, ByLanguage: map[string]int{}},
			coverage: signals.PreflightCoverage{Status: "unknown", Reason: "No coverage report was imported."},
			policy:   config.PreflightConfig{MaxFiles: 20, MaxLines: 800, ChangedLineCoverageMin: 80},
			wantRisk: "medium",
			wantRubric: map[string]string{
				"changed_line_coverage": "warn",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Build(
				signals.RepoMetadata{ID: "local:test"},
				"main",
				"HEAD",
				gitrepo.DiffSummary{Files: tt.files, FileSummary: tt.summary},
				tt.coverage,
				tt.policy,
				signals.ToolingReport{},
				nil,
				now,
			)
			if got.RiskLevel != tt.wantRisk {
				t.Fatalf("RiskLevel = %q, want %q\nreport = %+v", got.RiskLevel, tt.wantRisk, got)
			}
			for id, status := range tt.wantRubric {
				if !hasRubricStatus(got.Rubric, id, status) {
					t.Fatalf("rubric %q did not have status %q: %+v", id, status, got.Rubric)
				}
			}
		})
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

func marshalPreflightContractObject(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return object
}

func preflightContractNestedObject(t *testing.T, object map[string]any, key string) map[string]any {
	t.Helper()
	nested, ok := object[key].(map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want object", key, object[key])
	}
	return nested
}

func assertPreflightContractKeys(t *testing.T, object map[string]any, want []string) {
	t.Helper()
	got := preflightContractSortedKeys(object)
	want = append([]string{}, want...)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON keys = %v, want %v", got, want)
	}
}

func preflightContractSortedKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
