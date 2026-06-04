package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/contribution-dev/contribution/internal/signals"
)

func TestDiscoverReportsRequiredToolAndSkippedOptionalTools(t *testing.T) {
	bin := t.TempDir()
	writeFakeTool(t, bin, "git", "git version 2.0.0\n", 0)
	t.Setenv("PATH", bin)

	got := Discover(context.Background(), false, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	git := findTool(t, got.Tools, "git")
	if !git.Available || git.Version != "git version 2.0.0" || !git.Required {
		t.Fatalf("git availability = %+v, want required available git", git)
	}
	semgrep := findTool(t, got.Tools, "semgrep")
	if semgrep.Available || !strings.Contains(semgrep.Reason, "--no-external-tools") {
		t.Fatalf("semgrep availability = %+v, want skipped optional", semgrep)
	}
	if len(got.Limitations) == 0 {
		t.Fatal("Limitations empty, want skipped optional limitations")
	}
}

func TestDiscoverForRepoDoesNotTrustRepoLocalOptionalToolsByDefault(t *testing.T) {
	bin := t.TempDir()
	writeFakeTool(t, bin, "git", "git version 2.0.0\n", 0)
	t.Setenv("PATH", bin)

	repo := t.TempDir()
	marker := filepath.Join(repo, "executed")
	repoBin := filepath.Join(repo, ".tools", "bin")
	if err := os.MkdirAll(repoBin, 0o700); err != nil {
		t.Fatalf("mkdir repo bin: %v", err)
	}
	writeFakeToolScript(t, repoBin, "semgrep", "#!/bin/sh\ntouch "+shellQuote(marker)+"\nprintf '1.164.0\\n'\n")

	got := DiscoverForRepo(context.Background(), true, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), repo)

	semgrep := findTool(t, got.Tools, "semgrep")
	if semgrep.Available {
		t.Fatalf("semgrep availability = %+v, want repo-local optional tool ignored by default", semgrep)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("repo-local optional tool executed; stat marker err = %v", err)
	}
}

func TestDiscoverWithOptionsFindsTrustedRepoLocalOptionalTools(t *testing.T) {
	bin := t.TempDir()
	writeFakeTool(t, bin, "git", "git version 2.0.0\n", 0)
	t.Setenv("PATH", bin)

	repo := t.TempDir()
	repoBin := filepath.Join(repo, ".tools", "bin")
	if err := os.MkdirAll(repoBin, 0o700); err != nil {
		t.Fatalf("mkdir repo bin: %v", err)
	}
	writeFakeTool(t, repoBin, "semgrep", "1.164.0\n", 0)

	got := DiscoverWithOptions(context.Background(), true, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), DiscoverOptions{
		RepoPath:            repo,
		TrustRepoLocalTools: true,
	})

	semgrep := findTool(t, got.Tools, "semgrep")
	if !semgrep.Available || semgrep.Version != "1.164.0" {
		t.Fatalf("semgrep availability = %+v, want trusted repo-local optional tool", semgrep)
	}
}

func TestSignalsReflectRequiredToolFailure(t *testing.T) {
	report := Discover(context.Background(), false, time.Now())
	report.Tools = report.Tools[:0]
	report.Tools = append(report.Tools, signals.ToolAvailability{Name: "git", Required: true, Available: false, Reason: "not found"})

	got := Signals("repo", report, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if len(got) != 1 {
		t.Fatalf("signals = %d, want 1", len(got))
	}
	if got[0].Severity != "high" || got[0].Direction != "negative" || got[0].Value != 0 {
		t.Fatalf("signal = %+v, want high negative unavailable required tool", got[0])
	}
}

func TestToolCommandEnvAddsRepoLocalAnalyzerDefaults(t *testing.T) {
	repo := t.TempDir()

	got := toolCommandEnv([]string{"PATH=/bin"}, repo)

	for _, want := range []string{
		"SEMGREP_LOG_FILE=" + filepath.Join(repo, ".tools", "semgrep", "semgrep.log"),
		"SEMGREP_SETTINGS_FILE=" + filepath.Join(repo, ".tools", "semgrep", "settings.yml"),
		"SEMGREP_VERSION_CACHE_PATH=" + filepath.Join(repo, ".tools", "semgrep", "semgrep_version"),
		"TRIVY_CACHE_DIR=" + filepath.Join(repo, ".tools", "trivy-cache"),
	} {
		if !containsEnv(got, want) {
			t.Fatalf("tool env missing %q in %v", want, got)
		}
	}
}

func writeFakeTool(t *testing.T, bin string, name string, stdout string, exitCode int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake shell tools are unix-only")
	}
	body := "#!/bin/sh\nprintf '%s' " + shellQuote(stdout) + "\nexit " + fmt.Sprint(exitCode) + "\n"
	writeFakeToolScript(t, bin, name, body)
}

func writeFakeToolScript(t *testing.T, bin string, name string, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake shell tools are unix-only")
	}
	path := filepath.Join(bin, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
	// #nosec G302 -- test fake tools must be executable inside a private temp dir.
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatalf("chmod fake %s: %v", name, err)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func findTool(t *testing.T, tools []signals.ToolAvailability, name string) signals.ToolAvailability {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found in %+v", name, tools)
	return signals.ToolAvailability{}
}

func containsEnv(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
