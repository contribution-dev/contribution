package report

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/contribution-dev/contribution/internal/signals"
)

func BenchmarkWriteAnalysisBundle(b *testing.B) {
	analysis := signals.AnalysisReport{
		Version:     1,
		GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Repo: signals.RepoMetadata{
			ID:   "local:bench",
			Name: "bench",
		},
		Tooling: signals.ToolingReport{
			GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Tools: []signals.ToolAvailability{{
				Name:      "git",
				Required:  true,
				Available: true,
				Version:   "git version 2.54.0",
			}},
		},
		Inventory: signals.FileSummary{TotalFiles: 120, SourceFiles: 70, TestFiles: 35, DocsFiles: 10},
		Coverage:  signals.CoverageSummary{Status: "available", CoveredLines: 700, TotalLines: 1000, Percent: 70},
		PRCards: []signals.PRQualityCard{{
			PRNumber:     42,
			Label:        "strong",
			Confidence:   signals.ConfidenceMedium,
			Summary:      "Focused benchmark artifact.",
			Scope:        "3 files",
			TestEvidence: "Tests touched.",
			ReviewBurden: "Low.",
			Durability:   "Stable.",
			MainRisk:     "No major benchmark risk.",
			NextAction:   "Keep scope focused.",
		}},
		WeaknessMap: signals.WeaknessMap{
			Confidence: signals.ConfidenceMedium,
			Strengths:  []signals.Finding{{Label: "Focused scope", Evidence: "Small changes.", Confidence: signals.ConfidenceMedium}},
		},
		Profile: signals.ProfileSummary{
			Headline:           "Agentic readiness profile",
			AnalyzedPRs:        1,
			AnalysisWindowDays: 90,
			Confidence:         signals.ConfidenceMedium,
		},
		Privacy: signals.PrivacySummary{PublicSafe: true},
	}

	root := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		outputDir := filepath.Join(root, "run", string(rune('a'+i%26)))
		if err := WriteAnalysisBundle(outputDir, analysis, "all"); err != nil {
			b.Fatal(err)
		}
	}
}
