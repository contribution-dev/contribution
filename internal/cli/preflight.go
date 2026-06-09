package cli

import (
	"context"
	"io"
	"time"

	preflightpkg "github.com/contribution-dev/contribution/internal/preflight"
	"github.com/spf13/cobra"
)

func newPreflightCommand(out io.Writer) *cobra.Command {
	opts := preflightpkg.Options{Head: "HEAD", Format: "all", CoverageFormat: "auto", FailOnRisk: "never"}
	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Analyze the current diff before review.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
			defer cancel()
			_, err := preflightpkg.Run(ctx, out, opts)
			return err
		},
	}
	cmd.Flags().StringVar(&opts.Base, "base", "", "Base branch or SHA. Defaults to project.default_branch from .contribution.yml, or main.")
	cmd.Flags().StringVar(&opts.Head, "head", "HEAD", "Head branch or SHA.")
	cmd.Flags().StringVar(&opts.Output, "output", "", "Output directory root.")
	cmd.Flags().StringVar(&opts.Format, "format", "all", "Output format: json, markdown, or all.")
	cmd.Flags().StringArrayVar(&opts.CoveragePaths, "coverage", nil, "Coverage artifact path. May be repeated.")
	cmd.Flags().StringVar(&opts.CoverageFormat, "coverage-format", "auto", "Coverage format: auto, go, or lcov.")
	cmd.Flags().StringVar(&opts.FailOnRisk, "fail-on-risk", "never", "Exit nonzero for risk: never, medium, or high.")
	cmd.Flags().BoolVar(&opts.Worktree, "worktree", false, "Compare base against current tracked and untracked worktree changes.")
	cmd.Flags().BoolVar(&opts.NoExternalTools, "no-external-tools", false, "Skip optional external analyzer checks.")
	cmd.Flags().BoolVar(&opts.RunCoverage, "run-coverage", false, "Run configured coverage.command before importing coverage.")
	return cmd
}
