package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
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
	if publicSafe {
		repo.Root = ""
		repo.RemoteURL = ""
		card.URL = ""
		card.Risks = nil
	}
	evidence := []string{
		card.Summary,
		"Label: " + card.Label,
		"Test evidence: " + card.TestEvidence,
		"Review burden: " + card.ReviewBurden,
		"Durability: " + card.Durability,
	}
	return signals.FriendReviewPacket{
		Version:     1,
		GeneratedAt: now,
		Repo:        repo,
		PRNumber:    card.PRNumber,
		Context:     fmt.Sprintf("PR #%d was flagged as %s confidence with a %s artifact label. The packet omits raw diffs by default.", card.PRNumber, card.Confidence, card.Label),
		Card:        card,
		Evidence:    evidence,
		Questions: []string{
			"Does this PR solve the intended problem cleanly?",
			"Is the implementation maintainable?",
			"Are tests appropriate for the changed behavior?",
			"What is the biggest risk?",
			"What should the author improve next?",
			"Would you trust this developer with similar work?",
			"How confident are you?",
		},
		Confidence: card.Confidence,
		PublicSafe: publicSafe,
	}
}
