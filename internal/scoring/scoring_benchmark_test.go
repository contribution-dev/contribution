package scoring

import (
	"fmt"
	"testing"
	"time"

	gitrepo "github.com/contribution-dev/contribution/internal/git"
	"github.com/contribution-dev/contribution/internal/signals"
)

func BenchmarkBuild(b *testing.B) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	history := gitrepo.History{
		FileTouchCount: map[string]int{},
	}
	for i := 0; i < 120; i++ {
		sourcePath := fmt.Sprintf("internal/pkg%d/service.go", i%12)
		testPath := fmt.Sprintf("internal/pkg%d/service_test.go", i%12)
		commit := gitrepo.Commit{
			SHA:           fmt.Sprintf("%040d", i),
			Date:          now.Add(-time.Duration(i) * time.Hour),
			Subject:       fmt.Sprintf("change service %d", i),
			SourceTouched: true,
			TestsTouched:  i%3 != 0,
			RiskyTouched:  i%10 == 0,
			Files: []gitrepo.ChangedFile{{
				Path:      sourcePath,
				Additions: 20 + i%15,
				Deletions: 5,
			}},
		}
		if commit.TestsTouched {
			commit.Files = append(commit.Files, gitrepo.ChangedFile{Path: testPath, Additions: 8})
		}
		history.Commits = append(history.Commits, commit)
		history.FileTouchCount[sourcePath]++
	}
	history.HighChurnFiles = []string{
		"internal/pkg0/service.go",
		"internal/pkg1/service.go",
		"internal/pkg2/service.go",
	}

	input := Input{
		Repo: signals.RepoMetadata{
			ID:   "local:bench",
			Name: "bench",
		},
		History:            history,
		Inventory:          signals.FileSummary{TotalFiles: 260, SourceFiles: 120, TestFiles: 80, DocsFiles: 20},
		Coverage:           signals.CoverageSummary{Status: "available", CoveredLines: 7000, TotalLines: 10000, Percent: 70},
		CurrentWindowStart: now.AddDate(0, 0, -90),
		CurrentWindowEnd:   now,
		PriorWindowStart:   now.AddDate(0, 0, -180),
		PriorWindowEnd:     now.AddDate(0, 0, -90),
		SinceDays:          90,
		MaxCards:           20,
		DisplayName:        "bench",
		AITools:            []string{"codex"},
		AIModes:            []string{"review"},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		output := Build(input)
		if len(output.Cards) == 0 {
			b.Fatal("expected benchmark cards")
		}
	}
}
