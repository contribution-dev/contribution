package report

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/contribution-dev/contribution/internal/signals"
)

func TestAnalysisReportJSONContract(t *testing.T) {
	object := marshalReportContractObject(t, reportContractAnalysisFixture())
	assertReportContractKeys(t, object, []string{
		"version",
		"generated_at",
		"repo",
		"config",
		"tooling",
		"inventory",
		"coverage",
		"analyzer_findings",
		"signals",
		"pr_quality_cards",
		"weakness_map",
		"trends",
		"follow_up",
		"deep_dives",
		"profile",
		"setup_actions",
		"limitations",
		"privacy",
	})
	assertReportContractKeys(t, reportContractNestedObject(t, object, "privacy"), reportPrivacyContractKeys())
	assertReportContractKeys(t, reportContractNestedObject(t, object, "follow_up"), []string{
		"status",
		"previous_generated_at",
		"current_generated_at",
		"summary",
		"improved",
		"regressed",
		"resolved",
		"persistent",
		"next_action",
		"confidence",
	})
}

func TestProfileExportJSONContract(t *testing.T) {
	object := marshalReportContractObject(t, ProfileExport(reportContractAnalysisFixture()))
	assertReportContractKeys(t, object, []string{
		"version",
		"generated_at",
		"profile",
		"summary",
		"strengths",
		"improvement_trends",
		"badge_candidates",
		"selected_artifacts",
		"redaction",
	})
	assertReportContractKeys(t, reportContractNestedObject(t, object, "profile"), []string{
		"display_name",
		"headline",
		"visibility",
	})
	assertReportContractKeys(t, reportContractNestedObject(t, object, "summary"), []string{
		"analyzed_prs",
		"analysis_window_days",
		"confidence",
	})
	assertReportContractKeys(t, reportContractNestedObject(t, object, "redaction"), []string{
		"public_safe",
		"raw_code_included",
		"raw_diffs_included",
		"private_paths_included",
	})
}

func TestShareCardJSONContract(t *testing.T) {
	object := marshalReportContractObject(t, ShareCard(reportContractAnalysisFixture()))
	assertReportContractKeys(t, object, []string{
		"version",
		"title",
		"subtitle",
		"highlights",
		"confidence",
		"public_safe",
	})
}

func reportContractAnalysisFixture() signals.AnalysisReport {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return signals.AnalysisReport{
		Version:     1,
		GeneratedAt: now,
		Repo: signals.RepoMetadata{
			ID:            "local:test",
			Name:          "test",
			DefaultBranch: "main",
		},
		Config: signals.AnalysisConfigSnapshot{
			SinceDays:       90,
			MaxPRs:          3,
			PublicSafe:      true,
			OutputDirectory: "/tmp/contribution-test",
		},
		Tooling: signals.ToolingReport{
			GeneratedAt: now,
			Tools:       []signals.ToolAvailability{{Name: "git", Required: true, Available: true}},
			Limitations: []string{"GitHub metadata unavailable."},
		},
		Inventory: signals.FileSummary{
			TotalFiles:  1,
			ByClass:     map[string]int{"source": 1},
			ByLanguage:  map[string]int{"Go": 1},
			SourceFiles: 1,
		},
		Coverage: signals.CoverageSummary{
			Status: "unknown",
			Reason: "No coverage report was imported.",
		},
		AnalyzerFindings: []signals.AnalyzerFinding{{
			Tool:       "semgrep",
			RuleID:     "go.example",
			Severity:   signals.SeverityMedium,
			FilePath:   "internal/app.go",
			Scope:      "recently_touched",
			Message:    "Example static finding.",
			Confidence: signals.ConfidenceMedium,
		}},
		Signals: []signals.Signal{{
			ID:          "sig-1",
			RepoID:      "local:test",
			Source:      "git",
			Type:        "change_scope",
			SubjectType: "commit",
			Severity:    signals.SeverityInfo,
			Direction:   signals.DirectionPositive,
			Confidence:  signals.ConfidenceMedium,
			Message:     "Small tested change.",
			Evidence:    signals.Evidence{ToolVersion: "test"},
			PublicSafe:  true,
			CreatedAt:   now,
		}},
		PRCards: []signals.PRQualityCard{{
			PRNumber:     123,
			Title:        "Improve contract tests",
			Label:        "strong",
			Confidence:   signals.ConfidenceMedium,
			Summary:      "Small tested change.",
			Scope:        "1 file and 10 lines",
			TestEvidence: "Go tests cover the behavior.",
			ReviewBurden: "Low",
			Durability:   "Durable",
			MainRisk:     "Low",
			Strengths:    []signals.Finding{{Label: "Focused", Evidence: "Single-purpose change.", Confidence: signals.ConfidenceMedium}},
			Risks:        []signals.Finding{{Label: "Small risk", Evidence: "Contract drift.", Confidence: signals.ConfidenceLow}},
			Evidence:     []signals.SignalRef{{ID: "sig-1", Message: "Small tested change."}},
			NextAction:   "Keep contracts covered.",
		}},
		WeaknessMap: signals.WeaknessMap{
			Strengths:   []signals.Finding{{Label: "Focused", Evidence: "Single-purpose change.", Confidence: signals.ConfidenceMedium}},
			Weaknesses:  []signals.Finding{{Label: "Coverage gap", Evidence: "Contracts need tests.", Confidence: signals.ConfidenceLow}},
			WatchItems:  []signals.Finding{{Label: "Schema drift", Evidence: "Artifacts are consumed elsewhere.", Confidence: signals.ConfidenceMedium}},
			NextActions: []string{"Keep contracts covered."},
			Confidence:  signals.ConfidenceMedium,
		},
		Trends: signals.TrendComparison{
			Status: "available",
			CurrentWindow: signals.TrendWindow{
				Label:                     "recent",
				Since:                     now.AddDate(0, 0, -90),
				Until:                     now,
				Commits:                   2,
				SourceCommits:             1,
				TestTouchedCommits:        1,
				SourceWithoutTestsCommits: 0,
			},
			PriorWindow: signals.TrendWindow{
				Label:                     "prior",
				Since:                     now.AddDate(0, 0, -180),
				Until:                     now.AddDate(0, 0, -90),
				Commits:                   2,
				SourceCommits:             1,
				TestTouchedCommits:        0,
				SourceWithoutTestsCommits: 1,
			},
			Metrics: []signals.TrendMetric{{
				ID:           "source_test_evidence_rate",
				Label:        "Source changes with test evidence",
				CurrentValue: 100,
				PriorValue:   0,
				Delta:        100,
				Unit:         "percent",
				Direction:    "improved",
				Evidence:     "Source commits with test-file evidence improved.",
				Confidence:   signals.ConfidenceMedium,
			}},
			Findings:   []signals.Finding{{Label: "Test evidence improved", Evidence: "Source commits with test-file evidence improved.", Confidence: signals.ConfidenceMedium}},
			Confidence: signals.ConfidenceMedium,
		},
		FollowUp: signals.FollowUpComparison{
			Status:              "available",
			PreviousGeneratedAt: now.AddDate(0, 0, -7),
			CurrentGeneratedAt:  now,
			Summary:             "Since the last report, 1 improved.",
			Improved:            []signals.Finding{{Label: "Test evidence improved", Evidence: "Source changes with test evidence improved.", Confidence: signals.ConfidenceMedium}},
			Regressed:           []signals.Finding{},
			Resolved:            []signals.Finding{},
			Persistent:          []signals.Finding{},
			NextAction:          "Keep pairing source changes with nearby tests.",
			Confidence:          signals.ConfidenceMedium,
		},
		DeepDives: signals.AnalysisDeepDives{
			HighChurn:       []signals.HighChurnDeepDive{},
			NoTestArtifacts: []signals.NoTestArtifactDeepDive{},
		},
		Profile: signals.ProfileSummary{
			DisplayName:        "Example Developer",
			Headline:           "AI-native contribution profile",
			AnalyzedPRs:        1,
			AnalysisWindowDays: 90,
			Confidence:         signals.ConfidenceMedium,
			Strengths:          []signals.Finding{{Label: "Focused", Evidence: "Single-purpose change.", Confidence: signals.ConfidenceMedium}},
			ImprovementTrends:  []signals.Finding{{Label: "Contract coverage", Evidence: "Added focused tests.", Confidence: signals.ConfidenceMedium}},
			BadgeCandidates:    []signals.BadgeCandidate{{ID: "focused", Label: "Focused contributor", Confidence: signals.ConfidenceMedium}},
		},
		SetupActions: []signals.SetupAction{{
			ID:               "import_coverage",
			Label:            "Import coverage evidence",
			Command:          "go test ./... -coverprofile=coverage.out",
			Why:              "Coverage raises confidence.",
			ConfidenceImpact: "medium",
		}},
		Limitations: []string{"Local-only fixture."},
		Privacy: signals.PrivacySummary{
			PublicSafe:                         true,
			RawCodeIncluded:                    false,
			RawDiffsIncluded:                   false,
			PrivatePathsIncludedInPublicExport: false,
			AuthorEmailsIncluded:               false,
		},
	}
}

func reportPrivacyContractKeys() []string {
	return []string{
		"public_safe",
		"raw_code_included",
		"raw_diffs_included",
		"private_paths_included_in_public_export",
		"author_emails_included",
	}
}

func marshalReportContractObject(t *testing.T, value any) map[string]any {
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

func reportContractNestedObject(t *testing.T, object map[string]any, key string) map[string]any {
	t.Helper()
	nested, ok := object[key].(map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want object", key, object[key])
	}
	return nested
}

func assertReportContractKeys(t *testing.T, object map[string]any, want []string) {
	t.Helper()
	got := sortedReportContractKeys(object)
	want = append([]string{}, want...)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON keys = %v, want %v", got, want)
	}
}

func sortedReportContractKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
