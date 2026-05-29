package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/contribution-dev/contribution/internal/signals"
)

func TestBuildPacketV2IsPublicSafeByDefault(t *testing.T) {
	privateRoot := "/Users/example/private"
	card := signals.PRQualityCard{
		PRNumber:   123,
		Title:      "Fix internal/customer/acme/session.go",
		URL:        "https://github.com/owner/private/pull/123",
		Label:      "mixed",
		Confidence: signals.ConfidenceMedium,
		Summary:    "Merged PR touching 2 files with 10 additions and 2 deletions.",
		Scope:      "2 files and 12 lines",
		Risks:      []signals.Finding{{Label: "private"}},
		MainRisk:   "private path risk",
		NextAction: "private action",
	}

	got := buildPacket(signals.RepoMetadata{
		ID:          "owner/private",
		Name:        "private",
		Root:        privateRoot,
		RemoteURL:   "https://token=secret@example.test/private.git",
		HeadSHA:     "abcdef1234567890",
		GitHubOwner: "owner",
		GitHubRepo:  "private",
	}, card, true, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	if got.Version != 2 {
		t.Fatalf("Version = %d, want 2", got.Version)
	}
	if got.PacketID == "" || strings.Contains(got.PacketID, "abcdef") {
		t.Fatalf("PacketID = %q, want stable non-SHA-looking id", got.PacketID)
	}
	if got.ArtifactLabel != "PR #123" || got.Card.Title != "PR #123" {
		t.Fatalf("public-safe labels = %q/%q, want PR #123", got.ArtifactLabel, got.Card.Title)
	}
	if got.Repo.Root != "" || got.Repo.RemoteURL != "" || got.Repo.HeadSHA != "" || got.Card.URL != "" {
		t.Fatalf("private metadata was not cleared: %+v / %+v", got.Repo, got.Card)
	}
	if len(got.Card.Risks) != 0 || got.Card.MainRisk != "" || got.Card.NextAction != "" {
		t.Fatalf("public-safe card retained private fields: %+v", got.Card)
	}
	if len(got.Rubric) == 0 {
		t.Fatal("Rubric is empty")
	}
}
