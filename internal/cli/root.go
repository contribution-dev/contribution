// Package cli builds the contribution command line interface.
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// BuildInfo is filled by the main package from linker flags at release time.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// NewRootCommand builds the top-level CLI command.
func NewRootCommand(out io.Writer, errOut io.Writer, info BuildInfo) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "contribution",
		Short:         "Analyze contribution quality from local repo evidence.",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.AddCommand(
		newAnalyzeCommand(out, errOut),
		newDoctorCommand(out),
		newInitCommand(out),
		newPacketCommand(out),
		newPreflightCommand(out),
		newReportCommand(out),
		newVersionCommand(out, info),
	)
	return cmd
}

// Execute runs the CLI using process stdio.
func Execute(info BuildInfo) error {
	return NewRootCommand(os.Stdout, os.Stderr, info).Execute()
}

func newVersionCommand(out io.Writer, info BuildInfo) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(
				out,
				"contribution %s\ncommit: %s\ndate: %s\n",
				emptyDefault(info.Version, "dev"),
				emptyDefault(info.Commit, "none"),
				emptyDefault(info.Date, "unknown"),
			)
			return err
		},
	}
}

func emptyDefault(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
