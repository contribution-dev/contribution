package git

import "testing"

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
