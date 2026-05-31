package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/contribution-dev/contribution/internal/workunit"
	"github.com/spf13/cobra"
)

func newWorkUnitCommand(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "work-unit",
		Short: "Create and export local agentic work-unit markers.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newWorkUnitStartCommand(out), newWorkUnitExportCommand(out))
	return cmd
}

func newWorkUnitStartCommand(out io.Writer) *cobra.Command {
	opts := workunit.StartOptions{}
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Create a local marker for a unit of agentic work.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), time.Minute)
			defer cancel()
			path, marker, err := workunit.Start(ctx, opts)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(out, "Work unit: %s\nMarker: %s\n", marker.ID, path)
			return err
		},
	}
	cmd.Flags().StringVar(&opts.Repo, "repo", "", "Repo path. Defaults to current directory.")
	cmd.Flags().StringVar(&opts.Goal, "goal", "", "Goal for this unit of agentic work.")
	cmd.Flags().StringVar(&opts.Issue, "issue", "", "Optional issue, ticket, or project reference.")
	cmd.Flags().StringVar(&opts.Output, "output", "", "Marker directory. Defaults to .contribution/work-units in the repo.")
	return cmd
}

func newWorkUnitExportCommand(out io.Writer) *cobra.Command {
	opts := workunit.ExportOptions{}
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export local work-unit markers as JSON.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), time.Minute)
			defer cancel()
			path, export, err := workunit.Export(ctx, opts)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(out, "Work units: %d\nData: %s\n", len(export.Markers), path)
			return err
		},
	}
	cmd.Flags().StringVar(&opts.Repo, "repo", "", "Repo path. Defaults to current directory.")
	cmd.Flags().StringVar(&opts.Output, "output", "", "Output directory. Defaults to .contribution/work-units in the repo.")
	return cmd
}
