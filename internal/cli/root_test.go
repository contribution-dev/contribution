package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/contribution-dev/contribution/internal/signals"
)

func TestRootCommandShowsHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr, BuildInfo{})
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got := stdout.String(); !strings.Contains(got, "Analyze agentic readiness from local repo evidence.") {
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

func TestInitCommandCreatesConfigAndIsIdempotent(t *testing.T) {
	setupGitPath(t)
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "trunk")
	runGit(t, repo, "config", "user.email", "cli@example.test")
	runGit(t, repo, "config", "user.name", "CLI Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# fixture\n"), 0o600); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "initial fixture")
	chdir(t, repo)

	stdout, stderr, err := executeForTest([]string{"init"}, BuildInfo{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Created ") || !strings.Contains(stdout, "Next:") {
		t.Fatalf("init stdout = %q, want creation guidance", stdout)
	}
	configPath := filepath.Join(repo, ".contribution.yml")
	// #nosec G304 -- test reads the generated config path inside a private temp repo.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "default_branch: trunk") {
		t.Fatalf("config missing detected branch:\n%s", string(data))
	}

	stdout, stderr, err = executeForTest([]string{"init"}, BuildInfo{})
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("second stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, ".contribution.yml already exists") {
		t.Fatalf("second init stdout = %q, want idempotent message", stdout)
	}
}

func TestDoctorUsesRepoLocalOptionalTools(t *testing.T) {
	setupGitPath(t)
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	repoBin := filepath.Join(repo, ".tools", "bin")
	if err := os.MkdirAll(repoBin, 0o700); err != nil {
		t.Fatalf("mkdir repo bin: %v", err)
	}
	writeFakeExecutable(t, repoBin, "semgrep", "1.164.0\n")
	chdir(t, repo)

	stdout, stderr, err := executeForTest([]string{"doctor"}, BuildInfo{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "- semgrep: ok (optional) 1.164.0") {
		t.Fatalf("doctor stdout missing repo-local semgrep:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Install gitleaks and ensure it is on PATH") {
		t.Fatalf("doctor stdout missing generic optional-tool guidance:\n%s", stdout)
	}
	if strings.Contains(stdout, "scripts/with-tools") || strings.Contains(stdout, "pnpm tools:install:optional") {
		t.Fatalf("doctor stdout contains contribution-repo-only optional-tool guidance:\n%s", stdout)
	}
}

func TestDoctorSuggestsLCOVForPackageRepo(t *testing.T) {
	setupGitPath(t)
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "package.json"), []byte(`{"scripts":{"test":"vitest --coverage"}}`+"\n"), 0o600); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial fixture")
	chdir(t, repo)

	stdout, stderr, err := executeForTest([]string{"doctor"}, BuildInfo{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "coverage/lcov.info") || !strings.Contains(stdout, "--coverage-format lcov") {
		t.Fatalf("doctor stdout missing LCOV guidance for package repo:\n%s", stdout)
	}
	if strings.Contains(stdout, "go test ./... -coverprofile=coverage.out") {
		t.Fatalf("doctor stdout contains Go coverage guidance for package repo:\n%s", stdout)
	}
}

func TestPreflightCommandWritesJSONArtifacts(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "cli@example.test")
	runGit(t, repo, "config", "user.name", "CLI Test")
	if err := os.MkdirAll(filepath.Join(repo, "internal"), 0o750); err != nil {
		t.Fatalf("mkdir internal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "internal", "app.go"), []byte("package app\n"), 0o600); err != nil {
		t.Fatalf("write app.go: %v", err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial fixture")
	if err := os.WriteFile(filepath.Join(repo, "internal", "app.go"), []byte("package app\n\nfunc Value() int { return 1 }\n"), 0o600); err != nil {
		t.Fatalf("update app.go: %v", err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "add value")
	chdir(t, repo)

	outputRoot := t.TempDir()
	stdout, stderr, err := executeForTest([]string{"preflight", "--base", "HEAD~1", "--head", "HEAD", "--output", outputRoot, "--format", "json", "--no-external-tools"}, BuildInfo{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	for _, want := range []string{
		"Preflight: medium risk",
		"Scope: 1 files",
		"Tests: No test files changed. No coverage report was imported.",
		"Focus:",
		"- missing tests around changed behavior",
		"Data: ",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "Report:") || strings.Contains(stdout, "preflight.md") {
		t.Fatalf("stdout should not point at markdown for json output:\n%s", stdout)
	}
	outputDir := filepath.Dir(lineValue(t, stdout, "Data: "))
	// #nosec G304 -- test reads an artifact path created under a private temp dir.
	data, err := os.ReadFile(filepath.Join(outputDir, "preflight.json"))
	if err != nil {
		t.Fatalf("read preflight.json: %v", err)
	}
	var artifact map[string]any
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatalf("parse preflight.json: %v", err)
	}
	if artifact["version"] != float64(2) || artifact["head"] != "HEAD" {
		t.Fatalf("preflight artifact = %+v, want V2 HEAD artifact", artifact)
	}
}

func TestProbeCommandWritesCollectorBundle(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "cli@example.test")
	runGit(t, repo, "config", "user.name", "CLI Test")
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("# Agent guide\n"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "app.go"), []byte("package app\n"), 0o600); err != nil {
		t.Fatalf("write app.go: %v", err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "ENG-123 initial fixture")

	outputRoot := t.TempDir()
	stdout, stderr, err := executeForTest([]string{"probe", "--repo", repo, "--output", outputRoot, "--no-external-tools"}, BuildInfo{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	for _, want := range []string{"Agentic readiness report", "Top read:", "Next PR plan:", "Bundle: ", "Source coverage: ", "Attribution readiness: "} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("probe stdout missing %q:\n%s", want, stdout)
		}
	}
	bundlePath := lineValue(t, stdout, "Bundle: ")
	// #nosec G304 -- test reads the artifact path printed by the command under t.TempDir.
	data, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatalf("read collector bundle: %v", err)
	}
	var bundle map[string]any
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatalf("parse collector bundle: %v", err)
	}
	if bundle["top_read"] == nil || bundle["agentic_readiness"] == nil || bundle["source_coverage"] == nil || bundle["attribution_readiness"] == nil {
		t.Fatalf("collector bundle missing value-pipeline fields: %+v", bundle)
	}
}

func TestWorkUnitStartAndExportCommands(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "cli@example.test")
	runGit(t, repo, "config", "user.name", "CLI Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# fixture\n"), 0o600); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial fixture")

	stdout, stderr, err := executeForTest([]string{"work-unit", "start", "--repo", repo, "--goal", "Build onboarding", "--issue", "ENG-456"}, BuildInfo{})
	if err != nil {
		t.Fatalf("work-unit start error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	markerPath := lineValue(t, stdout, "Marker: ")
	assertFileExists(t, markerPath)

	outputRoot := t.TempDir()
	stdout, stderr, err = executeForTest([]string{"work-unit", "export", "--repo", repo, "--output", outputRoot}, BuildInfo{})
	if err != nil {
		t.Fatalf("work-unit export error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("export stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Work units: 1") {
		t.Fatalf("export stdout = %q, want one marker", stdout)
	}
	assertFileExists(t, filepath.Join(outputRoot, "work-units.json"))
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

func TestInvalidCoverageFormatFailsBeforeRepoAccess(t *testing.T) {
	stdout, stderr, err := executeForTest([]string{"preflight", "--coverage-format", "cobertura"}, BuildInfo{})
	if err == nil {
		t.Fatal("Execute() error = nil, want unsupported coverage format")
	}
	if !strings.Contains(err.Error(), `unsupported coverage format "cobertura"`) {
		t.Fatalf("error = %v, want unsupported coverage format", err)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("stdout/stderr = %q/%q, want empty", stdout, stderr)
	}
}

func TestInvalidFailOnRiskFailsBeforeRepoAccess(t *testing.T) {
	stdout, stderr, err := executeForTest([]string{"preflight", "--fail-on-risk", "low"}, BuildInfo{})
	if err == nil {
		t.Fatal("Execute() error = nil, want unsupported fail-on-risk")
	}
	if !strings.Contains(err.Error(), `unsupported fail-on-risk "low"`) {
		t.Fatalf("error = %v, want unsupported fail-on-risk", err)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("stdout/stderr = %q/%q, want empty", stdout, stderr)
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

func TestReportCommandPrintsShareHandoff(t *testing.T) {
	input := writeCLIAnalysisFixture(t)
	output := t.TempDir()

	stdout, stderr, err := executeForTest([]string{"report", "--input", input, "--output", output, "--format", "json"}, BuildInfo{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	for _, want := range []string{
		"Report artifacts written to " + output,
		"Shareable card (public-safe)",
		"Agentic readiness: B (82/100)",
		"Create image: https://contribution.dev/share",
		"Upload: " + filepath.Join(output, "profile.export.json"),
		"        " + filepath.Join(output, "share-card.json"),
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
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

func TestExportProfileCommandPrintsShareHandoff(t *testing.T) {
	input := writeCLIAnalysisFixture(t)
	output := t.TempDir()

	stdout, stderr, err := executeForTest([]string{"export-profile", "--input", input, "--output", output}, BuildInfo{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	for _, want := range []string{
		"Profile export artifacts written to " + output,
		"Shareable card (public-safe)",
		"Agentic readiness: B (82/100)",
		"Create image: https://contribution.dev/share",
		"Upload: " + filepath.Join(output, "profile.export.json"),
		"        " + filepath.Join(output, "share-card.json"),
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
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

func writeCLIAnalysisFixture(t *testing.T) string {
	t.Helper()
	analysis := signals.AnalysisReport{
		Version:     1,
		GeneratedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Profile: signals.ProfileSummary{
			DisplayName:        "fixture",
			AnalyzedPRs:        7,
			AnalysisWindowDays: 30,
			Confidence:         signals.ConfidenceMedium,
			Strengths: []signals.Finding{{
				Label:      "Focused local changes",
				Evidence:   "Recent changes stayed small.",
				Confidence: signals.ConfidenceMedium,
			}},
		},
		AgenticReadiness: signals.AgenticReadiness{
			Grade:      "B",
			Score:      82,
			Confidence: signals.ConfidenceMedium,
		},
		Privacy:        signals.PrivacySummary{PublicSafe: true},
		PrivacySummary: signals.PrivacySummary{PublicSafe: true},
	}
	data, err := json.Marshal(analysis)
	if err != nil {
		t.Fatalf("marshal analysis fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), "analysis.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write analysis fixture: %v", err)
	}
	return path
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

func TestImportFeedbackRequiresAnalysis(t *testing.T) {
	stdout, stderr, err := executeForTest([]string{"import-feedback"}, BuildInfo{})
	if err == nil {
		t.Fatal("Execute() error = nil, want missing analysis")
	}
	if !strings.Contains(err.Error(), "--analysis is required") {
		t.Fatalf("error = %v, want missing analysis", err)
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

func lineValue(t *testing.T, text string, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	t.Fatalf("missing line prefix %q in:\n%s", prefix, text)
	return ""
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %s to exist: %v", path, err)
	}
}

func setupGitPath(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell git path fixture is unix-only")
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	bin := t.TempDir()
	if err := os.Symlink(gitPath, filepath.Join(bin, "git")); err != nil {
		t.Fatalf("symlink git: %v", err)
	}
	t.Setenv("PATH", bin)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	// #nosec G204 -- test helper invokes the fixed git binary with fixture arguments.
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(old)
	})
}

func writeFakeExecutable(t *testing.T, bin string, name string, stdout string) {
	t.Helper()
	path := filepath.Join(bin, name)
	body := "#!/bin/sh\nprintf '%s' " + quoteShell(stdout) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
	// #nosec G302 -- test fake tools must be executable inside a private temp dir.
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatalf("chmod fake %s: %v", name, err)
	}
}

func quoteShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
