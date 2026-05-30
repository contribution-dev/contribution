// Package github provides optional V1 GitHub metadata enrichment.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Metadata contains optional GitHub PR enrichment.
type Metadata struct {
	Available   bool
	Reason      string
	Limitations []string
	PRs         []PullRequest
}

// PullRequest is the subset of GitHub PR metadata V1 uses.
type PullRequest struct {
	Number           int
	Title            string
	URL              string
	MergedAt         time.Time
	ChangedFiles     int
	Additions        int
	Deletions        int
	Commits          int
	ReviewComments   int
	IssueComments    int
	Files            []string
	ReviewCount      int
	RequestedChanges int
	Approvals        int
	CheckRuns        int
	FailedChecks     int
	SuccessfulChecks int
	MergeCommitSHA   string
}

var (
	httpClient       = http.DefaultClient
	githubAPIBaseURL = "https://api.github.com"
)

// ResolveToken treats the flag value as either a literal token or env var name.
func ResolveToken(flagValue string) (string, bool) {
	flagValue = strings.TrimSpace(flagValue)
	if flagValue != "" {
		if flagValue == "gh" || flagValue == "gh:token" {
			return tokenFromGH()
		}
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

// GHTokenAvailable reports whether the GitHub CLI can provide a token.
func GHTokenAvailable() bool {
	token, ok := tokenFromGH()
	return ok && token != ""
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
	listLimit := maxPRs * 2
	if listLimit > 100 {
		listLimit = 100
	}
	url := fmt.Sprintf("%s/repos/%s/%s/pulls?state=closed&sort=updated&direction=desc&per_page=%d", githubAPIBaseURL, owner, repo, listLimit)
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
		Number   int        `json:"number"`
		Title    string     `json:"title"`
		HTMLURL  string     `json:"html_url"`
		MergedAt *time.Time `json:"merged_at"`
		MergeSHA string     `json:"merge_commit_sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Metadata{}, fmt.Errorf("decode GitHub pull requests: %w", err)
	}
	metadata := Metadata{Available: true}
	for _, item := range raw {
		if item.MergedAt == nil {
			continue
		}
		detail, err := fetchPRDetails(ctx, owner, repo, token, item.Number)
		pr := detail.pr
		if err != nil {
			metadata.Limitations = append(metadata.Limitations, err.Error())
			pr.Number = item.Number
			pr.Title = item.Title
			pr.URL = item.HTMLURL
			pr.MergedAt = *item.MergedAt
			pr.MergeCommitSHA = item.MergeSHA
		}
		if pr.MergedAt.IsZero() {
			pr.MergedAt = *item.MergedAt
		}
		if pr.MergeCommitSHA == "" {
			pr.MergeCommitSHA = item.MergeSHA
		}
		files, err := fetchPRFiles(ctx, owner, repo, token, item.Number)
		if err != nil {
			metadata.Limitations = append(metadata.Limitations, err.Error())
		} else {
			pr.Files = files
			if pr.ChangedFiles == 0 {
				pr.ChangedFiles = len(files)
			}
		}
		reviews, err := fetchPRReviews(ctx, owner, repo, token, item.Number)
		if err != nil {
			metadata.Limitations = append(metadata.Limitations, err.Error())
		} else {
			pr.ReviewCount = reviews.total
			pr.RequestedChanges = reviews.requestedChanges
			pr.Approvals = reviews.approvals
		}
		if detail.headSHA != "" {
			checks, err := fetchCheckRuns(ctx, owner, repo, token, detail.headSHA)
			if err != nil {
				metadata.Limitations = append(metadata.Limitations, err.Error())
			} else {
				pr.CheckRuns = checks.total
				pr.FailedChecks = checks.failed
				pr.SuccessfulChecks = checks.successful
			}
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

type reviewSummary struct {
	total            int
	requestedChanges int
	approvals        int
}

type pullRequestDetails struct {
	pr      PullRequest
	headSHA string
}

type checkSummary struct {
	total      int
	successful int
	failed     int
}

func tokenFromGH() (string, bool) {
	path, err := exec.LookPath("gh")
	if err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// #nosec G204 -- path comes from exec.LookPath for the fixed GitHub CLI binary.
	cmd := exec.CommandContext(ctx, path, "auth", "token")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	token := strings.TrimSpace(string(out))
	return token, token != ""
}

func fetchPRDetails(ctx context.Context, owner, repo, token string, number int) (pullRequestDetails, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", githubAPIBaseURL, owner, repo, number)
	var raw struct {
		Number         int        `json:"number"`
		Title          string     `json:"title"`
		HTMLURL        string     `json:"html_url"`
		ChangedFiles   int        `json:"changed_files"`
		Additions      int        `json:"additions"`
		Deletions      int        `json:"deletions"`
		Commits        int        `json:"commits"`
		Comments       int        `json:"comments"`
		ReviewComments int        `json:"review_comments"`
		MergedAt       *time.Time `json:"merged_at"`
		MergeCommitSHA string     `json:"merge_commit_sha"`
		Head           struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := githubGetJSON(ctx, token, url, &raw); err != nil {
		return pullRequestDetails{}, fmt.Errorf("GitHub PR #%d detail metadata unavailable: %w", number, err)
	}
	return pullRequestDetails{
		pr: PullRequest{
			Number:         raw.Number,
			Title:          raw.Title,
			URL:            raw.HTMLURL,
			MergedAt:       derefTime(raw.MergedAt),
			ChangedFiles:   raw.ChangedFiles,
			Additions:      raw.Additions,
			Deletions:      raw.Deletions,
			Commits:        raw.Commits,
			IssueComments:  raw.Comments,
			ReviewComments: raw.ReviewComments,
			MergeCommitSHA: raw.MergeCommitSHA,
		},
		headSHA: raw.Head.SHA,
	}, nil
}

func derefTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func fetchPRFiles(ctx context.Context, owner, repo, token string, number int) ([]string, error) {
	var files []string
	for page := 1; ; page++ {
		url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/files?per_page=100&page=%d", githubAPIBaseURL, owner, repo, number, page)
		var raw []struct {
			Filename string `json:"filename"`
		}
		if err := githubGetJSON(ctx, token, url, &raw); err != nil {
			return nil, fmt.Errorf("GitHub PR #%d changed-file metadata unavailable: %w", number, err)
		}
		for _, item := range raw {
			if strings.TrimSpace(item.Filename) != "" {
				files = append(files, item.Filename)
			}
		}
		if len(raw) < 100 {
			break
		}
	}
	return files, nil
}

func fetchPRReviews(ctx context.Context, owner, repo, token string, number int) (reviewSummary, error) {
	var summary reviewSummary
	for page := 1; ; page++ {
		url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/reviews?per_page=100&page=%d", githubAPIBaseURL, owner, repo, number, page)
		var raw []struct {
			State string `json:"state"`
		}
		if err := githubGetJSON(ctx, token, url, &raw); err != nil {
			return reviewSummary{}, fmt.Errorf("GitHub PR #%d review metadata unavailable: %w", number, err)
		}
		for _, item := range raw {
			state := strings.ToUpper(strings.TrimSpace(item.State))
			if state == "" {
				continue
			}
			summary.total++
			switch state {
			case "CHANGES_REQUESTED":
				summary.requestedChanges++
			case "APPROVED":
				summary.approvals++
			}
		}
		if len(raw) < 100 {
			break
		}
	}
	return summary, nil
}

func fetchCheckRuns(ctx context.Context, owner, repo, token, sha string) (checkSummary, error) {
	var summary checkSummary
	for page := 1; ; page++ {
		url := fmt.Sprintf("%s/repos/%s/%s/commits/%s/check-runs?per_page=100&page=%d", githubAPIBaseURL, owner, repo, sha, page)
		var raw struct {
			CheckRuns []struct {
				Conclusion string `json:"conclusion"`
			} `json:"check_runs"`
		}
		if err := githubGetJSON(ctx, token, url, &raw); err != nil {
			return checkSummary{}, fmt.Errorf("GitHub checks for %s unavailable: %w", shortSHA(sha), err)
		}
		for _, item := range raw.CheckRuns {
			conclusion := strings.ToLower(strings.TrimSpace(item.Conclusion))
			if conclusion == "" {
				continue
			}
			summary.total++
			switch conclusion {
			case "success", "neutral", "skipped":
				summary.successful++
			default:
				summary.failed++
			}
		}
		if len(raw.CheckRuns) < 100 {
			break
		}
	}
	return summary, nil
}

func githubGetJSON(ctx context.Context, token, url string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("GitHub API returned %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func shortSHA(value string) string {
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}
