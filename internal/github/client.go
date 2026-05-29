// Package github provides optional V1 GitHub metadata enrichment.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Metadata contains optional GitHub PR enrichment.
type Metadata struct {
	Available bool
	Reason    string
	PRs       []PullRequest
}

// PullRequest is the subset of GitHub PR metadata V1 uses.
type PullRequest struct {
	Number       int
	Title        string
	URL          string
	ChangedFiles int
	Additions    int
	Deletions    int
}

var (
	httpClient       = http.DefaultClient
	githubAPIBaseURL = "https://api.github.com"
)

// ResolveToken treats the flag value as either a literal token or env var name.
func ResolveToken(flagValue string) (string, bool) {
	flagValue = strings.TrimSpace(flagValue)
	if flagValue != "" {
		if value := os.Getenv(flagValue); value != "" {
			return value, true
		}
		if strings.HasPrefix(flagValue, "env:") {
			if value := os.Getenv(strings.TrimPrefix(flagValue, "env:")); value != "" {
				return value, true
			}
			return "", false
		}
		return flagValue, true
	}
	for _, name := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if value := os.Getenv(name); value != "" {
			return value, true
		}
	}
	return "", false
}

// FetchMergedPRs fetches recent merged pull requests. It is enrichment only.
func FetchMergedPRs(ctx context.Context, owner, repo, token string, maxPRs int) (Metadata, error) {
	if owner == "" || repo == "" {
		return Metadata{Reason: "Repository is not a GitHub remote."}, nil
	}
	if token == "" {
		return Metadata{Reason: "No GitHub token was provided; review burden is unavailable."}, nil
	}
	if maxPRs <= 0 {
		maxPRs = 20
	}
	url := fmt.Sprintf("%s/repos/%s/%s/pulls?state=closed&sort=updated&direction=desc&per_page=%d", githubAPIBaseURL, owner, repo, maxPRs)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Metadata{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return Metadata{}, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Metadata{Reason: fmt.Sprintf("GitHub API returned %s.", resp.Status)}, nil
	}
	var raw []struct {
		Number       int        `json:"number"`
		Title        string     `json:"title"`
		HTMLURL      string     `json:"html_url"`
		MergedAt     *time.Time `json:"merged_at"`
		ChangedFiles int        `json:"changed_files"`
		Additions    int        `json:"additions"`
		Deletions    int        `json:"deletions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Metadata{}, fmt.Errorf("decode GitHub pull requests: %w", err)
	}
	metadata := Metadata{Available: true}
	for _, item := range raw {
		if item.MergedAt == nil {
			continue
		}
		pr := PullRequest{
			Number:       item.Number,
			Title:        item.Title,
			URL:          item.HTMLURL,
			ChangedFiles: item.ChangedFiles,
			Additions:    item.Additions,
			Deletions:    item.Deletions,
		}
		metadata.PRs = append(metadata.PRs, pr)
		if len(metadata.PRs) >= maxPRs {
			break
		}
	}
	if len(metadata.PRs) == 0 {
		metadata.Reason = "GitHub returned no merged PRs in the requested window."
	}
	return metadata, nil
}
