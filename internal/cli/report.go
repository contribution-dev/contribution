package cli

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/contribution-dev/contribution/internal/report"
	"github.com/spf13/cobra"
)

func newReportCommand(out io.Writer) *cobra.Command {
	var input string
	var output string
	var format string
	var publicSafe bool
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Regenerate reports from analysis.json.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if input == "" {
				return fmt.Errorf("--input is required")
			}
			if err := report.ValidateFormat(format, true); err != nil {
				return err
			}
			analysis, err := report.ReadAnalysis(input)
			if err != nil {
				return err
			}
			if publicSafe {
				analysis = report.PublicSafeAnalysis(analysis)
			}
			if output == "" {
				output = filepath.Dir(input)
			}
			if err := report.WriteReportOnly(output, analysis, format); err != nil {
				return err
			}
			return writef(out, "Report artifacts written to %s\n", output)
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "Path to analysis.json.")
	cmd.Flags().StringVar(&output, "output", "", "Output directory.")
	cmd.Flags().StringVar(&format, "format", "all", "Output format: json, markdown, or all.")
	cmd.Flags().BoolVar(&publicSafe, "public-safe", false, "Redact local repo metadata from regenerated artifacts.")
	return cmd
}
