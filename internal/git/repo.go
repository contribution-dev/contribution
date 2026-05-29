// Package git provides V1 repository inspection through the git executable.
package git

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/contribution-dev/contribution/internal/privacy"
	"github.com/contribution-dev/contribution/internal/signals"
)

// Repo describes a resolved repository location.
type Repo struct {
	Path          string
	RemoteURL     string
	DefaultBranch string
	HeadSHA       string
	ID            string
	Name          string
	IsRemoteClone bool
	GitHubOwner   string
	GitHubRepo    string
	cleanup       func() error
}

// Metadata converts a resolved repo to the report data model.
func (r Repo) Metadata(publicSafe bool) signals.RepoMetadata {
	name := r.Name
	root := r.Path
	remote := r.RemoteURL
	headSHA := r.HeadSHA
	owner := r.GitHubOwner
	repoName := r.GitHubRepo
	if publicSafe {
		root = ""
		headSHA = ""
		remote = ""
		owner = ""
		repoName = ""
		name = "private repository"
	}
	return signals.RepoMetadata{
		ID:            r.ID,
		Name:          name,
		Root:          root,
		RemoteURL:     remote,
		DefaultBranch: r.DefaultBranch,
		HeadSHA:       headSHA,
		IsRemoteClone: r.IsRemoteClone,
		GitHubOwner:   owner,
		GitHubRepo:    repoName,
	}
}

// Close removes temp clones.
func (r Repo) Close() error {
	if r.cleanup == nil {
		return nil
	}
	return r.cleanup()
}

// Resolve opens a local repo or clones a remote Git URL to a temp directory.
func Resolve(ctx context.Context, source string) (Repo, error) {
	if source == "" {
		source = "."
	}
	if isGitURL(source) {
		return clone(ctx, source)
	}
	abs, err := filepath.Abs(source)
	if err != nil {
		return Repo{}, fmt.Errorf("resolve repo path: %w", err)
	}
	root, err := gitOutput(ctx, abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return Repo{}, fmt.Errorf("%s is not a git repository: %w", abs, err)
	}
	repo := Repo{Path: strings.TrimSpace(root)}
	if err := hydrateMetadata(ctx, &repo); err != nil {
		return Repo{}, err
	}
	return repo, nil
}

func clone(ctx context.Context, url string) (Repo, error) {
	parent, err := os.MkdirTemp("", "contribution-repo-*")
	if err != nil {
		return Repo{}, fmt.Errorf("create temp clone dir: %w", err)
	}
	target := filepath.Join(parent, "repo")
	args := []string{"clone", "--quiet", "--depth=250", "--no-tags", url, target}
	if _, err := gitOutput(ctx, "", args...); err != nil {
		_ = os.RemoveAll(parent)
		return Repo{}, fmt.Errorf("clone %s: %w", privacy.RedactRemoteURL(url), err)
	}
	repo := Repo{
		Path:          target,
		RemoteURL:     privacy.RedactRemoteURL(url),
		IsRemoteClone: true,
		cleanup: func() error {
			return os.RemoveAll(parent)
		},
	}
	if err := hydrateMetadata(ctx, &repo); err != nil {
		_ = repo.Close()
		return Repo{}, err
	}
	return repo, nil
}

func hydrateMetadata(ctx context.Context, repo *Repo) error {
	head, err := gitOutput(ctx, repo.Path, "rev-parse", "HEAD")
	if err == nil {
		repo.HeadSHA = strings.TrimSpace(head)
	}
	if repo.RemoteURL == "" {
		remote, err := gitOutput(ctx, repo.Path, "remote", "get-url", "origin")
		if err == nil {
			repo.RemoteURL = privacy.RedactRemoteURL(remote)
		}
	}
	branch, err := DefaultBranch(ctx, repo.Path)
	if err == nil {
		repo.DefaultBranch = branch
	}
	owner, name := ParseGitHubRepo(repo.RemoteURL)
	repo.GitHubOwner = owner
	repo.GitHubRepo = name
	if owner != "" && name != "" {
		repo.ID = owner + "/" + name
		repo.Name = name
		return nil
	}
	base := filepath.Base(repo.Path)
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = "repository"
	}
	repo.Name = strings.TrimSuffix(base, ".git")
	repo.ID = "local:" + shortHash(repo.Path)
	return nil
}

// DefaultBranch returns the best-effort default branch for a repository.
func DefaultBranch(ctx context.Context, repoPath string) (string, error) {
	if out, err := gitOutput(ctx, repoPath, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		value := strings.TrimSpace(out)
		if strings.HasPrefix(value, "origin/") {
			return strings.TrimPrefix(value, "origin/"), nil
		}
		if value != "" {
			return value, nil
		}
	}
	if out, err := gitOutput(ctx, repoPath, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		value := strings.TrimSpace(out)
		if value != "" && value != "HEAD" {
			return value, nil
		}
	}
	for _, branch := range []string{"main", "master"} {
		if _, err := gitOutput(ctx, repoPath, "rev-parse", "--verify", branch); err == nil {
			return branch, nil
		}
	}
	return "main", nil
}

// ParseGitHubRepo extracts owner and repo from common GitHub remote URL forms.
func ParseGitHubRepo(remote string) (string, string) {
	remote = strings.TrimSpace(remote)
	if parsed, err := url.Parse(remote); err == nil && parsed.Hostname() == "github.com" {
		parts := strings.Split(strings.Trim(strings.TrimSuffix(parsed.Path, ".git"), "/"), "/")
		if len(parts) >= 2 {
			return parts[0], parts[1]
		}
	}
	remote = strings.TrimSuffix(remote, ".git")
	switch {
	case strings.HasPrefix(remote, "https://github.com/"):
		parts := strings.Split(strings.TrimPrefix(remote, "https://github.com/"), "/")
		if len(parts) >= 2 {
			return parts[0], parts[1]
		}
	case strings.HasPrefix(remote, "http://github.com/"):
		parts := strings.Split(strings.TrimPrefix(remote, "http://github.com/"), "/")
		if len(parts) >= 2 {
			return parts[0], parts[1]
		}
	case strings.HasPrefix(remote, "git@github.com:"):
		parts := strings.Split(strings.TrimPrefix(remote, "git@github.com:"), "/")
		if len(parts) >= 2 {
			return parts[0], parts[1]
		}
	case strings.HasPrefix(remote, "ssh://git@github.com/"):
		parts := strings.Split(strings.TrimPrefix(remote, "ssh://git@github.com/"), "/")
		if len(parts) >= 2 {
			return parts[0], parts[1]
		}
	}
	return "", ""
}

// Classification describes a V1 file path classification.
type Classification struct {
	Class             string
	Language          string
	IsTest            bool
	IsDocs            bool
	IsSource          bool
	IsDependency      bool
	IsConfig          bool
	IsGenerated       bool
	IsVendor          bool
	IsInfrastructure  bool
	IsBuildArtifact   bool
	IsMigration       bool
	IsSecurityRelated bool
}

// ClassifyPath classifies a repository-relative path.
func ClassifyPath(path string) Classification {
	p := filepath.ToSlash(strings.TrimPrefix(path, "./"))
	lower := strings.ToLower(p)
	base := strings.ToLower(filepath.Base(p))
	class := Classification{Class: "source", Language: languageForPath(lower), IsSource: true}

	switch {
	case isVendorPath(lower):
		class = Classification{Class: "vendor", Language: languageForPath(lower), IsVendor: true}
	case isGeneratedPath(lower):
		class = Classification{Class: "generated", Language: languageForPath(lower), IsGenerated: true}
	case isBuildArtifactPath(lower):
		class = Classification{Class: "build_artifact", Language: languageForPath(lower), IsBuildArtifact: true}
	case isTestPath(lower):
		class = Classification{Class: "test", Language: languageForPath(lower), IsTest: true}
	case isDocsPath(lower, base):
		class = Classification{Class: "docs", Language: "Markdown", IsDocs: true}
	case isDependencyFile(lower, base):
		class = Classification{Class: "dependency", Language: languageForPath(lower), IsDependency: true}
	case isConfigPath(lower, base):
		class = Classification{Class: "config", Language: languageForPath(lower), IsConfig: true}
	case isInfrastructurePath(lower, base):
		class = Classification{Class: "infrastructure", Language: languageForPath(lower), IsInfrastructure: true}
	case isMigrationPath(lower):
		class = Classification{Class: "migration", Language: languageForPath(lower), IsMigration: true, IsSource: true}
	case isExtensionlessScriptPath(lower, base):
		class = Classification{Class: "source", Language: "Shell", IsSource: true}
	case languageForPath(lower) == "Other":
		class = Classification{Class: "unknown", Language: "Other"}
	}

	class.IsSecurityRelated = isSecuritySensitivePath(lower)
	return class
}

// Inventory uses Git's visible file set and emits repo-level inventory signals.
func Inventory(ctx context.Context, repoPath, repoID string, createdAt time.Time) (signals.FileSummary, []signals.Signal, error) {
	summary := newFileSummary()
	paths, err := gitInventoryPaths(ctx, repoPath)
	if err != nil {
		return summary, nil, err
	}
	for _, rel := range paths {
		info, err := os.Stat(filepath.Join(repoPath, filepath.FromSlash(rel)))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return summary, nil, fmt.Errorf("stat inventory path %s: %w", rel, err)
		}
		if info.IsDir() {
			continue
		}
		addFileSummary(&summary, rel)
	}
	sigs := []signals.Signal{
		signals.New(repoID, "git", "repo_file_count", "repo", repoID, signals.SeverityInfo, signals.DirectionNeutral, signals.ConfidenceHigh, float64(summary.TotalFiles), "count", fmt.Sprintf("Repository inventory found %d files.", summary.TotalFiles), true, createdAt),
		signals.New(repoID, "git", "repo_source_file_count", "repo", repoID, signals.SeverityInfo, signals.DirectionNeutral, signals.ConfidenceHigh, float64(summary.SourceFiles), "count", fmt.Sprintf("Repository inventory found %d source files.", summary.SourceFiles), true, createdAt),
		signals.New(repoID, "git", "repo_test_file_count", "repo", repoID, signals.SeverityInfo, directionForPositiveCount(summary.TestFiles), signals.ConfidenceHigh, float64(summary.TestFiles), "count", fmt.Sprintf("Repository inventory found %d test files.", summary.TestFiles), true, createdAt),
		signals.New(repoID, "git", "repo_docs_file_count", "repo", repoID, signals.SeverityInfo, directionForPositiveCount(summary.DocsFiles), signals.ConfidenceHigh, float64(summary.DocsFiles), "count", fmt.Sprintf("Repository inventory found %d docs files.", summary.DocsFiles), true, createdAt),
	}
	if summary.RiskyFiles > 0 {
		sigs = append(sigs, signals.New(repoID, "git", "repo_security_sensitive_file_count", "repo", repoID, signals.SeverityInfo, signals.DirectionNeutral, signals.ConfidenceMedium, float64(summary.RiskyFiles), "count", fmt.Sprintf("Repository inventory found %d security-sensitive paths.", summary.RiskyFiles), false, createdAt))
	}
	return summary, sigs, nil
}

func gitInventoryPaths(ctx context.Context, repoPath string) ([]string, error) {
	out, err := gitOutput(ctx, repoPath, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("collect git inventory: %w", err)
	}
	raw := strings.Split(out, "\x00")
	paths := make([]string, 0, len(raw))
	for _, path := range raw {
		if path == "" {
			continue
		}
		path = filepath.ToSlash(filepath.Clean(path))
		if path == "." || strings.HasPrefix(path, "../") || filepath.IsAbs(path) {
			continue
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// ChangedFile is a changed path in commit history or a diff.
type ChangedFile struct {
	Path       string              `json:"path"`
	Additions  int                 `json:"additions,omitempty"`
	Deletions  int                 `json:"deletions,omitempty"`
	LineRanges []signals.LineRange `json:"line_ranges,omitempty"`
}

// Commit is a recent commit with classified file context.
type Commit struct {
	SHA               string
	Date              time.Time
	Subject           string
	Files             []ChangedFile
	TestsTouched      bool
	DocsTouched       bool
	SourceTouched     bool
	DependencyTouched bool
	GeneratedTouched  bool
	VendorTouched     bool
	RiskyTouched      bool
	IsRevert          bool
	IsFollowUpFix     bool
}

// History is a local git-history summary.
type History struct {
	Commits        []Commit
	FileTouchCount map[string]int
	HighChurnFiles []string
}

// CollectHistory collects recent commit and churn signals.
func CollectHistory(ctx context.Context, repoPath, repoID string, since time.Time, maxCommits int, createdAt time.Time) (History, []signals.Signal, []string, error) {
	return CollectHistoryWindow(ctx, repoPath, repoID, since, time.Time{}, maxCommits, createdAt)
}

// CollectHistoryWindow collects commit and churn signals within an explicit window.
func CollectHistoryWindow(ctx context.Context, repoPath, repoID string, since time.Time, until time.Time, maxCommits int, createdAt time.Time) (History, []signals.Signal, []string, error) {
	args := []string{
		"log",
		"--since=" + since.Format(time.RFC3339),
	}
	if !until.IsZero() {
		args = append(args, "--until="+until.Format(time.RFC3339))
	}
	args = append(args,
		"--date=iso-strict",
		"--pretty=format:@@@%H%x09%aI%x09%s",
		"--numstat",
	)
	out, err := gitOutput(ctx, repoPath, args...)
	if err != nil {
		return History{}, nil, nil, fmt.Errorf("collect git history: %w", err)
	}
	history := parseHistory(out, maxCommits)
	signalsOut := historySignals(repoID, history, createdAt)
	var limitations []string
	if len(history.Commits) == 0 {
		limitations = append(limitations, "No commits were found in the analysis window.")
	}
	return history, signalsOut, limitations, nil
}

// DiffSummary describes the current review diff.
type DiffSummary struct {
	Files       []ChangedFile
	FileSummary signals.FileSummary
}

// Diff computes changed files between base and head.
func Diff(ctx context.Context, repoPath, base, head string) (DiffSummary, error) {
	if base == "" {
		base = "main"
	}
	if head == "" {
		head = "HEAD"
	}
	resolvedBase, err := ResolveCommit(ctx, repoPath, base)
	if err != nil {
		return DiffSummary{}, err
	}
	resolvedHead, err := ResolveCommit(ctx, repoPath, head)
	if err != nil {
		return DiffSummary{}, err
	}
	spec := resolvedBase + "..." + resolvedHead
	out, err := gitOutput(ctx, repoPath, "diff", "--numstat", spec)
	if err != nil {
		spec = resolvedBase + ".." + resolvedHead
		out, err = gitOutput(ctx, repoPath, "diff", "--numstat", spec)
		if err != nil {
			return DiffSummary{}, fmt.Errorf("git diff %s: %w", spec, err)
		}
	}
	files := parseNumstat(out)
	patch, patchErr := gitOutput(ctx, repoPath, "diff", "--unified=0", "--no-color", spec)
	if patchErr == nil {
		ranges := parseUnifiedChangedLineRanges(patch)
		for i := range files {
			files[i].LineRanges = ranges[files[i].Path]
		}
	}
	summary := newFileSummary()
	for _, file := range files {
		addFileSummary(&summary, file.Path)
	}
	return DiffSummary{Files: files, FileSummary: summary}, nil
}

// DiffWorktree computes changed files between base and the current worktree.
// It includes tracked staged/unstaged changes plus untracked non-ignored files.
func DiffWorktree(ctx context.Context, repoPath, base string) (DiffSummary, error) {
	if base == "" {
		base = "main"
	}
	resolvedBase, err := ResolveCommit(ctx, repoPath, base)
	if err != nil {
		return DiffSummary{}, err
	}
	out, err := gitOutput(ctx, repoPath, "diff", "--numstat", resolvedBase, "--")
	if err != nil {
		return DiffSummary{}, fmt.Errorf("git diff %s: %w", resolvedBase, err)
	}
	files := parseNumstat(out)
	patch, patchErr := gitOutput(ctx, repoPath, "diff", "--unified=0", "--no-color", resolvedBase, "--")
	if patchErr == nil {
		ranges := parseUnifiedChangedLineRanges(patch)
		for i := range files {
			files[i].LineRanges = ranges[files[i].Path]
		}
	}
	untracked, err := untrackedChangedFiles(ctx, repoPath)
	if err != nil {
		return DiffSummary{}, err
	}
	files = append(files, untracked...)
	summary := newFileSummary()
	for _, file := range files {
		addFileSummary(&summary, file.Path)
	}
	return DiffSummary{Files: files, FileSummary: summary}, nil
}

// ResolveCommit resolves a user-provided ref to a commit SHA for diff commands.
func ResolveCommit(ctx context.Context, repoPath string, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "-") || strings.ContainsRune(ref, '\x00') {
		return "", fmt.Errorf("invalid git ref %q", ref)
	}
	out, err := gitOutput(ctx, repoPath, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("invalid git ref %q: %w", ref, err)
	}
	return strings.TrimSpace(out), nil
}

func untrackedChangedFiles(ctx context.Context, repoPath string) ([]ChangedFile, error) {
	out, err := gitOutput(ctx, repoPath, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("collect untracked files: %w", err)
	}
	var files []ChangedFile
	for _, rel := range strings.Split(out, "\x00") {
		if rel == "" {
			continue
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		if rel == "." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
			continue
		}
		file, ok, err := untrackedChangedFile(repoPath, rel)
		if err != nil {
			return nil, err
		}
		if ok {
			files = append(files, file)
		}
	}
	return files, nil
}

func untrackedChangedFile(repoPath string, rel string) (ChangedFile, bool, error) {
	path := filepath.Join(repoPath, filepath.FromSlash(rel))
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ChangedFile{}, false, nil
		}
		return ChangedFile{}, false, fmt.Errorf("stat untracked path %s: %w", rel, err)
	}
	if info.IsDir() {
		return ChangedFile{}, false, nil
	}
	// #nosec G304 -- path comes from git ls-files output constrained to repo-relative paths.
	data, err := os.ReadFile(path)
	if err != nil {
		return ChangedFile{}, false, fmt.Errorf("read untracked path %s: %w", rel, err)
	}
	lines := countTextLines(data)
	file := ChangedFile{Path: rel, Additions: lines}
	if lines > 0 {
		file.LineRanges = []signals.LineRange{{Start: 1, End: lines}}
	}
	return file, true, nil
}

func countTextLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	lines := bytes.Count(data, []byte{'\n'})
	if data[len(data)-1] != '\n' {
		lines++
	}
	return lines
}

func parseHistory(out string, maxCommits int) History {
	var commits []Commit
	var current *Commit
	counts := map[string]int{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "@@@") {
			if current != nil {
				commits = append(commits, *current)
				if maxCommits > 0 && len(commits) >= maxCommits {
					current = nil
					break
				}
			}
			parts := strings.SplitN(strings.TrimPrefix(line, "@@@"), "\t", 3)
			current = &Commit{}
			if len(parts) > 0 {
				current.SHA = parts[0]
			}
			if len(parts) > 1 {
				if parsed, err := time.Parse(time.RFC3339, parts[1]); err == nil {
					current.Date = parsed
				}
			}
			if len(parts) > 2 {
				current.Subject = parts[2]
			}
			subject := strings.ToLower(current.Subject)
			current.IsRevert = strings.HasPrefix(subject, "revert") || strings.Contains(subject, " revert")
			current.IsFollowUpFix = containsAny(subject, []string{"fix", "bug", "regression", "hotfix", "rollback", "revert", "broken", "issue", "patch"})
			continue
		}
		if current == nil {
			continue
		}
		file, ok := parseNumstatLine(line)
		if !ok {
			continue
		}
		path := file.Path
		current.Files = append(current.Files, file)
		class := ClassifyPath(path)
		current.TestsTouched = current.TestsTouched || class.IsTest
		current.DocsTouched = current.DocsTouched || class.IsDocs
		current.SourceTouched = current.SourceTouched || class.IsSource
		current.DependencyTouched = current.DependencyTouched || class.IsDependency
		current.GeneratedTouched = current.GeneratedTouched || class.IsGenerated
		current.VendorTouched = current.VendorTouched || class.IsVendor
		current.RiskyTouched = current.RiskyTouched || class.IsSecurityRelated
		counts[path]++
	}
	if current != nil && (maxCommits <= 0 || len(commits) < maxCommits) {
		commits = append(commits, *current)
	}
	return History{Commits: commits, FileTouchCount: counts, HighChurnFiles: highChurnFiles(counts)}
}

func historySignals(repoID string, history History, createdAt time.Time) []signals.Signal {
	out := []signals.Signal{
		signals.New(repoID, "git", "commit_count", "repo", repoID, signals.SeverityInfo, signals.DirectionNeutral, signals.ConfidenceHigh, float64(len(history.Commits)), "count", fmt.Sprintf("%d commits were found in the analysis window.", len(history.Commits)), true, createdAt),
		signals.New(repoID, "git", "files_changed_count", "repo", repoID, signals.SeverityInfo, signals.DirectionNeutral, signals.ConfidenceHigh, float64(len(history.FileTouchCount)), "count", fmt.Sprintf("%d unique files changed in the analysis window.", len(history.FileTouchCount)), true, createdAt),
	}
	for _, commit := range history.Commits {
		short := ShortSHA(commit.SHA)
		out = append(out, signals.New(repoID, "git", "commit_file_count", "commit", commit.SHA, signals.SeverityInfo, signals.DirectionNeutral, signals.ConfidenceHigh, float64(len(commit.Files)), "count", fmt.Sprintf("Commit %s changed %d files.", short, len(commit.Files)), true, createdAt))
		lineCount := TotalChangedLines(commit.Files)
		if lineCount > 0 {
			out = append(out, signals.New(repoID, "git", "commit_line_count", "commit", commit.SHA, signals.SeverityInfo, signals.DirectionNeutral, signals.ConfidenceHigh, float64(lineCount), "lines", fmt.Sprintf("Commit %s changed %d lines.", short, lineCount), true, createdAt))
		}
		if commit.TestsTouched {
			out = append(out, signals.New(repoID, "git", "commit_tests_touched", "commit", commit.SHA, signals.SeverityInfo, signals.DirectionPositive, signals.ConfidenceHigh, 1, "boolean", fmt.Sprintf("Commit %s touched test files.", short), true, createdAt))
		} else if commit.SourceTouched {
			out = append(out, signals.New(repoID, "git", "commit_no_tests_touched", "commit", commit.SHA, signals.SeverityLow, signals.DirectionNegative, signals.ConfidenceMedium, 1, "boolean", fmt.Sprintf("Commit %s changed source files without touching tests.", short), true, createdAt))
		}
		if commit.RiskyTouched {
			out = append(out, signals.New(repoID, "git", "commit_risky_paths_touched", "commit", commit.SHA, signals.SeverityMedium, signals.DirectionNegative, signals.ConfidenceMedium, 1, "boolean", fmt.Sprintf("Commit %s touched security-sensitive paths.", short), false, createdAt))
		}
		if commit.IsRevert {
			out = append(out, signals.New(repoID, "git", "revert_commit", "commit", commit.SHA, signals.SeverityMedium, signals.DirectionNegative, signals.ConfidenceHigh, 1, "boolean", fmt.Sprintf("Commit %s appears to be a revert.", short), true, createdAt))
		} else if commit.IsFollowUpFix {
			out = append(out, signals.New(repoID, "git", "follow_up_fix_commit", "commit", commit.SHA, signals.SeverityLow, signals.DirectionNeutral, signals.ConfidenceLow, 1, "boolean", fmt.Sprintf("Commit %s looks like a follow-up fix by message heuristic.", short), true, createdAt))
		}
	}
	for _, file := range history.HighChurnFiles {
		sig := signals.New(repoID, "git", "high_churn_file", "file", file, signals.SeverityMedium, signals.DirectionNegative, signals.ConfidenceMedium, float64(history.FileTouchCount[file]), "count", fmt.Sprintf("%s changed %d times in the analysis window.", file, history.FileTouchCount[file]), false, createdAt)
		sig.FilePath = file
		out = append(out, sig)
	}
	return out
}

func parseNumstat(out string) []ChangedFile {
	var files []ChangedFile
	for _, line := range strings.Split(out, "\n") {
		file, ok := parseNumstatLine(line)
		if ok {
			files = append(files, file)
		}
	}
	return files
}

func parseNumstatLine(line string) (ChangedFile, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return ChangedFile{}, false
	}
	parts := strings.Split(line, "\t")
	if len(parts) < 3 {
		return ChangedFile{}, false
	}
	additions := parseNumstatCount(parts[0])
	deletions := parseNumstatCount(parts[1])
	path := strings.Join(parts[2:], "\t")
	return ChangedFile{Path: filepath.ToSlash(path), Additions: additions, Deletions: deletions}, true
}

func parseUnifiedChangedLineRanges(out string) map[string][]signals.LineRange {
	ranges := map[string][]signals.LineRange{}
	currentPath := ""
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "+++ ") {
			currentPath = parseUnifiedPath(strings.TrimSpace(strings.TrimPrefix(line, "+++ ")))
			continue
		}
		if currentPath == "" || !strings.HasPrefix(line, "@@ ") {
			continue
		}
		rng, ok := parseUnifiedNewRange(line)
		if ok {
			ranges[currentPath] = append(ranges[currentPath], rng)
		}
	}
	return ranges
}

func parseUnifiedPath(path string) string {
	if tab := strings.Index(path, "\t"); tab >= 0 {
		path = path[:tab]
	}
	path = strings.TrimSpace(path)
	if path == "/dev/null" {
		return ""
	}
	path = strings.TrimPrefix(path, "b/")
	path = strings.Trim(path, `"`)
	return filepath.ToSlash(path)
}

func parseUnifiedNewRange(line string) (signals.LineRange, bool) {
	plus := strings.Index(line, "+")
	if plus < 0 {
		return signals.LineRange{}, false
	}
	rest := line[plus+1:]
	end := strings.Index(rest, " ")
	if end < 0 {
		return signals.LineRange{}, false
	}
	token := rest[:end]
	parts := strings.SplitN(token, ",", 2)
	start, err := strconv.Atoi(parts[0])
	if err != nil || start <= 0 {
		return signals.LineRange{}, false
	}
	count := 1
	if len(parts) == 2 {
		parsed, err := strconv.Atoi(parts[1])
		if err != nil {
			return signals.LineRange{}, false
		}
		count = parsed
	}
	if count <= 0 {
		return signals.LineRange{}, false
	}
	return signals.LineRange{Start: start, End: start + count - 1}, true
}

func parseNumstatCount(value string) int {
	if value == "-" {
		return 0
	}
	count, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return count
}

// TotalChangedLines returns additions plus deletions for a changed-file list.
func TotalChangedLines(files []ChangedFile) int {
	var total int
	for _, file := range files {
		total += file.Additions + file.Deletions
	}
	return total
}

func newFileSummary() signals.FileSummary {
	return signals.FileSummary{
		ByClass:    map[string]int{},
		ByLanguage: map[string]int{},
	}
}

func addFileSummary(summary *signals.FileSummary, path string) {
	class := ClassifyPath(path)
	summary.TotalFiles++
	summary.ByClass[class.Class]++
	summary.ByLanguage[class.Language]++
	switch {
	case class.IsTest:
		summary.TestFiles++
	case class.IsDocs:
		summary.DocsFiles++
	case class.IsDependency:
		summary.DependencyFiles++
	case class.IsConfig:
		summary.ConfigFiles++
	case class.IsGenerated:
		summary.GeneratedFiles++
	case class.IsVendor:
		summary.VendorFiles++
	case class.IsSource:
		summary.SourceFiles++
	}
	if class.IsSecurityRelated {
		summary.RiskyFiles++
	}
}

func highChurnFiles(counts map[string]int) []string {
	type pair struct {
		path  string
		count int
	}
	var pairs []pair
	for path, count := range counts {
		if count >= 3 {
			pairs = append(pairs, pair{path: path, count: count})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count == pairs[j].count {
			return pairs[i].path < pairs[j].path
		}
		return pairs[i].count > pairs[j].count
	})
	limit := min(len(pairs), 5)
	out := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, pairs[i].path)
	}
	return out
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	// #nosec G204 -- the executable is fixed to git; args are internal command arguments.
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("git timed out")
	}
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = err.Error()
		}
		text = privacy.RedactSecretLikeText(text)
		return "", fmt.Errorf("%s", text)
	}
	return string(out), nil
}

func isGitURL(source string) bool {
	return strings.HasPrefix(source, "https://") ||
		strings.HasPrefix(source, "http://") ||
		strings.HasPrefix(source, "git@") ||
		strings.HasPrefix(source, "ssh://") ||
		strings.HasSuffix(source, ".git")
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:12]
}

// ShortSHA returns the conventional short display prefix for a commit SHA.
func ShortSHA(value string) string {
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}

func directionForPositiveCount(count int) signals.Direction {
	if count > 0 {
		return signals.DirectionPositive
	}
	return signals.DirectionNeutral
}

func languageForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "Go"
	case ".js", ".mjs", ".cjs":
		return "JavaScript"
	case ".ts", ".tsx":
		return "TypeScript"
	case ".jsx":
		return "JavaScript"
	case ".py":
		return "Python"
	case ".rb":
		return "Ruby"
	case ".rs":
		return "Rust"
	case ".java":
		return "Java"
	case ".kt", ".kts":
		return "Kotlin"
	case ".swift":
		return "Swift"
	case ".c", ".h":
		return "C"
	case ".cc", ".cpp", ".cxx", ".hpp":
		return "C++"
	case ".cs":
		return "C#"
	case ".php":
		return "PHP"
	case ".md", ".mdx":
		return "Markdown"
	case ".yaml", ".yml":
		return "YAML"
	case ".json":
		return "JSON"
	case ".toml":
		return "TOML"
	case ".tf":
		return "Terraform"
	case ".sh", ".bash", ".zsh":
		return "Shell"
	case ".sql":
		return "SQL"
	default:
		return "Other"
	}
}

func isTestPath(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, "_test.go") ||
		strings.HasSuffix(base, ".test.ts") ||
		strings.HasSuffix(base, ".test.tsx") ||
		strings.HasSuffix(base, ".spec.ts") ||
		strings.HasSuffix(base, ".spec.tsx") ||
		strings.HasSuffix(base, ".test.js") ||
		strings.HasSuffix(base, ".spec.js") ||
		strings.HasSuffix(base, ".test.mjs") ||
		strings.HasSuffix(base, ".spec.mjs") ||
		strings.HasSuffix(base, ".test.cjs") ||
		strings.HasSuffix(base, ".spec.cjs") ||
		strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py") ||
		strings.HasSuffix(base, "_test.py") ||
		strings.HasPrefix(path, "tests/") ||
		strings.HasPrefix(path, "spec/") ||
		strings.HasPrefix(path, "__tests__/")
}

func isDocsPath(path, base string) bool {
	return strings.HasPrefix(base, "readme") ||
		strings.HasPrefix(path, "docs/") ||
		strings.HasSuffix(path, ".md") ||
		strings.HasSuffix(path, ".mdx")
}

func isDependencyFile(path, base string) bool {
	switch base {
	case "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "go.mod", "go.sum",
		"requirements.txt", "poetry.lock", "pipfile", "pipfile.lock", "cargo.toml", "cargo.lock",
		"gemfile", "gemfile.lock", "pom.xml", "build.gradle":
		return true
	}
	return path == "pipfile" || path == "pipfile.lock"
}

func isConfigPath(path, base string) bool {
	switch base {
	case ".editorconfig", ".gitignore", ".gitattributes", ".npmrc", ".nvmrc",
		".prettierrc", ".prettierignore", ".eslintrc", ".golangci.yml", ".golangci.yaml",
		".goreleaser.yml", ".goreleaser.yaml", ".dockerignore", "makefile", "justfile",
		".contribution.yml", ".contribution.yaml", "lint-staged.config.js", "pnpm-workspace.yaml", "tsconfig.json", "jsconfig.json",
		"license", "licence", "copying", "notice":
		return true
	}
	return strings.HasPrefix(base, ".prettierrc.") ||
		strings.HasPrefix(base, ".eslintrc.") ||
		strings.HasPrefix(path, ".codex/")
}

func isExtensionlessScriptPath(path, base string) bool {
	return strings.HasPrefix(path, "scripts/") && filepath.Ext(base) == ""
}

func isVendorPath(path string) bool {
	return strings.HasPrefix(path, "vendor/") || strings.HasPrefix(path, "node_modules/")
}

func isGeneratedPath(path string) bool {
	base := filepath.Base(path)
	return strings.HasPrefix(path, "generated/") ||
		strings.Contains(base, ".generated.") ||
		strings.HasSuffix(base, ".pb.go") ||
		strings.Contains(base, ".gen.")
}

func isBuildArtifactPath(path string) bool {
	return strings.HasPrefix(path, "dist/") ||
		strings.HasPrefix(path, "build/") ||
		strings.HasPrefix(path, "coverage/")
}

func isInfrastructurePath(path, base string) bool {
	return base == "dockerfile" ||
		strings.HasPrefix(base, "docker-compose") ||
		strings.HasPrefix(path, ".github/") ||
		strings.HasPrefix(path, "terraform/") ||
		strings.HasSuffix(path, ".tf") ||
		strings.HasPrefix(path, "k8s/") ||
		strings.HasPrefix(path, "helm/") ||
		strings.HasPrefix(path, "charts/")
}

func isMigrationPath(path string) bool {
	return strings.Contains(path, "migration") || strings.Contains(path, "migrations/")
}

func isSecuritySensitivePath(path string) bool {
	keywords := []string{"auth", "oauth", "permission", "permissions", "rbac", "security", "secret", "secrets", "token", "tokens", "billing", "payment", "payments", "checkout", "session", "sessions", "crypto", "password", "passwords", "admin"}
	return containsAny(path, keywords)
}

func containsAny(value string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
