package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "gh-env")
	t.Setenv("CUSTOM_TOKEN", "custom-env")

	tests := []struct {
		name      string
		flag      string
		wantToken string
		wantOK    bool
	}{
		{name: "literal", flag: "literal-token", wantToken: "literal-token", wantOK: true},
		{name: "env name", flag: "CUSTOM_TOKEN", wantToken: "custom-env", wantOK: true},
		{name: "env prefix", flag: "env:CUSTOM_TOKEN", wantToken: "custom-env", wantOK: true},
		{name: "missing env prefix", flag: "env:MISSING_TOKEN", wantOK: false},
		{name: "default env", flag: "", wantToken: "gh-env", wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ResolveToken(tt.flag)
			if ok != tt.wantOK || got != tt.wantToken {
				t.Fatalf("ResolveToken(%q) = %q/%v, want %q/%v", tt.flag, got, ok, tt.wantToken, tt.wantOK)
			}
		})
	}
}

func TestResolveTokenFromGitHubCLI(t *testing.T) {
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	if err := os.WriteFile(ghPath, []byte("#!/bin/sh\nprintf 'gh-cli-token\\n'\n"), 0o600); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	// #nosec G302 -- the test fixture must be executable and is private to t.TempDir.
	if err := os.Chmod(ghPath, 0o500); err != nil {
		t.Fatalf("chmod fake gh: %v", err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")

	got, ok := ResolveToken("gh")
	if !ok || got != "gh-cli-token" {
		t.Fatalf("ResolveToken(\"gh\") = %q/%v, want gh-cli-token/true", got, ok)
	}
}

func TestFetchMergedPRsFiltersMergedAndLimits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/repos/owner/repo/pulls":
			if !strings.Contains(r.URL.RawQuery, "per_page=4") {
				t.Fatalf("query = %q, want per_page=4", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[
				{"number":1,"title":"open","html_url":"https://example.test/1","merged_at":null},
				{"number":2,"title":"merged two","html_url":"https://example.test/2","merged_at":"2026-01-01T00:00:00Z","merge_commit_sha":"merge222"},
				{"number":3,"title":"merged three","html_url":"https://example.test/3","merged_at":"2026-01-02T00:00:00Z","merge_commit_sha":"merge333"}
			]`))
		case r.URL.Path == "/repos/owner/repo/pulls/2":
			_, _ = w.Write([]byte(`{"number":2,"title":"merged two","html_url":"https://example.test/2","merged_at":"2026-01-01T00:00:00Z","changed_files":2,"additions":10,"deletions":3,"commits":2,"comments":4,"review_comments":5,"merge_commit_sha":"merge222detail","head":{"sha":"abcdef123456"}}`))
		case r.URL.Path == "/repos/owner/repo/pulls/3":
			_, _ = w.Write([]byte(`{"number":3,"title":"merged three","html_url":"https://example.test/3","changed_files":3,"additions":20,"deletions":4,"commits":1,"merge_commit_sha":"merge333detail","head":{"sha":"123456abcdef"}}`))
		case r.URL.Path == "/repos/owner/repo/pulls/2/files":
			_, _ = w.Write([]byte(`[{"filename":"internal/app.go"},{"filename":"internal/app_test.go"}]`))
		case r.URL.Path == "/repos/owner/repo/pulls/3/files":
			_, _ = w.Write([]byte(`[{"filename":"internal/auth/session.go"}]`))
		case r.URL.Path == "/repos/owner/repo/pulls/2/reviews":
			_, _ = w.Write([]byte(`[{"state":"APPROVED"},{"state":"CHANGES_REQUESTED"}]`))
		case r.URL.Path == "/repos/owner/repo/pulls/3/reviews":
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			_, _ = w.Write([]byte(`{"check_runs":[{"conclusion":"success"},{"conclusion":"failure"}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	restoreGitHubTestClient(server.URL)
	defer restoreGitHubTestClient("")

	got, err := FetchMergedPRs(context.Background(), "owner", "repo", "token", 2)
	if err != nil {
		t.Fatalf("FetchMergedPRs() error = %v", err)
	}
	if !got.Available || got.Reason != "" {
		t.Fatalf("metadata availability = %v reason %q, want available", got.Available, got.Reason)
	}
	if len(got.PRs) != 2 || got.PRs[0].Number != 2 || got.PRs[1].Number != 3 {
		t.Fatalf("PRs = %+v, want merged PRs 2 and 3", got.PRs)
	}
	if len(got.PRs[0].Files) != 2 || got.PRs[0].ReviewCount != 2 || got.PRs[0].RequestedChanges != 1 || got.PRs[0].CheckRuns != 2 || got.PRs[0].FailedChecks != 1 {
		t.Fatalf("PR enrichment missing: %+v", got.PRs[0])
	}
	if got.PRs[0].ChangedFiles != 2 || got.PRs[0].Additions != 10 || got.PRs[0].Deletions != 3 || got.PRs[0].IssueComments != 4 || got.PRs[0].ReviewComments != 5 {
		t.Fatalf("PR detail counts missing: %+v", got.PRs[0])
	}
	if got.PRs[0].MergedAt.IsZero() {
		t.Fatalf("MergedAt was not imported: %+v", got.PRs[0])
	}
	if got.PRs[0].MergeCommitSHA != "merge222detail" || got.PRs[1].MergeCommitSHA != "merge333detail" {
		t.Fatalf("MergeCommitSHA was not imported: %+v", got.PRs)
	}
}

func TestFetchMergedPRsDegradesForMissingInputsAndHTTPStatus(t *testing.T) {
	if got, err := FetchMergedPRs(context.Background(), "", "repo", "token", 1); err != nil || got.Reason == "" {
		t.Fatalf("missing owner = %+v/%v, want degradation reason", got, err)
	}
	if got, err := FetchMergedPRs(context.Background(), "owner", "repo", "", 1); err != nil || got.Reason == "" {
		t.Fatalf("missing token = %+v/%v, want degradation reason", got, err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()
	restoreGitHubTestClient(server.URL)
	defer restoreGitHubTestClient("")

	got, err := FetchMergedPRs(context.Background(), "owner", "repo", "token", 1)
	if err != nil {
		t.Fatalf("FetchMergedPRs() error = %v", err)
	}
	if got.Available || !strings.Contains(got.Reason, "403") {
		t.Fatalf("metadata = %+v, want non-available 403 reason", got)
	}
}

func TestFetchMergedPRsReturnsMalformedJSONError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()
	restoreGitHubTestClient(server.URL)
	defer restoreGitHubTestClient("")

	if _, err := FetchMergedPRs(context.Background(), "owner", "repo", "token", 1); err == nil {
		t.Fatal("FetchMergedPRs() error = nil, want decode error")
	}
}

func restoreGitHubTestClient(baseURL string) {
	if baseURL == "" {
		httpClient = http.DefaultClient
		githubAPIBaseURL = "https://api.github.com"
		return
	}
	httpClient = http.DefaultClient
	githubAPIBaseURL = baseURL
}
