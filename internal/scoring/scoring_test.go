package scoring

import (
	"testing"
	"time"

	gitrepo "github.com/contribution-dev/contribution/internal/git"
	"github.com/contribution-dev/contribution/internal/github"
	"github.com/contribution-dev/contribution/internal/signals"
)

func TestBuildFlagsSourceChangesWithoutTests(t *testing.T) {
	history := gitrepo.History{
		Commits: []gitrepo.Commit{{
			SHA:           "1234567890",
			Date:          time.Now(),
			Subject:       "change auth behavior",
			Files:         []gitrepo.ChangedFile{{Path: "internal/auth/session.go"}},
			SourceTouched: true,
			RiskyTouched:  true,
		}},
		FileTouchCount: map[string]int{"internal/auth/session.go": 1},
	}
	out := Build(Input{
		Repo:      signals.RepoMetadata{DefaultBranch: "main"},
		History:   history,
		Inventory: signals.FileSummary{TotalFiles: 1, SourceFiles: 1},
		SinceDays: 90,
		MaxCards:  20,
	})
	if len(out.Cards) != 1 {
		t.Fatalf("cards = %d, want 1", len(out.Cards))
	}
	if out.Cards[0].Label != "risky" {
		t.Fatalf("card label = %q, want risky", out.Cards[0].Label)
	}
	if len(out.WeaknessMap.Weaknesses) == 0 {
		t.Fatal("expected weakness findings")
	}
	if out.Profile.Headline == "" {
		t.Fatal("expected profile headline")
	}
}

func TestLocalOnlyConfidenceCapsAtMedium(t *testing.T) {
	history := gitrepo.History{FileTouchCount: map[string]int{}}
	for i := 0; i < 12; i++ {
		commit := gitrepo.Commit{
			SHA:   "abcdef1234567890",
			Date:  time.Now(),
			Files: []gitrepo.ChangedFile{{Path: "internal/app.go", Additions: 2}},
		}
		history.Commits = append(history.Commits, commit)
	}
	out := Build(Input{
		Repo:      signals.RepoMetadata{DefaultBranch: "main"},
		History:   history,
		Inventory: signals.FileSummary{TotalFiles: 1, SourceFiles: 1},
		SinceDays: 90,
		MaxCards:  20,
	})
	if out.WeaknessMap.Confidence != signals.ConfidenceMedium {
		t.Fatalf("weakness confidence = %q, want medium", out.WeaknessMap.Confidence)
	}
	if out.Profile.Confidence != signals.ConfidenceMedium {
		t.Fatalf("profile confidence = %q, want medium", out.Profile.Confidence)
	}
}

func TestEnrichedConfidenceCanBeHigh(t *testing.T) {
	history := gitrepo.History{FileTouchCount: map[string]int{}}
	for i := 0; i < 12; i++ {
		history.Commits = append(history.Commits, gitrepo.Commit{
			SHA:   "abcdef1234567890",
			Date:  time.Now(),
			Files: []gitrepo.ChangedFile{{Path: "internal/app.go", Additions: 2}},
		})
	}
	out := Build(Input{
		Repo:      signals.RepoMetadata{DefaultBranch: "main"},
		History:   history,
		GitHub:    github.Metadata{Available: true},
		Inventory: signals.FileSummary{TotalFiles: 1, SourceFiles: 1},
		SinceDays: 90,
		MaxCards:  20,
	})
	if out.WeaknessMap.Confidence != signals.ConfidenceHigh {
		t.Fatalf("weakness confidence = %q, want high", out.WeaknessMap.Confidence)
	}
}

func TestCommitCardsIncludeLineScope(t *testing.T) {
	history := gitrepo.History{
		Commits: []gitrepo.Commit{{
			SHA:   "1234567890",
			Date:  time.Now(),
			Files: []gitrepo.ChangedFile{{Path: "internal/app.go", Additions: 10, Deletions: 3}},
		}},
		FileTouchCount: map[string]int{"internal/app.go": 1},
	}
	out := Build(Input{
		Repo:      signals.RepoMetadata{DefaultBranch: "main"},
		History:   history,
		Inventory: signals.FileSummary{TotalFiles: 1, SourceFiles: 1},
		SinceDays: 90,
		MaxCards:  20,
	})
	if len(out.Cards) != 1 {
		t.Fatalf("cards = %d, want 1", len(out.Cards))
	}
	if out.Cards[0].Scope != "1 file and 13 lines" {
		t.Fatalf("scope = %q, want file and line count", out.Cards[0].Scope)
	}
	if out.Cards[0].Summary != "Commit-group card based on 1 file and 13 lines." {
		t.Fatalf("summary = %q, want file and line count", out.Cards[0].Summary)
	}
}
