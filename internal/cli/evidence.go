package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/contribution-dev/contribution/internal/evidence"
	"github.com/spf13/cobra"
)

func newEvidenceCommand(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evidence",
		Short: "Preview or export opt-in derived AI work evidence bundles.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		newEvidencePreviewCommand(out),
		newEvidenceExportCommand(out),
		newEvidenceDoctorCommand(out),
		newEvidenceUploadCommand(),
	)
	return cmd
}

func newEvidencePreviewCommand(out io.Writer) *cobra.Command {
	opts := evidence.Options{}
	cmd := &cobra.Command{
		Use:   "preview",
		Short: "Preview derived AI work evidence without writing or uploading.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
			defer cancel()
			result, err := evidence.Preview(ctx, opts)
			if err != nil {
				return err
			}
			return writeEvidencePreview(out, result)
		},
	}
	addEvidenceSourceFlags(cmd, &opts)
	return cmd
}

func newEvidenceExportCommand(out io.Writer) *cobra.Command {
	opts := evidence.Options{}
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Write an offline derived AI work evidence JSON bundle.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
			defer cancel()
			result, err := evidence.Export(ctx, opts)
			if err != nil {
				return err
			}
			return writeEvidenceExport(out, result)
		},
	}
	addEvidenceSourceFlags(cmd, &opts)
	cmd.Flags().StringVar(&opts.Output, "output", "", "Output root. Defaults to the repo reports output directory.")
	cmd.Flags().BoolVar(&opts.IncludeFilePaths, "include-file-paths", false, "Include repo-relative file paths instead of only path hashes.")
	cmd.Flags().BoolVar(&opts.IncludeRepoName, "include-repo-name", false, "Include repo name when you know it is public-safe.")
	return cmd
}

func newEvidenceDoctorCommand(out io.Writer) *cobra.Command {
	opts := evidence.Options{}
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check local AI evidence source availability without reading sessions.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), time.Minute)
			defer cancel()
			result, err := evidence.Doctor(ctx, opts)
			if err != nil {
				return err
			}
			return writeEvidenceDoctor(out, result)
		},
	}
	addEvidenceSourceFlags(cmd, &opts)
	return cmd
}

func newEvidenceUploadCommand() *cobra.Command {
	opts := evidence.Options{}
	return &cobra.Command{
		Use:   "upload",
		Short: "Disabled until the CLI consumes a finalized website receiving contract.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), time.Minute)
			defer cancel()
			return evidence.Upload(ctx, opts)
		},
	}
}

func addEvidenceSourceFlags(cmd *cobra.Command, opts *evidence.Options) {
	cmd.Flags().StringVar(&opts.Repo, "repo", "", "Repo path. Defaults to current directory.")
	cmd.Flags().StringArrayVar(&opts.Sources, "source", nil, "Evidence source to scan: claude or codex. May be repeated; defaults to both.")
	cmd.Flags().StringVar(&opts.ClaudeDir, "claude-dir", "", "Claude Code projects directory. Defaults to ~/.claude/projects.")
	cmd.Flags().StringVar(&opts.CodexDir, "codex-dir", "", "Codex CLI sessions directory. Defaults to ~/.codex/sessions.")
}

func writeEvidencePreview(out io.Writer, result evidence.Result) error {
	if _, err := fmt.Fprintln(out, "AI work evidence preview"); err != nil {
		return err
	}
	for _, summary := range result.SourceSummaries {
		if _, err := fmt.Fprintf(out, "- %s: %s (%d artifact(s), %d linked session(s))\n", summary.SourceTool, summary.Status, summary.ArtifactsScanned, summary.SessionsLinked); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "Sources scanned: %d\n", result.SourcesScanned); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Sessions found: %d\n", result.SessionsFound); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Sessions linked: %d\n", result.SessionsLinked); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Sessions skipped: %d\n", result.SessionsSkipped); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Fields extracted: %d\n", result.FieldsExtracted); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Fields redacted: %d\n", result.FieldsRedacted); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Fields blocked: %d\n", result.FieldsBlocked); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "Would export: ai-work-evidence.bundle.json"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "Would export: redaction-receipt.json"); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out, "Hosted upload: disabled")
	return err
}

func writeEvidenceExport(out io.Writer, result evidence.Result) error {
	if _, err := fmt.Fprintln(out, "AI work evidence bundle"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Sessions linked: %d\n", result.SessionsLinked); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Fields blocked: %d\n", result.FieldsBlocked); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Bundle: %s\n", result.BundlePath); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Redaction receipt: %s\n", result.RedactionReceiptPath); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out, "Upload: disabled")
	return err
}

func writeEvidenceDoctor(out io.Writer, result evidence.DoctorResult) error {
	if _, err := fmt.Fprintln(out, "AI work evidence doctor"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Claude Code sessions: %s\n", availability(result.ClaudeAvailable)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Codex CLI sessions: %s\n", availability(result.CodexAvailable)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "Network: not used"); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out, "Upload: disabled until the CLI consumes a finalized website receiving contract")
	return err
}

func availability(available bool) string {
	if available {
		return "available"
	}
	return "missing"
}
