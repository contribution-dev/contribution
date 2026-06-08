package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvidencePreviewAndExportCommands(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := newCLIEvidenceRepo(t)
	claudeDir, codexDir := writeCLIEvidenceFixtures(t, repo)

	stdout, stderr, err := executeForTest([]string{
		"evidence", "preview",
		"--repo", repo,
		"--claude-dir", claudeDir,
		"--codex-dir", codexDir,
	}, BuildInfo{})
	if err != nil {
		t.Fatalf("evidence preview error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("preview stderr = %q, want empty", stderr)
	}
	for _, want := range []string{
		"AI work evidence preview",
		"Sources scanned: 2",
		"Sessions found: 2",
		"Sessions linked: 2",
		"Fields extracted:",
		"Fields blocked:",
		"Would export: ai-work-evidence.bundle.json",
		"Hosted upload: disabled",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("preview stdout missing %q:\n%s", want, stdout)
		}
	}

	outputRoot := t.TempDir()
	stdout, stderr, err = executeForTest([]string{
		"evidence", "export",
		"--repo", repo,
		"--output", outputRoot,
		"--claude-dir", claudeDir,
		"--codex-dir", codexDir,
	}, BuildInfo{})
	if err != nil {
		t.Fatalf("evidence export error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("export stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "AI work evidence bundle") || !strings.Contains(stdout, "Upload: disabled") {
		t.Fatalf("export stdout missing summary:\n%s", stdout)
	}
	bundlePath := lineValue(t, stdout, "Bundle: ")
	receiptPath := lineValue(t, stdout, "Redaction receipt: ")
	assertFileExists(t, bundlePath)
	assertFileExists(t, receiptPath)

	// #nosec G304 -- test reads an artifact path printed by the command under t.TempDir.
	data, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	var bundle map[string]any
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatalf("parse bundle: %v", err)
	}
	if bundle["schema"] != "ai_work_evidence_bundle" || bundle["work_sessions"] == nil {
		t.Fatalf("bundle missing schema/sessions: %+v", bundle)
	}
	if strings.Contains(string(data), "raw prompt must not leak") || strings.Contains(string(data), "super-secret") {
		t.Fatalf("bundle leaked raw fixture content:\n%s", string(data))
	}
}

func TestEvidenceDoctorAndUploadDisabled(t *testing.T) {
	repo := t.TempDir()
	claudeDir := filepath.Join(t.TempDir(), "claude", "projects")
	codexDir := filepath.Join(t.TempDir(), "codex", "sessions")
	if err := os.MkdirAll(claudeDir, 0o750); err != nil {
		t.Fatalf("mkdir claude dir: %v", err)
	}

	stdout, stderr, err := executeForTest([]string{
		"evidence", "doctor",
		"--repo", repo,
		"--claude-dir", claudeDir,
		"--codex-dir", codexDir,
	}, BuildInfo{})
	if err != nil {
		t.Fatalf("evidence doctor error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("doctor stderr = %q, want empty", stderr)
	}
	for _, want := range []string{
		"AI work evidence doctor",
		"Claude Code sessions: available",
		"Codex CLI sessions: missing",
		"Network: not used",
		"Upload: disabled until the CLI consumes a finalized website receiving contract",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("doctor stdout missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, err = executeForTest([]string{"evidence", "upload"}, BuildInfo{})
	if err == nil {
		t.Fatal("evidence upload error = nil, want disabled upload")
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("upload stdout/stderr = %q/%q, want empty before process-level handling", stdout, stderr)
	}
	if !strings.Contains(err.Error(), "disabled until the CLI consumes a finalized website receiving contract") {
		t.Fatalf("upload error = %v", err)
	}
}

func newCLIEvidenceRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "cli@example.test")
	runGit(t, repo, "config", "user.name", "CLI Test")
	if err := os.MkdirAll(filepath.Join(repo, "cmd"), 0o750); err != nil {
		t.Fatalf("mkdir cmd: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "cmd", "app.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatalf("write app.go: %v", err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "ENG-123 initial fixture")
	runGit(t, repo, "remote", "add", "origin", "https://github.com/acme/cli-evidence.git")
	return repo
}

func writeCLIEvidenceFixtures(t *testing.T, repo string) (string, string) {
	t.Helper()
	head := strings.TrimSpace(cliGitOutput(t, repo, "rev-parse", "HEAD"))
	claudeDir := filepath.Join(t.TempDir(), "claude", "projects")
	codexDir := filepath.Join(t.TempDir(), "codex", "sessions")
	claudeProject := filepath.Join(claudeDir, "-"+strings.ReplaceAll(strings.Trim(filepath.ToSlash(repo), "/"), "/", "-"))
	writeCLIFile(t, filepath.Join(claudeProject, "session.jsonl"), strings.Join([]string{
		`{"timestamp":"2026-06-01T10:00:00Z","type":"user","cwd":"` + filepath.ToSlash(repo) + `","branch":"main","commit_sha":"` + head + `","message":{"role":"user","content":"ENG-123 raw prompt must not leak API_TOKEN=super-secret"}}`,
		`{"timestamp":"2026-06-01T10:01:00Z","type":"assistant","message":{"role":"assistant","content":"Raw model output must not leak"}}`,
		`{"timestamp":"2026-06-01T10:02:00Z","type":"tool_use","name":"Edit","input":{"file_path":"cmd/app.go","diff":"@@ raw diff must not leak"}}`,
		`{"timestamp":"2026-06-01T10:03:00Z","type":"tool_result","content":"go test ./... failed"}}`,
	}, "\n")+"\n")
	writeCLIFile(t, filepath.Join(codexDir, "session.jsonl"), strings.Join([]string{
		`{"timestamp":"2026-06-01T11:00:00Z","type":"session_meta","payload":{"cwd":"` + filepath.ToSlash(repo) + `","branch":"main","commit_sha":"` + head + `"}}`,
		`{"timestamp":"2026-06-01T11:01:00Z","type":"agent_reasoning","payload":{"intent_summary":"Implement ENG-123 derived summary","plan_summary":"Wire evidence commands","implementation_summary":"Added preview and export"}}`,
		`{"timestamp":"2026-06-01T11:02:00Z","type":"exec_command","payload":{"cmd":"go test ./...","stdout":"terminal log with PASSWORD=super-secret"}}`,
	}, "\n")+"\n")
	return claudeDir, codexDir
}

func cliGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	// #nosec G204 -- test helper invokes the fixed git binary with fixture arguments.
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}

func writeCLIFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
