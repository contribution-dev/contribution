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
