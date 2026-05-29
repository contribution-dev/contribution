package cli

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/contribution-dev/contribution/internal/report"
	"github.com/contribution-dev/contribution/internal/signals"
	"github.com/spf13/cobra"
)

func newPacketCommand(out io.Writer) *cobra.Command {
	var pr int
	var output string
	var publicSafe bool
	cmd := &cobra.Command{
		Use:   "packet",
		Short: "Generate a public-safe friend-review packet for a PR.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if pr <= 0 {
				return fmt.Errorf("--pr is required")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			repo, err := currentRepo(ctx)
			if err != nil {
				return err
			}
			defer func() {
				_ = repo.Close()
			}()
			cfg, _, err := loadConfigBestEffort(repo.Path)
			if err != nil {
				return err
			}
			root, err := outputRootForCurrent(output, repo, cfg)
			if err != nil {
				return err
			}
			input, err := latestAnalysisPath(root)
			if err != nil {
				return err
			}
			analysis, err := report.ReadAnalysis(input)
			if err != nil {
				return err
			}
			card, ok := findPRCard(analysis.PRCards, pr)
			if !ok {
				return fmt.Errorf("analysis does not contain PR #%d; run analyze with GitHub metadata for PR packets", pr)
			}
			start := time.Now().UTC()
			packet := buildPacket(analysis.Repo, card, publicSafe, start)
			outputDir := filepath.Join(root, timestamp(start))
			if err := report.WritePacket(outputDir, packet); err != nil {
				return err
			}
			return writef(out, "Friend-review packet written to %s\n", outputDir)
		},
	}
	cmd.Flags().IntVar(&pr, "pr", 0, "PR number.")
	cmd.Flags().StringVar(&output, "output", "", "Output directory root.")
	cmd.Flags().BoolVar(&publicSafe, "public-safe", true, "Redact packet for safe sharing.")
	return cmd
}

func findPRCard(cards []signals.PRQualityCard, pr int) (signals.PRQualityCard, bool) {
	for _, card := range cards {
		if card.PRNumber == pr {
			return card, true
		}
	}
	return signals.PRQualityCard{}, false
}

func buildPacket(repo signals.RepoMetadata, card signals.PRQualityCard, publicSafe bool, now time.Time) signals.FriendReviewPacket {
	artifactLabel := card.Title
	if publicSafe {
		repo.ID = "private-repository"
		repo.Name = "private repository"
		repo.Root = ""
		repo.RemoteURL = ""
		repo.HeadSHA = ""
		repo.GitHubOwner = ""
		repo.GitHubRepo = ""
		card = report.PublicSafeCard(card, 1)
		artifactLabel = card.Title
	}
	evidence := []string{
		card.Summary,
		"Label: " + card.Label,
		"Test evidence: " + card.TestEvidence,
		"Review burden: " + card.ReviewBurden,
		"Durability: " + card.Durability,
	}
	return signals.FriendReviewPacket{
		Version:       2,
		GeneratedAt:   now,
		PacketID:      packetID(repo.ID, card.PRNumber, artifactLabel),
		Repo:          repo,
		PRNumber:      card.PRNumber,
		ArtifactLabel: artifactLabel,
		Context:       fmt.Sprintf("%s was flagged as %s confidence with a %s artifact label. The packet omits raw diffs by default.", artifactLabel, card.Confidence, card.Label),
		Card:          card,
		Evidence:      evidence,
		Rubric: []signals.ReviewRubricQuestion{
			{ID: "problem_fit", Prompt: "Does this change solve the intended problem cleanly?", Focus: "scope and correctness"},
			{ID: "maintainability", Prompt: "Is the implementation maintainable?", Focus: "boundaries, readability, and future changes"},
			{ID: "test_evidence", Prompt: "Are tests appropriate for the changed behavior?", Focus: "missing or weak verification"},
			{ID: "main_risk", Prompt: "What is the biggest risk?", Focus: "security, data, durability, or review burden"},
			{ID: "next_improvement", Prompt: "What should the author improve next?", Focus: "one concrete next action"},
			{ID: "trust", Prompt: "Would you trust this developer with similar work?", Focus: "overall trust signal"},
			{ID: "confidence", Prompt: "How confident are you in this feedback?", Focus: "low, medium, or high"},
		},
		Confidence: card.Confidence,
		PublicSafe: publicSafe,
	}
}

func packetID(repoID string, pr int, label string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s", repoID, pr, label)))
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(h[:])
	return "pkt-" + strings.ToLower(encoded[:16])
}
