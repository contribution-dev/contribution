package evidence

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportBuildsDerivedBundleAndBlocksRawContent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := newEvidenceRepo(t)
	head := strings.TrimSpace(gitOutput(t, repo, "rev-parse", "HEAD"))
	claudeDir, codexDir := writeEvidenceFixtures(t, repo)

	result, err := Export(context.Background(), Options{
		Repo:      repo,
		Output:    t.TempDir(),
		ClaudeDir: claudeDir,
		CodexDir:  codexDir,
	})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	if result.Bundle.Schema != "ai_work_evidence_bundle" || result.Bundle.Version != 1 {
		t.Fatalf("bundle identity = %q/%d", result.Bundle.Schema, result.Bundle.Version)
	}
	if result.Bundle.Upload.Mode != UploadModeDisabled || result.Bundle.Export.Mode != ExportModeOffline {
		t.Fatalf("modes = %+v/%+v, want disabled upload and offline export", result.Bundle.Upload, result.Bundle.Export)
	}
	if len(result.Bundle.WorkSessions) != 2 {
		t.Fatalf("work sessions = %+v, want two linked sessions", result.Bundle.WorkSessions)
	}
	for _, session := range result.Bundle.WorkSessions {
		if session.SourceTool == "" || session.SourceKind == "" || session.StartedAt.IsZero() || session.EndedAt.IsZero() {
			t.Fatalf("session missing required source/time fields: %+v", session)
		}
		if session.RepoRemoteHash == "" || session.Branch == "" || len(session.CommitSHAHashes) == 0 {
			t.Fatalf("session missing repo anchors: %+v", session)
		}
		if session.HumanSteeringCount == 0 || session.AgentActionCount == 0 || session.TestDebugCount == 0 {
			t.Fatalf("session missing derived counts: %+v", session)
		}
		if session.FilesTouchedCount == 0 || len(session.FilePathHashes) == 0 {
			t.Fatalf("session missing hashed file evidence: %+v", session)
		}
		if len(session.FilePaths) != 0 {
			t.Fatalf("session included raw file paths by default: %+v", session.FilePaths)
		}
		if session.RedactionReceiptID != result.Bundle.RedactionReceipt.ID {
			t.Fatalf("redaction receipt linkage = %q, want %q", session.RedactionReceiptID, result.Bundle.RedactionReceipt.ID)
		}
	}
	if result.Bundle.Privacy.RawPromptsIncluded ||
		result.Bundle.Privacy.RawModelOutputsIncluded ||
		result.Bundle.Privacy.RawTranscriptsIncluded ||
		result.Bundle.Privacy.RawDiffsIncluded ||
		result.Bundle.Privacy.RawLogsIncluded ||
		result.Bundle.Privacy.SourceCodeIncluded {
		t.Fatalf("privacy flags allow raw content: %+v", result.Bundle.Privacy)
	}
	if result.Bundle.RedactionReceipt.BlockedContent["raw_prompt"] == 0 ||
		result.Bundle.RedactionReceipt.BlockedContent["raw_model_output"] == 0 ||
		result.Bundle.RedactionReceipt.BlockedContent["raw_diff"] == 0 ||
		result.Bundle.RedactionReceipt.BlockedContent["raw_terminal_log"] == 0 ||
		result.Bundle.RedactionReceipt.BlockedContent["source_code"] == 0 {
		t.Fatalf("blocked content counts missing: %+v", result.Bundle.RedactionReceipt.BlockedContent)
	}
	if result.Bundle.RedactionReceipt.RedactedContent["secret"] == 0 {
		t.Fatalf("secret redaction count missing: %+v", result.Bundle.RedactionReceipt.RedactedContent)
	}
	if result.Bundle.RedactionReceipt.RedactionGuaranteed != true {
		t.Fatalf("redaction not guaranteed: %+v", result.Bundle.RedactionReceipt)
	}
	if result.Bundle.EvidenceUpload.Status != "not_uploaded" {
		t.Fatalf("upload status = %+v, want not_uploaded", result.Bundle.EvidenceUpload)
	}
	assertBundleDoesNotContain(t, result.Bundle, head)
	if result.Bundle.Repo.CurrentCommitSHAHash == "" || result.Bundle.Repo.CurrentCommitSHAHash == head {
		t.Fatalf("bundle current commit hash = %+v, want hashed anchor", result.Bundle.Repo)
	}
	assertNoRawFixtureContent(t, result.Bundle)
	assertFileExists(t, result.BundlePath)
	assertFileExists(t, result.RedactionReceiptPath)
}

func TestPreviewReportsFoundLinkedAndHeuristicConfidence(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := newEvidenceRepo(t)
	claudeDir, codexDir := writeEvidenceFixtures(t, repo)
	writeUnrelatedCodexSession(t, codexDir)
	writeHeuristicCodexSession(t, codexDir, "fixture-repo")

	result, err := Preview(context.Background(), Options{
		Repo:      repo,
		ClaudeDir: claudeDir,
		CodexDir:  codexDir,
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if result.SessionsFound != 4 {
		t.Fatalf("sessions found = %d, want 4", result.SessionsFound)
	}
	if result.SessionsLinked != 3 {
		t.Fatalf("sessions linked = %d, want 3", result.SessionsLinked)
	}
	if result.SessionsSkipped != 1 {
		t.Fatalf("sessions skipped = %d, want 1", result.SessionsSkipped)
	}
	if result.FieldsExtracted == 0 || result.FieldsBlocked == 0 {
		t.Fatalf("field counts missing: %+v", result)
	}
	foundLowConfidence := false
	for _, session := range result.Bundle.WorkSessions {
		for _, note := range session.LinkageNotes {
			if note.Confidence == "low" && strings.Contains(note.Note, "repo name") {
				foundLowConfidence = true
			}
		}
	}
	if !foundLowConfidence {
		t.Fatalf("heuristic low-confidence linkage note missing: %+v", result.Bundle.WorkSessions)
	}
}

func TestIncludeFilePathsRequiresExplicitFlag(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := newEvidenceRepo(t)
	claudeDir, codexDir := writeEvidenceFixtures(t, repo)

	result, err := Export(context.Background(), Options{
		Repo:             repo,
		Output:           t.TempDir(),
		ClaudeDir:        claudeDir,
		CodexDir:         codexDir,
		IncludeFilePaths: true,
	})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	for _, session := range result.Bundle.WorkSessions {
		if session.FilesTouchedCount > 0 && len(session.FilePaths) == 0 {
			t.Fatalf("explicit file path export omitted paths: %+v", session)
		}
	}
}

func TestUploadIsDisabled(t *testing.T) {
	err := Upload(context.Background(), Options{})
	if err == nil {
		t.Fatal("Upload() error = nil, want disabled upload error")
	}
	if !strings.Contains(err.Error(), "disabled until the CLI consumes a finalized website receiving contract") {
		t.Fatalf("upload error = %v", err)
	}
}

func TestEvidenceRejectsRemoteRepoToStayOffline(t *testing.T) {
	_, err := Preview(context.Background(), Options{Repo: "https://github.com/acme/fixture-repo.git"})
	if err == nil {
		t.Fatal("Preview() error = nil, want remote repo rejection")
	}
	if !strings.Contains(err.Error(), "requires a local git repository path") {
		t.Fatalf("remote repo error = %v", err)
	}
}

func TestPromptLikeGenericSummariesAreBlocked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := newEvidenceRepo(t)
	head := strings.TrimSpace(gitOutput(t, repo, "rev-parse", "HEAD"))
	codexDir := filepath.Join(t.TempDir(), "codex", "sessions")
	writeEvidenceFile(t, filepath.Join(codexDir, "prompt-summary.jsonl"), strings.Join([]string{
		`{"timestamp":"2026-06-01T11:00:00Z","type":"session_meta","payload":{"cwd":"` + slash(repo) + `","branch":"main","commit_sha":"` + head + `"}}`,
		`{"timestamp":"2026-06-01T11:01:00Z","type":"user_message","summary":"raw user prompt summary must not leak","payload":{"content":"normal prompt content"}}`,
		`{"timestamp":"2026-06-01T11:02:00Z","type":"assistant","title":"raw assistant title must not leak","payload":{"content":"normal assistant output"}}`,
		`{"timestamp":"2026-06-01T11:03:00Z","intent_summary":"raw content-adjacent summary must not leak","content":"normal prompt content without event type"}`,
		`{"timestamp":"2026-06-01T11:04:00Z","type":"agent_reasoning","payload":{"intent_summary":"Safe derived summary"}}`,
	}, "\n")+"\n")

	result, err := Export(context.Background(), Options{
		Repo:     repo,
		Output:   t.TempDir(),
		CodexDir: codexDir,
		Sources:  []string{"codex"},
	})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	assertBundleDoesNotContain(t, result.Bundle, "raw user prompt summary must not leak")
	assertBundleDoesNotContain(t, result.Bundle, "raw assistant title must not leak")
	assertBundleDoesNotContain(t, result.Bundle, "raw content-adjacent summary must not leak")
	if len(result.Bundle.WorkSessions) != 1 || result.Bundle.WorkSessions[0].IntentSummary != "Safe derived summary" {
		t.Fatalf("summary = %+v, want only explicit derived summary", result.Bundle.WorkSessions)
	}
}

func newEvidenceRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "fixture-repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	runEvidenceGit(t, repo, "init", "-b", "main")
	runEvidenceGit(t, repo, "config", "user.email", "cli@example.test")
	runEvidenceGit(t, repo, "config", "user.name", "CLI Test")
	writeEvidenceFile(t, filepath.Join(repo, "cmd", "app.go"), "package main\n")
	runEvidenceGit(t, repo, "add", ".")
	runEvidenceGit(t, repo, "commit", "-m", "ENG-123 initial fixture")
	runEvidenceGit(t, repo, "remote", "add", "origin", "https://github.com/acme/fixture-repo.git")
	return repo
}

func writeEvidenceFixtures(t *testing.T, repo string) (string, string) {
	t.Helper()
	head := strings.TrimSpace(gitOutput(t, repo, "rev-parse", "HEAD"))
	claudeDir := filepath.Join(t.TempDir(), "claude", "projects")
	codexDir := filepath.Join(t.TempDir(), "codex", "sessions")
	claudeProject := filepath.Join(claudeDir, fixtureClaudeProjectName(repo))
	if err := os.MkdirAll(claudeProject, 0o750); err != nil {
		t.Fatalf("mkdir claude project: %v", err)
	}
	writeEvidenceFile(t, filepath.Join(claudeProject, "session.jsonl"), strings.Join([]string{
		`{"timestamp":"2026-06-01T10:00:00Z","type":"user","cwd":"` + slash(repo) + `","branch":"main","commit_sha":"` + head + `","message":{"role":"user","content":"Implement ENG-123 with API_TOKEN=super-secret; raw prompt must not leak."}}`,
		`{"timestamp":"2026-06-01T10:01:00Z","type":"assistant","message":{"role":"assistant","content":"Raw model output must not leak."}}`,
		`{"timestamp":"2026-06-01T10:02:00Z","type":"tool_use","name":"Edit","input":{"file_path":"cmd/app.go","diff":"@@ raw diff must not leak","content":"package main\nfunc main() {}"}}`,
		`{"timestamp":"2026-06-01T10:03:00Z","type":"tool_result","content":"go test ./... failed with terminal log"}}`,
	}, "\n")+"\n")

	writeEvidenceFile(t, filepath.Join(codexDir, "2026", "06", "01", "session.jsonl"), strings.Join([]string{
		`{"timestamp":"2026-06-01T11:00:00Z","type":"session_meta","payload":{"cwd":"` + slash(repo) + `","branch":"main","commit_sha":"` + head + `","repo_remote":"https://github.com/acme/fixture-repo.git"}}`,
		`{"timestamp":"2026-06-01T11:00:30Z","type":"user_message","payload":{"content":"Run the ENG-123 implementation; raw prompt must not leak."}}`,
		`{"timestamp":"2026-06-01T11:01:00Z","type":"agent_reasoning","payload":{"intent_summary":"Implement ENG-123 derived summary","plan_summary":"Wire the evidence command","implementation_summary":"Updated command wiring and tests"}}`,
		`{"timestamp":"2026-06-01T11:02:00Z","type":"exec_command","payload":{"cmd":"go test ./...","stdout":"terminal log with PASSWORD=super-secret"}}`,
		`{"timestamp":"2026-06-01T11:03:00Z","type":"apply_patch","payload":{"path":"internal/evidence/evidence.go","patch":"@@ raw diff must not leak"}}`,
	}, "\n")+"\n")
	return claudeDir, codexDir
}

func writeUnrelatedCodexSession(t *testing.T, codexDir string) {
	t.Helper()
	writeEvidenceFile(t, filepath.Join(codexDir, "2026", "06", "02", "unrelated.jsonl"), strings.Join([]string{
		`{"timestamp":"2026-06-02T10:00:00Z","type":"session_meta","payload":{"cwd":"/tmp/other-repo","branch":"main","repo_remote":"https://github.com/acme/other-repo.git"}}`,
		`{"timestamp":"2026-06-02T10:01:00Z","type":"agent_reasoning","payload":{"intent_summary":"Unrelated work"}}`,
	}, "\n")+"\n")
}

func writeHeuristicCodexSession(t *testing.T, codexDir string, repoName string) {
	t.Helper()
	writeEvidenceFile(t, filepath.Join(codexDir, "2026", "06", "03", "heuristic.jsonl"), strings.Join([]string{
		`{"timestamp":"2026-06-03T10:00:00Z","type":"session_meta","payload":{"repo_name":"` + repoName + `"}}`,
		`{"timestamp":"2026-06-03T10:01:00Z","type":"assistant","payload":{"tool":"edit","path":"cmd/heuristic.go"}}`,
	}, "\n")+"\n")
}

func assertNoRawFixtureContent(t *testing.T, bundle AIWorkEvidenceBundle) {
	t.Helper()
	for _, blocked := range []string{
		"super-secret",
		"raw prompt must not leak",
		"Raw model output must not leak",
		"@@ raw diff must not leak",
		"terminal log with",
		"package main",
		"cmd/app.go",
		"internal/evidence/evidence.go",
		"acme/fixture-repo",
	} {
		assertBundleDoesNotContain(t, bundle, blocked)
	}
}

func assertBundleDoesNotContain(t *testing.T, bundle AIWorkEvidenceBundle, blocked string) {
	t.Helper()
	data, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	if strings.Contains(string(data), blocked) {
		t.Fatalf("bundle leaked %q:\n%s", blocked, string(data))
	}
}

func fixtureClaudeProjectName(repo string) string {
	return "-" + strings.ReplaceAll(strings.Trim(slash(repo), "/"), "/", "-")
}

func slash(path string) string {
	return filepath.ToSlash(path)
}

func runEvidenceGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	// #nosec G204 -- tests invoke a fixed git binary with fixture arguments.
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	// #nosec G204 -- tests invoke a fixed git binary with fixture arguments.
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}

func writeEvidenceFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %s to exist: %v", path, err)
	}
}
