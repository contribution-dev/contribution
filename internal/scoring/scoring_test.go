package scoring

import (
	"testing"
	"time"

	gitrepo "github.com/contribution-dev/contribution/internal/git"
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
