package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCommandShowsHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr, BuildInfo{})
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got := stdout.String(); !strings.Contains(got, "Analyze contribution quality from local repo evidence.") {
		t.Fatalf("help output missing summary: %q", got)
	}
}

func TestVersionCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr, BuildInfo{
		Version: "1.2.3",
		Commit:  "abc123",
		Date:    "2026-05-28",
	})
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	got := stdout.String()
	for _, want := range []string{"contribution 1.2.3", "commit: abc123", "date: 2026-05-28"} {
		if !strings.Contains(got, want) {
			t.Fatalf("version output missing %q: %q", want, got)
		}
	}
}

func TestVersionCommandUsesFallbacks(t *testing.T) {
	stdout, stderr, err := executeForTest([]string{"version"}, BuildInfo{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	for _, want := range []string{"contribution dev", "commit: none", "date: unknown"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("version fallback output missing %q: %q", want, stdout)
		}
	}
}

func TestInvalidFormatFailsBeforeRepoAccess(t *testing.T) {
	stdout, stderr, err := executeForTest([]string{"preflight", "--format", "xml"}, BuildInfo{})
	if err == nil {
		t.Fatal("Execute() error = nil, want unsupported format")
	}
	if !strings.Contains(err.Error(), `unsupported format "xml"`) {
		t.Fatalf("error = %v, want unsupported format", err)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty before process-level error handling", stderr)
	}
}

func TestReportRequiresInput(t *testing.T) {
	stdout, stderr, err := executeForTest([]string{"report"}, BuildInfo{})
	if err == nil {
		t.Fatal("Execute() error = nil, want missing input")
	}
	if !strings.Contains(err.Error(), "--input is required") {
		t.Fatalf("error = %v, want missing input", err)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty before process-level error handling", stderr)
	}
}

func TestExportProfileRequiresInput(t *testing.T) {
	stdout, stderr, err := executeForTest([]string{"export-profile"}, BuildInfo{})
	if err == nil {
		t.Fatal("Execute() error = nil, want missing input")
	}
	if !strings.Contains(err.Error(), "--input is required") {
		t.Fatalf("error = %v, want missing input", err)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty before process-level error handling", stderr)
	}
}

func TestRedactRequiresOutput(t *testing.T) {
	stdout, stderr, err := executeForTest([]string{"redact", "--input", "analysis.json"}, BuildInfo{})
	if err == nil {
		t.Fatal("Execute() error = nil, want missing output")
	}
	if !strings.Contains(err.Error(), "--output is required") {
		t.Fatalf("error = %v, want missing output", err)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty before process-level error handling", stderr)
	}
}

func TestPacketRequiresPR(t *testing.T) {
	stdout, stderr, err := executeForTest([]string{"packet"}, BuildInfo{})
	if err == nil {
		t.Fatal("Execute() error = nil, want missing PR")
	}
	if !strings.Contains(err.Error(), "--pr is required") {
		t.Fatalf("error = %v, want missing PR", err)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty before process-level error handling", stderr)
	}
}

func TestUnknownCommandReturnsErrorWithoutStdout(t *testing.T) {
	stdout, stderr, err := executeForTest([]string{"unknown-command"}, BuildInfo{})
	if err == nil {
		t.Fatal("Execute() error = nil, want unknown command")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("error = %v, want unknown command", err)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty before process-level error handling", stderr)
	}
}

func executeForTest(args []string, info BuildInfo) (string, string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr, info)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}
