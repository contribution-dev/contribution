package cli

import (
	"context"
	"io"
	"time"

	"github.com/spf13/cobra"
)

func newAnalyzeCommand(out io.Writer, _ io.Writer) *cobra.Command {
	opts := analyzeOptions{}
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze a local repo or public Git URL.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
			defer cancel()
			_, err := runAnalyze(ctx, out, opts)
			return err
		},
	}
	cmd.Flags().StringVar(&opts.repo, "repo", "", "Repo path or Git URL. Defaults to current directory.")
	cmd.Flags().StringVar(&opts.since, "since", "", "Analysis window such as 90d.")
	cmd.Flags().IntVar(&opts.maxPRs, "max-prs", 0, "Maximum PRs or commit groups to include.")
	cmd.Flags().StringVar(&opts.githubToken, "github-token", "", "GitHub token or env var name for optional metadata.")
	cmd.Flags().StringVar(&opts.output, "output", "", "Output directory root.")
	cmd.Flags().StringVar(&opts.format, "format", "all", "Output format: json, markdown, or all.")
	cmd.Flags().BoolVar(&opts.publicSafe, "public-safe", false, "Redact local repo metadata from analysis.json.")
	cmd.Flags().BoolVar(&opts.noExternalTools, "no-external-tools", false, "Skip optional external tool discovery.")
	cmd.Flags().BoolVar(&opts.verbose, "verbose", false, "Print verbose progress.")
	return cmd
}
