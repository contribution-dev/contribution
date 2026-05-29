package github

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestFetchMergedPRsFiltersMergedAndLimits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		if !strings.Contains(r.URL.RawQuery, "per_page=2") {
			t.Fatalf("query = %q, want per_page=2", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"number":1,"title":"open","html_url":"https://example.test/1","merged_at":null,"changed_files":9,"additions":1,"deletions":1},
			{"number":2,"title":"merged two","html_url":"https://example.test/2","merged_at":"2026-01-01T00:00:00Z","changed_files":2,"additions":10,"deletions":3},
			{"number":3,"title":"merged three","html_url":"https://example.test/3","merged_at":"2026-01-02T00:00:00Z","changed_files":3,"additions":20,"deletions":4}
		]`))
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
