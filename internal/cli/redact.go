package cli

import (
	"fmt"
	"io"

	"github.com/contribution-dev/contribution/internal/publicsafe"
	"github.com/contribution-dev/contribution/internal/report"
	"github.com/spf13/cobra"
)

func newRedactCommand(out io.Writer) *cobra.Command {
	var input string
	var output string
	var format string
	cmd := &cobra.Command{
		Use:   "redact",
		Short: "Write public-safe redacted artifacts from analysis.json.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if input == "" {
				return fmt.Errorf("--input is required")
			}
			if output == "" {
				return fmt.Errorf("--output is required")
			}
			if err := report.ValidateFormat(format, true); err != nil {
				return err
			}
			analysis, err := report.ReadAnalysis(input)
			if err != nil {
				return err
			}
			analysis = publicsafe.Analysis(analysis)
			if err := report.WriteReportOnly(output, analysis, format); err != nil {
				return err
			}
			return writef(out, "Public-safe artifacts written to %s\n", output)
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "Path to analysis.json.")
	cmd.Flags().StringVar(&output, "output", "", "Output directory.")
	cmd.Flags().StringVar(&format, "format", "all", "Output format: json, markdown, or all.")
	return cmd
}
