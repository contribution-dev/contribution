package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseGitHubRepo(t *testing.T) {
	tests := map[string][2]string{
		"https://github.com/owner/repo.git": {"owner", "repo"},
		"git@github.com:owner/repo.git":     {"owner", "repo"},
		"ssh://git@github.com/owner/repo":   {"owner", "repo"},
	}
	for remote, want := range tests {
		owner, repo := ParseGitHubRepo(remote)
		if owner != want[0] || repo != want[1] {
			t.Fatalf("ParseGitHubRepo(%q) = %q/%q, want %q/%q", remote, owner, repo, want[0], want[1])
		}
	}
}

func TestResolveRedactsCredentialedOrigin(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repoPath := t.TempDir()
	runGit(t, repoPath, "init", "-b", "main")
	runGit(t, repoPath, "config", "user.email", "dogfood@example.test")
	runGit(t, repoPath, "config", "user.name", "Dogfood User")
	readme := filepath.Join(repoPath, "README.md")
	if err := os.WriteFile(readme, []byte("# fixture\n"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repoPath, "add", ".")
	runGit(t, repoPath, "commit", "-m", "initial fixture")
	secret := "dogfood-secret-value"
	remote := "https://token=" + secret + "@github.com/owner/private.git"
	runGit(t, repoPath, "remote", "add", "origin", remote)

	repo, err := Resolve(context.Background(), repoPath)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if strings.Contains(repo.RemoteURL, secret) {
		t.Fatalf("remote URL retained secret: %q", repo.RemoteURL)
	}
	if !strings.Contains(repo.RemoteURL, "REDACTED") {
		t.Fatalf("remote URL missing redaction marker: %q", repo.RemoteURL)
	}
	if repo.GitHubOwner != "owner" || repo.GitHubRepo != "private" {
		t.Fatalf("GitHub metadata = %q/%q, want owner/private", repo.GitHubOwner, repo.GitHubRepo)
	}
}

func TestResolveRedactsCloneFailureOutput(t *testing.T) {
	bin := t.TempDir()
	fakeGit := filepath.Join(bin, "git")
	script := "#!/bin/sh\nprintf 'fatal: unable to access %s: authentication failed\\n' \"$5\" >&2\nexit 1\n"
	// #nosec G306 -- this test writes an executable fake git binary.
	if err := os.WriteFile(fakeGit, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", bin)
	secret := "dogfood-secret-value"
	remote := "https://example.test/owner/repo.git?token=" + secret

	_, err := Resolve(context.Background(), remote)
	if err == nil {
		t.Fatal("Resolve() error = nil, want clone failure")
	}
	got := err.Error()
	if strings.Contains(got, secret) {
		t.Fatalf("clone error retained secret: %q", got)
	}
	if !strings.Contains(got, "token=REDACTED") {
		t.Fatalf("clone error missing redacted query marker: %q", got)
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

func TestClassifyPath(t *testing.T) {
	tests := []struct {
		path       string
		class      string
		risky      bool
		test       bool
		dependency bool
		config     bool
		language   string
	}{
		{path: "internal/auth/session.go", class: "source", risky: true},
		{path: "internal/auth/session_test.go", class: "test", risky: true, test: true},
		{path: "scripts/codex-review-hooks.test.mjs", class: "test", test: true, language: "JavaScript"},
		{path: "docs/vision.md", class: "docs"},
		{path: "go.mod", class: "dependency", dependency: true},
		{path: ".contribution.yml", class: "config", config: true, language: "YAML"},
		{path: "LICENSE", class: "config", config: true, language: "Other"},
		{path: "lint-staged.config.js", class: "config", config: true, language: "JavaScript"},
		{path: "scripts/codex-review-worker", class: "source", language: "Shell"},
		{path: "vendor/example/file.go", class: "vendor"},
		{path: ".github/workflows/ci.yml", class: "infrastructure"},
	}
	for _, tt := range tests {
		got := ClassifyPath(tt.path)
		if got.Class != tt.class {
			t.Fatalf("ClassifyPath(%q).Class = %q, want %q", tt.path, got.Class, tt.class)
		}
		if got.IsSecurityRelated != tt.risky {
			t.Fatalf("ClassifyPath(%q).IsSecurityRelated = %v, want %v", tt.path, got.IsSecurityRelated, tt.risky)
		}
		if got.IsTest != tt.test {
			t.Fatalf("ClassifyPath(%q).IsTest = %v, want %v", tt.path, got.IsTest, tt.test)
		}
		if got.IsDependency != tt.dependency {
			t.Fatalf("ClassifyPath(%q).IsDependency = %v, want %v", tt.path, got.IsDependency, tt.dependency)
		}
		if got.IsConfig != tt.config {
			t.Fatalf("ClassifyPath(%q).IsConfig = %v, want %v", tt.path, got.IsConfig, tt.config)
		}
		if tt.language != "" && got.Language != tt.language {
			t.Fatalf("ClassifyPath(%q).Language = %q, want %q", tt.path, got.Language, tt.language)
		}
	}
}

func TestInventoryUsesGitVisibleFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repoPath := t.TempDir()
	runGit(t, repoPath, "init", "-b", "main")
	runGit(t, repoPath, "config", "user.email", "dogfood@example.test")
	runGit(t, repoPath, "config", "user.name", "Dogfood User")
	writeTestFile(t, repoPath, ".gitignore", ".pnpm-store/\n.code-reviews/\n.tools/\nbin/\nnode_modules/\ndocs-shared/\n")
	writeTestFile(t, repoPath, "README.md", "# fixture\n")
	writeTestFile(t, repoPath, "internal/app.go", "package app\n")
	writeTestFile(t, repoPath, "internal/app_test.go", "package app\n")
	writeTestFile(t, repoPath, "lint-staged.config.js", "export default [];\n")
	writeTestFile(t, repoPath, "scripts/codex-review-worker", "#!/bin/sh\n")
	writeTestFile(t, repoPath, "deleted.txt", "deleted\n")
	runGit(t, repoPath, "add", ".")
	runGit(t, repoPath, "commit", "-m", "initial fixture")

	writeTestFile(t, repoPath, "internal/untracked.go", "package app\n")
	writeTestFile(t, repoPath, ".pnpm-store/store.json", "{}\n")
	writeTestFile(t, repoPath, ".code-reviews/report.json", "{}\n")
	writeTestFile(t, repoPath, ".tools/tool.json", "{}\n")
	writeTestFile(t, repoPath, "bin/contribution", "binary\n")
	writeTestFile(t, repoPath, "node_modules/pkg/index.js", "module.exports = {}\n")
	writeTestFile(t, repoPath, "docs-shared/vision.md", "private\n")
	if err := os.Remove(filepath.Join(repoPath, "deleted.txt")); err != nil {
		t.Fatalf("remove deleted tracked file: %v", err)
	}

	summary, _, err := Inventory(context.Background(), repoPath, "local:test", time.Now())
	if err != nil {
		t.Fatalf("Inventory() error = %v", err)
	}
	want := len(gitVisibleExistingFiles(t, repoPath))
	if summary.TotalFiles != want {
		t.Fatalf("TotalFiles = %d, want git-visible existing files %d", summary.TotalFiles, want)
	}
	if summary.SourceFiles != 3 {
		t.Fatalf("SourceFiles = %d, want 3", summary.SourceFiles)
	}
	if summary.TestFiles != 1 {
		t.Fatalf("TestFiles = %d, want 1", summary.TestFiles)
	}
	if summary.ConfigFiles < 2 {
		t.Fatalf("ConfigFiles = %d, want at least .gitignore and lint-staged config", summary.ConfigFiles)
	}
}

func TestParseHistoryUsesNumstat(t *testing.T) {
	out := strings.Join([]string{
		"@@@abcdef1234567890\t2026-05-28T00:00:00Z\tadd app",
		"10\t2\tinternal/app.go",
		"1\t0\tinternal/app_test.go",
		"-\t-\tassets/logo.png",
	}, "\n")
	history := parseHistory(out, 10)
	if len(history.Commits) != 1 {
		t.Fatalf("commits = %d, want 1", len(history.Commits))
	}
	files := history.Commits[0].Files
	if len(files) != 3 {
		t.Fatalf("files = %d, want 3", len(files))
	}
	if files[0].Path != "internal/app.go" || files[0].Additions != 10 || files[0].Deletions != 2 {
		t.Fatalf("first numstat file = %+v", files[0])
	}
	if files[2].Additions != 0 || files[2].Deletions != 0 {
		t.Fatalf("binary numstat counts = %+v, want zero counts", files[2])
	}
	if !history.Commits[0].SourceTouched || !history.Commits[0].TestsTouched {
		t.Fatalf("classification flags were not populated: %+v", history.Commits[0])
	}
}

func TestParseUnifiedChangedLineRanges(t *testing.T) {
	out := strings.Join([]string{
		"diff --git a/internal/app.go b/internal/app.go",
		"--- a/internal/app.go",
		"+++ b/internal/app.go",
		"@@ -10,0 +11,2 @@",
		"+one",
		"+two",
		"@@ -20 +22 @@",
		"-old",
		"+new",
		"diff --git a/deleted.go b/deleted.go",
		"--- a/deleted.go",
		"+++ /dev/null",
		"@@ -1 +0,0 @@",
	}, "\n")

	got := parseUnifiedChangedLineRanges(out)
	ranges := got["internal/app.go"]
	if len(ranges) != 2 {
		t.Fatalf("ranges = %+v, want 2 ranges", ranges)
	}
	if ranges[0].Start != 11 || ranges[0].End != 12 {
		t.Fatalf("first range = %+v, want 11-12", ranges[0])
	}
	if ranges[1].Start != 22 || ranges[1].End != 22 {
		t.Fatalf("second range = %+v, want 22-22", ranges[1])
	}
	if _, ok := got["deleted.go"]; ok {
		t.Fatalf("deleted file received new-side ranges: %+v", got["deleted.go"])
	}
}

func TestDiffRejectsOptionLikeRefs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repoPath := t.TempDir()
	runGit(t, repoPath, "init", "-b", "main")
	runGit(t, repoPath, "config", "user.email", "dogfood@example.test")
	runGit(t, repoPath, "config", "user.name", "Dogfood User")
	writeTestFile(t, repoPath, "README.md", "# fixture\n")
	runGit(t, repoPath, "add", ".")
	runGit(t, repoPath, "commit", "-m", "initial fixture")

	if _, err := Diff(context.Background(), repoPath, "--stat", "HEAD"); err == nil {
		t.Fatal("Diff() error = nil, want invalid ref error")
	} else if !strings.Contains(err.Error(), "invalid git ref") {
		t.Fatalf("Diff() error = %v, want invalid git ref", err)
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

func gitVisibleExistingFiles(t *testing.T, repoPath string) []string {
	t.Helper()
	// #nosec G204 -- tests execute the fixed git binary with test-controlled args.
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	var files []string
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(repoPath, filepath.FromSlash(rel))); err == nil {
			files = append(files, rel)
		}
	}
	return files
}
