package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

func TestRedactRemoteURL(t *testing.T) {
	tokenKey := "to" + "ken"
	passwordKey := "pass" + "word"
	redactMe := "redact-me"
	tests := map[string]string{
		"https://" + tokenKey + "=" + redactMe + "@github.com/owner/repo.git":     "https://REDACTED@github.com/owner/repo.git",
		"https://user:" + redactMe + "@github.com/owner/repo.git":                 "https://REDACTED@github.com/owner/repo.git",
		"ssh://git:" + redactMe + "@github.com/owner/repo.git":                    "ssh://REDACTED@github.com/owner/repo.git",
		"git@github.com:owner/repo.git":                                           "git@github.com:owner/repo.git",
		"https://github.com/owner/repo.git":                                       "https://github.com/owner/repo.git",
		"https://" + tokenKey + "=" + redactMe + "@[::1]/owner/repo.git":          "https://REDACTED@[::1]/owner/repo.git",
		"https://" + tokenKey + "=" + redactMe + "@127.0.0.1/owner/repo.git":      "https://REDACTED@127.0.0.1/owner/repo.git",
		"https://" + tokenKey + "=" + redactMe + "@example.test/owner/repo.git?a": "https://REDACTED@example.test/owner/repo.git?a",
		"https://example.test/owner/repo.git?" + tokenKey + "=" + redactMe:        "https://example.test/owner/repo.git?" + tokenKey + "=REDACTED",
		"https://example.test/owner/repo.git?x=" + tokenKey + "=" + redactMe:      "https://example.test/owner/repo.git?x=REDACTED",
		"https://example.test/owner/repo.git?" + passwordKey + "=" + redactMe:     "https://example.test/owner/repo.git?" + passwordKey + "=REDACTED",
	}
	for remote, want := range tests {
		if got := RedactRemoteURL(remote); got != want {
			t.Fatalf("RedactRemoteURL(%q) = %q, want %q", remote, got, want)
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
	}{
		{path: "internal/auth/session.go", class: "source", risky: true},
		{path: "internal/auth/session_test.go", class: "test", risky: true, test: true},
		{path: "docs/vision.md", class: "docs"},
		{path: "go.mod", class: "dependency", dependency: true},
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
	}
}
