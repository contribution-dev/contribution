package preflight

import (
	"fmt"
	"testing"
	"time"

	"github.com/contribution-dev/contribution/internal/config"
	gitrepo "github.com/contribution-dev/contribution/internal/git"
	"github.com/contribution-dev/contribution/internal/signals"
)

func BenchmarkBuild(b *testing.B) {
	diff := gitrepo.DiffSummary{
		FileSummary: signals.FileSummary{
			TotalFiles:  80,
			ByClass:     map[string]int{"source": 60, "test": 20},
			ByLanguage:  map[string]int{"Go": 80},
			SourceFiles: 60,
			TestFiles:   20,
		},
	}
	for i := 0; i < 80; i++ {
		diff.Files = append(diff.Files, gitrepo.ChangedFile{
			Path:       fmt.Sprintf("internal/pkg%d/service.go", i%20),
			Additions:  10 + i%8,
			Deletions:  i % 4,
			LineRanges: []signals.LineRange{{Start: 10, End: 20 + i%8}},
		})
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	personal := signals.PersonalPreflightContext{
		HighChurnFiles:           []string{"internal/pkg0/service.go", "internal/pkg1/service.go"},
		RecentSourceWithoutTests: 4,
		TypicalFiles:             5,
		TypicalLines:             120,
		ArtifactsAnalyzed:        25,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		report := Build(BuildInput{
			Repo:     signals.RepoMetadata{ID: "local:bench", Name: "bench"},
			Base:     "main",
			Head:     "HEAD",
			Diff:     diff,
			Coverage: signals.PreflightCoverage{Status: "available", CoveredLines: 650, TotalLines: 900, Percent: 72.2},
			Policy:   config.PreflightConfig{MaxFiles: 40, MaxLines: 800, RequireTestsForSource: true, ChangedLineCoverageMin: 80},
			Personal: personal,
			Tooling:  signals.ToolingReport{},
			Now:      now,
		})
		if report.Version != 2 {
			b.Fatalf("Version = %d, want 2", report.Version)
		}
	}
}
