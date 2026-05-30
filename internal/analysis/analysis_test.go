package analysis

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gitrepo "github.com/contribution-dev/contribution/internal/git"
	"github.com/contribution-dev/contribution/internal/github"
	"github.com/contribution-dev/contribution/internal/report"
	"github.com/contribution-dev/contribution/internal/signals"
)

func TestRunWritesJsonArtifactsAndLocalOnlyFallback(t *testing.T) {
	requireGit(t)
	withFixedNow(t, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")

	repoPath := newAnalysisRepo(t)
	writeTestFile(t, repoPath, ".contribution.yml", strings.Join([]string{
		"version: 1",
		"project:",
		"  name: fixture project",
		"analysis:",
		"  since_days: 14",
		"  max_prs: 1",
		"ai_usage:",
		"  self_reported_tools:",
		"    - codex",
		"  self_reported_modes:",
		"    - review",
		"",
	}, "\n"))
	runGit(t, repoPath, "add", ".")
	runGit(t, repoPath, "commit", "-m", "add config")

	outputRoot := t.TempDir()
	var stdout bytes.Buffer
	outputDir, err := Run(context.Background(), &stdout, Options{
		Repo:            repoPath,
		Output:          outputRoot,
		Format:          "json",
		NoExternalTools: true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	wantDir := filepath.Join(outputRoot, "2026-01-02T030405Z")
	if outputDir != wantDir {
		t.Fatalf("outputDir = %q, want %q", outputDir, wantDir)
	}
	for _, name := range []string{"analysis.json", "profile.export.json", "share-card.json", "tooling.json"} {
		assertFileExists(t, filepath.Join(outputDir, name))
	}
	assertFileMissing(t, filepath.Join(outputDir, "report.md"))

	analysis, err := report.ReadAnalysis(filepath.Join(outputDir, "analysis.json"))
	if err != nil {
		t.Fatalf("ReadAnalysis() error = %v", err)
	}
	if analysis.Config.SinceDays != 14 || analysis.Config.MaxPRs != 1 {
		t.Fatalf("config snapshot = %+v, want config-driven since/max PRs", analysis.Config)
	}
	if analysis.Config.GitHubMetadataConfigured {
		t.Fatalf("GitHubMetadataConfigured = true, want false without token")
	}
	if analysis.Coverage.Status != "unknown" {
		t.Fatalf("coverage status = %q, want unknown without coverage input", analysis.Coverage.Status)
	}
	if analysis.Trends.Status == "" || analysis.Trends.CurrentWindow.Commits == 0 {
		t.Fatalf("trend comparison missing current-window evidence: %+v", analysis.Trends)
	}
	if len(analysis.DeepDives.NoTestArtifacts) != 0 {
		t.Fatalf("unexpected no-test deep dives: %+v", analysis.DeepDives.NoTestArtifacts)
	}
	if len(analysis.SetupActions) == 0 {
		t.Fatal("expected confidence setup actions")
	}
	assertContains(t, analysis.Limitations, "GitHub metadata was not requested; continuing local-only.")
	got := stdout.String()
	for _, want := range []string{
		"GitHub metadata: unavailable, continuing local-only",
		"Contribution receipt",
		"Artifacts: 1 recent artifacts over 14 days",
		"Confidence: low",
		"Strength:",
		"Risk:",
		"Next:",
		"Unavailable:",
		"Optional tool signals unavailable:",
		"Data: " + filepath.Join(outputDir, "analysis.json"),
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("stdout missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{
		"Report:",
		"report.md",
		"scc was skipped",
		"semgrep was skipped",
		"gitleaks was skipped",
		"osv-scanner was skipped",
		"trivy was skipped",
		"dogfood@example.test",
	} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("stdout contains %q:\n%s", unwanted, got)
		}
	}
}

func TestRunImportsAnalyzeCoverage(t *testing.T) {
	requireGit(t)
	withFixedNow(t, time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC))
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")

	repoPath := newAnalysisRepo(t)
	coveragePath := filepath.Join(t.TempDir(), "coverage.out")
	if err := os.WriteFile(coveragePath, []byte("mode: set\ninternal/app.go:3.1,3.30 1 1\n"), 0o600); err != nil {
		t.Fatalf("write coverage: %v", err)
	}
	outputRoot := t.TempDir()
	var stdout bytes.Buffer
	outputDir, err := Run(context.Background(), &stdout, Options{
		Repo:            repoPath,
		Output:          outputRoot,
		Format:          "json",
		NoExternalTools: true,
		CoveragePaths:   []string{coveragePath},
		CoverageFormat:  "go",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	analysis, err := report.ReadAnalysis(filepath.Join(outputDir, "analysis.json"))
	if err != nil {
		t.Fatalf("ReadAnalysis() error = %v", err)
	}
	if analysis.Coverage.Status != "available" || analysis.Coverage.Percent != 100 {
		t.Fatalf("coverage = %+v, want available 100%%", analysis.Coverage)
	}
	if !hasSignalType(analysis.Signals, "coverage_line_percent") {
		t.Fatalf("coverage signal missing: %+v", analysis.Signals)
	}
}

func TestRunImportsConfiguredCoverageWhenPresent(t *testing.T) {
	requireGit(t)
	withFixedNow(t, time.Date(2026, 3, 4, 5, 6, 8, 0, time.UTC))
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")

	repoPath := newAnalysisRepo(t)
	writeTestFile(t, repoPath, ".contribution.yml", strings.Join([]string{
		"version: 1",
		"coverage:",
		"  path: coverage.out",
		"  format: go",
		"",
	}, "\n"))
	writeTestFile(t, repoPath, "coverage.out", "mode: set\ninternal/app.go:3.1,3.30 1 1\n")
	outputRoot := t.TempDir()
	var stdout bytes.Buffer
	outputDir, err := Run(context.Background(), &stdout, Options{
		Repo:            repoPath,
		Output:          outputRoot,
		Format:          "json",
		NoExternalTools: true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	analysis, err := report.ReadAnalysis(filepath.Join(outputDir, "analysis.json"))
	if err != nil {
		t.Fatalf("ReadAnalysis() error = %v", err)
	}
	if analysis.Coverage.Status != "available" || analysis.Coverage.Percent != 100 {
		t.Fatalf("coverage = %+v, want configured coverage imported", analysis.Coverage)
	}
}

func TestRunMarkdownWritesCanonicalAnalysisWhenGitHubFetchFails(t *testing.T) {
	requireGit(t)
	withFixedNow(t, time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC))
	oldFetch := fetchMergedPRs
	fetchMergedPRs = func(_ context.Context, owner string, repo string, token string, maxPRs int) (github.Metadata, error) {
		if owner != "owner" || repo != "repo" || token != "literal-token" || maxPRs != 20 {
			t.Fatalf("FetchMergedPRs args = owner=%q repo=%q token=%q maxPRs=%d", owner, repo, token, maxPRs)
		}
		return github.Metadata{}, errors.New("network unavailable")
	}
	t.Cleanup(func() {
		fetchMergedPRs = oldFetch
	})

	repoPath := newAnalysisRepo(t)
	runGit(t, repoPath, "remote", "add", "origin", "https://github.com/owner/repo.git")
	outputRoot := t.TempDir()
	var stdout bytes.Buffer
	outputDir, err := Run(context.Background(), &stdout, Options{
		Repo:            repoPath,
		Output:          outputRoot,
		Format:          "markdown",
		GitHubToken:     "literal-token",
		NoExternalTools: true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertFileExists(t, filepath.Join(outputDir, "report.md"))
	assertFileExists(t, filepath.Join(outputDir, "analysis.json"))

	analysis, err := report.ReadAnalysis(filepath.Join(outputDir, "analysis.json"))
	if err != nil {
		t.Fatalf("ReadAnalysis() error = %v", err)
	}
	if !analysis.Config.GitHubMetadataConfigured {
		t.Fatalf("GitHubMetadataConfigured = false, want true when token is configured")
	}
	assertContains(t, analysis.Limitations, "GitHub metadata failed: network unavailable")
	if !strings.Contains(stdout.String(), "GitHub metadata: requested") {
		t.Fatalf("stdout missing requested metadata message:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Report: "+filepath.Join(outputDir, "report.md")) {
		t.Fatalf("stdout missing markdown completion message:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Data: "+filepath.Join(outputDir, "analysis.json")) {
		t.Fatalf("stdout missing canonical analysis path:\n%s", stdout.String())
	}
}

func TestRunWithZeroGitHubPRsSuggestsWiderWindow(t *testing.T) {
	requireGit(t)
	withFixedNow(t, time.Date(2026, 2, 3, 4, 5, 7, 0, time.UTC))
	oldFetch := fetchMergedPRs
	fetchMergedPRs = func(_ context.Context, owner string, repo string, token string, maxPRs int) (github.Metadata, error) {
		if owner != "owner" || repo != "repo" || token != "literal-token" || maxPRs != 20 {
			t.Fatalf("FetchMergedPRs args = owner=%q repo=%q token=%q maxPRs=%d", owner, repo, token, maxPRs)
		}
		return github.Metadata{Available: true, Reason: "GitHub returned no merged PRs in the requested window."}, nil
	}
	t.Cleanup(func() {
		fetchMergedPRs = oldFetch
	})

	repoPath := newAnalysisRepo(t)
	runGit(t, repoPath, "remote", "add", "origin", "https://github.com/owner/repo.git")
	outputRoot := t.TempDir()
	var stdout bytes.Buffer
	outputDir, err := Run(context.Background(), &stdout, Options{
		Repo:            repoPath,
		Output:          outputRoot,
		Format:          "json",
		GitHubToken:     "literal-token",
		NoExternalTools: true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	analysis, err := report.ReadAnalysis(filepath.Join(outputDir, "analysis.json"))
	if err != nil {
		t.Fatalf("ReadAnalysis() error = %v", err)
	}
	if analysis.Profile.Confidence == signals.ConfidenceHigh || analysis.WeaknessMap.Confidence == signals.ConfidenceHigh {
		t.Fatalf("confidence = profile %q weakness %q, want not high without imported PRs", analysis.Profile.Confidence, analysis.WeaknessMap.Confidence)
	}
	assertContains(t, analysis.Limitations, "GitHub returned no merged PRs in the requested window.")
	assertContains(t, analysis.Limitations, "Review burden is unavailable without imported PR review metadata.")
	if !hasSetupAction(analysis.SetupActions, "widen_github_metadata_window") {
		t.Fatalf("setup actions = %+v, want widen_github_metadata_window", analysis.SetupActions)
	}
}

func TestRunSuggestsLCOVCoverageForPackageRepo(t *testing.T) {
	requireGit(t)
	withFixedNow(t, time.Date(2026, 3, 4, 5, 6, 9, 0, time.UTC))
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")

	repoPath := newPackageAnalysisRepo(t)
	outputRoot := t.TempDir()
	var stdout bytes.Buffer
	outputDir, err := Run(context.Background(), &stdout, Options{
		Repo:            repoPath,
		Output:          outputRoot,
		Format:          "json",
		NoExternalTools: true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	analysis, err := report.ReadAnalysis(filepath.Join(outputDir, "analysis.json"))
	if err != nil {
		t.Fatalf("ReadAnalysis() error = %v", err)
	}
	action, ok := findSetupAction(analysis.SetupActions, "import_coverage")
	if !ok {
		t.Fatalf("setup actions = %+v, want import_coverage", analysis.SetupActions)
	}
	if !strings.Contains(action.Command, "coverage/lcov.info") || !strings.Contains(action.Command, "--coverage-format lcov") {
		t.Fatalf("coverage setup command = %q, want LCOV guidance", action.Command)
	}
	if strings.Contains(action.Command, "go test ./...") {
		t.Fatalf("coverage setup command = %q, want no Go test guidance for package repo", action.Command)
	}
}

func withFixedNow(t *testing.T, value time.Time) {
	t.Helper()
	oldNow := nowUTC
	nowUTC = value.UTC
	t.Cleanup(func() {
		nowUTC = oldNow
	})
}

func newAnalysisRepo(t *testing.T) string {
	t.Helper()
	repoPath := t.TempDir()
	runGit(t, repoPath, "init", "-b", "main")
	runGit(t, repoPath, "config", "user.email", "dogfood@example.test")
	runGit(t, repoPath, "config", "user.name", "Dogfood User")
	writeTestFile(t, repoPath, "README.md", "# fixture\n")
	writeTestFile(t, repoPath, "internal/app.go", "package app\n\nfunc value() int { return 1 }\n")
	writeTestFile(t, repoPath, "internal/app_test.go", "package app\n\nfunc TestValue() {}\n")
	runGit(t, repoPath, "add", ".")
	runGit(t, repoPath, "commit", "-m", "initial fixture")
	return repoPath
}

func newPackageAnalysisRepo(t *testing.T) string {
	t.Helper()
	repoPath := t.TempDir()
	runGit(t, repoPath, "init", "-b", "main")
	runGit(t, repoPath, "config", "user.email", "dogfood@example.test")
	runGit(t, repoPath, "config", "user.name", "Dogfood User")
	writeTestFile(t, repoPath, "README.md", "# package fixture\n")
	writeTestFile(t, repoPath, "package.json", `{"scripts":{"test":"vitest --coverage"}}`+"\n")
	writeTestFile(t, repoPath, "src/app.ts", "export function value() { return 1 }\n")
	writeTestFile(t, repoPath, "src/app.test.ts", "import { value } from './app'\nvalue()\n")
	runGit(t, repoPath, "add", ".")
	runGit(t, repoPath, "commit", "-m", "initial fixture")
	return repoPath
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	// #nosec G204 -- tests execute the fixed git binary with test-controlled args.
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func writeTestFile(t *testing.T, root string, rel string, content string) {
	t.Helper()
	target := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(target), err)
	}
	if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("%s was not written", path)
	}
}

func assertFileMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s exists, want missing", path)
	}
}

func assertContains(t *testing.T, values []string, needle string) {
	t.Helper()
	for _, value := range values {
		if value == needle {
			return
		}
	}
	t.Fatalf("%q not found in %v", needle, values)
}

func hasSignalType(values []signals.Signal, signalType string) bool {
	for _, value := range values {
		if value.Type == signalType {
			return true
		}
	}
	return false
}

func hasSetupAction(values []signals.SetupAction, id string) bool {
	_, ok := findSetupAction(values, id)
	return ok
}

func findSetupAction(values []signals.SetupAction, id string) (signals.SetupAction, bool) {
	for _, value := range values {
		if value.ID == id {
			return value, true
		}
	}
	return signals.SetupAction{}, false
}

func TestClassifyAnalyzerFindingScopes(t *testing.T) {
	got := classifyAnalyzerFindingScopes([]signals.AnalyzerFinding{{
		Tool:     "semgrep",
		FilePath: "internal/app.go",
	}, {
		Tool:     "semgrep",
		FilePath: "internal/old.go",
	}}, gitrepo.History{FileTouchCount: map[string]int{"internal/app.go": 2}})

	if got[0].Scope != "recently_touched" || got[1].Scope != "repo_existing_or_unknown" {
		t.Fatalf("scopes = %+v", got)
	}
}
