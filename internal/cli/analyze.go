package cli

import (
	"context"
	"io"
	"time"

	"github.com/contribution-dev/contribution/internal/analysis"
	"github.com/spf13/cobra"
)

func newAnalyzeCommand(out io.Writer, _ io.Writer) *cobra.Command {
	opts := analysis.Options{}
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze a local repo or public Git URL.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
			defer cancel()
			_, err := analysis.Run(ctx, out, opts)
			return err
		},
	}
	cmd.Flags().StringVar(&opts.Repo, "repo", "", "Repo path or Git URL. Defaults to current directory.")
	cmd.Flags().StringVar(&opts.Since, "since", "", "Analysis window such as 90d.")
	cmd.Flags().IntVar(&opts.MaxPRs, "max-prs", 0, "Maximum PRs or commit groups to include.")
	cmd.Flags().StringVar(&opts.GitHubToken, "github-token", "", "GitHub token or env var name for optional metadata.")
	cmd.Flags().StringVar(&opts.Output, "output", "", "Output directory root.")
	cmd.Flags().StringVar(&opts.Format, "format", "all", "Output format: json, markdown, or all.")
	cmd.Flags().BoolVar(&opts.PublicSafe, "public-safe", false, "Redact local repo metadata from analysis.json.")
	cmd.Flags().BoolVar(&opts.NoExternalTools, "no-external-tools", false, "Skip optional external tool discovery.")
	cmd.Flags().StringArrayVar(&opts.CoveragePaths, "coverage", nil, "Coverage artifact path. May be repeated.")
	cmd.Flags().StringVar(&opts.CoverageFormat, "coverage-format", "auto", "Coverage format: auto, go, or lcov.")
	cmd.Flags().BoolVar(&opts.IncludeAgentArtifacts, "include-agent-artifacts", false, "Opt in to metadata-only import from explicitly supplied agent artifact paths.")
	cmd.Flags().StringArrayVar(&opts.AgentArtifactPaths, "agent-artifact", nil, "Metadata-only agent artifact JSON path. Requires --include-agent-artifacts. May be repeated.")
	return cmd
}

func newProbeCommand(out io.Writer, _ io.Writer) *cobra.Command {
	opts := analysis.Options{PublicSafe: true, Format: "json"}
	cmd := &cobra.Command{
		Use:   "probe",
		Short: "Collect a public-safe local probe bundle for the web app.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
			defer cancel()
			_, err := analysis.RunProbe(ctx, out, opts)
			return err
		},
	}
	cmd.Flags().StringVar(&opts.Repo, "repo", "", "Repo path or Git URL. Defaults to current directory.")
	cmd.Flags().StringVar(&opts.GitHubToken, "github-token", "", "GitHub token or env var name for optional metadata.")
	cmd.Flags().StringVar(&opts.Output, "output", "", "Output directory root.")
	cmd.Flags().BoolVar(&opts.PublicSafe, "public-safe", true, "Redact local repo metadata from the probe bundle.")
	cmd.Flags().BoolVar(&opts.NoExternalTools, "no-external-tools", false, "Skip optional external tool discovery.")
	cmd.Flags().BoolVar(&opts.IncludeAgentArtifacts, "include-agent-artifacts", false, "Opt in to metadata-only import from explicitly supplied agent artifact paths.")
	cmd.Flags().StringArrayVar(&opts.AgentArtifactPaths, "agent-artifact", nil, "Metadata-only agent artifact JSON path. Requires --include-agent-artifacts. May be repeated.")
	return cmd
}
