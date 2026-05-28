package report

import (
	"testing"
	"time"

	"github.com/contribution-dev/contribution/internal/signals"
)

func TestProfileExportIsPublicSafe(t *testing.T) {
	analysis := signals.AnalysisReport{
		GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Profile: signals.ProfileSummary{
			Headline:           "AI-native contribution profile",
			AnalyzedPRs:        1,
			AnalysisWindowDays: 90,
			Confidence:         signals.ConfidenceMedium,
			Strengths: []signals.Finding{{
				Label:      "Focused local changes",
				Evidence:   "1 recent commit changed five or fewer files.",
				Confidence: signals.ConfidenceMedium,
			}},
		},
		PRCards: []signals.PRQualityCard{{
			PRNumber:   123,
			Title:      "Sensitive PR",
			URL:        "https://example.test/private",
			Label:      "mixed",
			Confidence: signals.ConfidenceLow,
			Risks: []signals.Finding{{
				Label: "Private risk",
			}},
			MainRisk:   "private path risk",
			NextAction: "private action",
		}},
	}
	export := ProfileExport(analysis)
	if !export.Redaction.PublicSafe {
		t.Fatal("PublicSafe = false, want true")
	}
	if export.Redaction.RawCodeIncluded || export.Redaction.RawDiffsIncluded || export.Redaction.PrivatePathsIncluded {
		t.Fatalf("redaction flags are unsafe: %+v", export.Redaction)
	}
	if got := export.SelectedArtifacts[0]; got.URL != "" || len(got.Risks) != 0 || got.MainRisk != "" || got.NextAction != "" {
		t.Fatalf("selected artifact was not redacted: %+v", got)
	}
}
