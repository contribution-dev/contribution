package scoring

import (
	"strings"
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
	if len(out.DeepDives.NoTestArtifacts) != 1 {
		t.Fatalf("no-test deep dives = %+v, want one", out.DeepDives.NoTestArtifacts)
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

func TestBuildExplainsHighChurnArtifacts(t *testing.T) {
	history := gitrepo.History{
		Commits: []gitrepo.Commit{{
			SHA:   "abcdef1234567890",
			Date:  time.Now(),
			Files: []gitrepo.ChangedFile{{Path: "internal/report/report.go", Additions: 10}},
		}},
		FileTouchCount: map[string]int{"internal/report/report.go": 3},
		HighChurnFiles: []string{"internal/report/report.go"},
	}
	out := Build(Input{
		Repo:      signals.RepoMetadata{DefaultBranch: "main"},
		History:   history,
		Inventory: signals.FileSummary{TotalFiles: 1, SourceFiles: 1},
		SinceDays: 90,
		MaxCards:  20,
	})
	if len(out.DeepDives.HighChurn) != 1 {
		t.Fatalf("high churn deep dives = %+v, want one", out.DeepDives.HighChurn)
	}
	if got := out.DeepDives.HighChurn[0]; got.Path != "internal/report/report.go" || got.Touches != 3 || len(got.Artifacts) != 1 {
		t.Fatalf("high churn detail = %+v", got)
	}
}

func TestBuildUsesGitHubDurabilityEvidence(t *testing.T) {
	mergedAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	history := gitrepo.History{
		Commits: []gitrepo.Commit{{
			SHA:           "fix1234567890",
			Date:          mergedAt.Add(24 * time.Hour),
			Subject:       "fix auth regression",
			Files:         []gitrepo.ChangedFile{{Path: "internal/auth/session.go", Additions: 4}},
			SourceTouched: true,
			RiskyTouched:  true,
			IsFollowUpFix: true,
		}},
		FileTouchCount: map[string]int{"internal/auth/session.go": 3},
		HighChurnFiles: []string{"internal/auth/session.go"},
	}
	out := Build(Input{
		Repo:    signals.RepoMetadata{DefaultBranch: "main"},
		History: history,
		GitHub: github.Metadata{Available: true, PRs: []github.PullRequest{{
			Number:           7,
			Title:            "Change auth session",
			MergedAt:         mergedAt,
			ChangedFiles:     1,
			Additions:        20,
			Deletions:        5,
			Files:            []string{"internal/auth/session.go"},
			ReviewCount:      2,
			RequestedChanges: 1,
			CheckRuns:        2,
			FailedChecks:     1,
		}}},
		Inventory: signals.FileSummary{TotalFiles: 1, SourceFiles: 1},
		SinceDays: 90,
		MaxCards:  20,
	})

	if len(out.Cards) != 1 {
		t.Fatalf("cards = %d, want 1", len(out.Cards))
	}
	card := out.Cards[0]
	if card.Label != "risky" || !strings.Contains(card.Durability, "later fix/revert-like") {
		t.Fatalf("card = %+v, want risky durability evidence", card)
	}
	if !hasWeakness(out.WeaknessMap.Weaknesses, "Some PRs needed post-merge follow-up") {
		t.Fatalf("weaknesses = %+v, want post-merge follow-up weakness", out.WeaknessMap.Weaknesses)
	}
}

func TestBuildAddsAnalyzerWeakness(t *testing.T) {
	out := Build(Input{
		Repo:      signals.RepoMetadata{DefaultBranch: "main"},
		History:   gitrepo.History{FileTouchCount: map[string]int{}},
		Inventory: signals.FileSummary{TotalFiles: 1, SourceFiles: 1},
		AnalyzerFindings: []signals.AnalyzerFinding{{
			Tool:       "semgrep",
			RuleID:     "go.rule",
			Severity:   signals.SeverityMedium,
			FilePath:   "internal/app.go",
			Message:    "avoid this",
			Confidence: signals.ConfidenceMedium,
		}},
		SinceDays: 90,
		MaxCards:  20,
	})
	if !hasWeakness(out.WeaknessMap.Weaknesses, "Optional analyzers found issues") {
		t.Fatalf("weaknesses = %+v, want analyzer weakness", out.WeaknessMap.Weaknesses)
	}
}

func hasWeakness(findings []signals.Finding, label string) bool {
	for _, finding := range findings {
		if finding.Label == label {
			return true
		}
	}
	return false
}
