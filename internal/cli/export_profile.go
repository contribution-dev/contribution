package cli

import (
	"fmt"
	"io"

	"github.com/contribution-dev/contribution/internal/report"
	"github.com/spf13/cobra"
)

func newExportProfileCommand(out io.Writer) *cobra.Command {
	var input string
	var output string
	cmd := &cobra.Command{
		Use:   "export-profile",
		Short: "Write public-safe profile export artifacts from analysis.json.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if input == "" {
				return fmt.Errorf("--input is required")
			}
			if output == "" {
				return fmt.Errorf("--output is required")
			}
			analysis, err := report.ReadAnalysis(input)
			if err != nil {
				return err
			}
			analysis = report.PublicSafeAnalysis(analysis)
			if err := report.WriteProfileArtifacts(output, analysis); err != nil {
				return err
			}
			return writef(out, "Profile export artifacts written to %s\n", output)
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "Path to analysis.json.")
	cmd.Flags().StringVar(&output, "output", "", "Output directory.")
	return cmd
}
