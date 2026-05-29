package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/contribution-dev/contribution/internal/config"
	"github.com/contribution-dev/contribution/internal/friend"
	"github.com/contribution-dev/contribution/internal/report"
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
			cfg, _, err := config.Load(repo.Path)
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
			card, ok := friend.FindPRCard(analysis.PRCards, pr)
			if !ok {
				return fmt.Errorf("analysis does not contain PR #%d; run analyze with GitHub metadata for PR packets", pr)
			}
			start := time.Now().UTC()
			packet := friend.BuildPacket(analysis.Repo, card, publicSafe, start)
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
