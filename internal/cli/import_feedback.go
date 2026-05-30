package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/contribution-dev/contribution/internal/friend"
	"github.com/contribution-dev/contribution/internal/publicsafe"
	"github.com/contribution-dev/contribution/internal/report"
	"github.com/spf13/cobra"
)

func newImportFeedbackCommand(out io.Writer) *cobra.Command {
	var analysisPath string
	var feedbackPath string
	var output string
	var format string
	var publicSafe bool
	cmd := &cobra.Command{
		Use:   "import-feedback",
		Short: "Import public-safe friend feedback into an analysis bundle.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if analysisPath == "" {
				return fmt.Errorf("--analysis is required")
			}
			if feedbackPath == "" {
				return fmt.Errorf("--feedback is required")
			}
			if output == "" {
				return fmt.Errorf("--output is required")
			}
			if err := report.ValidateFormat(format, true); err != nil {
				return err
			}
			analysis, err := report.ReadAnalysis(analysisPath)
			if err != nil {
				return err
			}
			feedback, err := friend.ReadFeedbackExports(feedbackPath)
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			analysis = friend.ApplyFeedback(analysis, feedback, now)
			if publicSafe {
				analysis = publicsafe.Analysis(analysis)
			}
			if err := report.WriteAnalysisBundle(output, analysis, format); err != nil {
				return err
			}
			return writef(out, "Feedback import written to %s\n", output)
		},
	}
	cmd.Flags().StringVar(&analysisPath, "analysis", "", "Input analysis.json.")
	cmd.Flags().StringVar(&feedbackPath, "feedback", "", "Feedback JSON file or directory.")
	cmd.Flags().StringVar(&output, "output", "", "Output directory.")
	cmd.Flags().StringVar(&format, "format", "all", "Output format: json, markdown, or all.")
	cmd.Flags().BoolVar(&publicSafe, "public-safe", false, "Redact local repo metadata from imported analysis output.")
	return cmd
}
