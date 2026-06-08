package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/contribution-dev/contribution/internal/config"
	gitrepo "github.com/contribution-dev/contribution/internal/git"
	"github.com/contribution-dev/contribution/internal/privacy"
	"github.com/contribution-dev/contribution/internal/signals"
)

const (
	sourceClaude = "claude_code"
	sourceCodex  = "codex_cli"
	sourceKind   = "local_agent_session"

	maxSessionArtifactBytes = 1024 * 1024
	maxSessionArtifacts     = 500
)

var (
	evidenceNowUTC       = func() time.Time { return time.Now().UTC() }
	linearIssueIDPattern = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-\d+\b`)
	githubIssuePattern   = regexp.MustCompile(`(?:^|[\s(])#(\d+)\b`)
	prNumberPattern      = regexp.MustCompile(`(?i)\b(?:pr|pull request)[\s#:-]*(\d+)\b|refs/pull/(\d+)`)
	hexSHASevenPattern   = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
	pathLikePattern      = regexp.MustCompile(`^[A-Za-z0-9._/@:+\-\s]+\/[A-Za-z0-9._/@:+\-\s]+\.[A-Za-z0-9]{1,12}$`)
	privateKeyPattern    = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
	envAssignmentPattern = regexp.MustCompile(`\b[A-Z][A-Z0-9_]{2,}\s*=\s*[^\s'"]+`)
)

// Preview scans opt-in local AI session sources and returns a derived bundle without writing it.
func Preview(ctx context.Context, opts Options) (Result, error) {
	return build(ctx, opts, false)
}

// Export scans opt-in local AI session sources and writes an offline JSON bundle.
func Export(ctx context.Context, opts Options) (Result, error) {
	return build(ctx, opts, true)
}

// Doctor checks whether local evidence source roots are available. It does not read session files.
func Doctor(_ context.Context, opts Options) (DoctorResult, error) {
	opts = withDefaultSourceDirs(opts)
	claudeInfo, claudeErr := os.Stat(opts.ClaudeDir)
	codexInfo, codexErr := os.Stat(opts.CodexDir)
	return DoctorResult{
		ClaudeAvailable: claudeErr == nil && claudeInfo.IsDir(),
		ClaudePath:      opts.ClaudeDir,
		CodexAvailable:  codexErr == nil && codexInfo.IsDir(),
		CodexPath:       opts.CodexDir,
		NetworkUsed:     false,
		UploadMode:      UploadModeDisabled,
	}, nil
}

// Upload is intentionally disabled until the CLI consumes a finalized website contract.
func Upload(_ context.Context, _ Options) error {
	return errors.New("evidence upload is disabled until the CLI consumes a finalized website receiving contract")
}

func build(ctx context.Context, opts Options, write bool) (Result, error) {
	opts = withDefaultSourceDirs(opts)
	if looksLikeGitURL(opts.Repo) {
		return Result{}, fmt.Errorf("evidence collection requires a local git repository path; remote Git URLs would break offline mode")
	}
	now := evidenceNowUTC()
	repo, err := gitrepo.Resolve(ctx, opts.Repo)
	if err != nil {
		return Result{}, err
	}
	defer func() {
		_ = repo.Close()
	}()
	anchor := collectRepoAnchor(ctx, repo, opts)
	bundleID := "aiweb_" + shortHash(anchor.RepoID+"|"+anchor.RepoRemoteHash+"|"+now.Format(time.RFC3339Nano))
	receipt := newRedactionReceipt(now, bundleID, opts)
	sessions, summaries, lineage, notes := discoverSessions(ctx, repo, anchor, opts, &receipt)
	receipt.ScannedSources = scannedSources(summaries)
	receipt.BlockedFieldClasses = sortedCountKeys(receipt.BlockedContent)
	sort.Strings(receipt.ExtractedFieldNames)
	receipt.FieldsExtracted = len(receipt.ExtractedFieldNames)
	privacyFlags := privacyFlags(opts)
	receipt.PrivacyFlags = privacyFlags
	receipt.RedactionGuaranteed = receipt.FailureReason == ""
	receipt.RawContentIncluded = false
	for i := range sessions {
		sessions[i].RedactionReceiptID = receipt.ID
		sessions[i].RepoRemoteHash = anchor.RepoRemoteHash
		sessions[i].RepoID = anchor.RepoID
		if opts.IncludeRepoName {
			sessions[i].RepoName = anchor.RepoName
		}
	}
	confidence := bundleConfidence(sessions)
	bundle := AIWorkEvidenceBundle{
		Schema:      BundleSchema,
		Version:     BundleVersion,
		GeneratedAt: now,
		BundleID:    bundleID,
		Repo:        anchor,
		Export: ModeSummary{
			Mode:        ExportModeOffline,
			Destination: "local_file",
			Enabled:     true,
		},
		Upload: ModeSummary{
			Mode:    UploadModeDisabled,
			Enabled: false,
		},
		EvidenceUpload: UploadRecord{
			ID:        "evupl_" + shortHash(bundleID+"|upload"),
			BundleID:  bundleID,
			Mode:      UploadModeDisabled,
			Status:    "not_uploaded",
			CreatedAt: now,
		},
		Privacy:          privacyFlags,
		SourceLineage:    lineage,
		WorkSessions:     sessions,
		RedactionReceipt: receipt,
		Confidence:       confidence,
		LinkageNotes:     notes,
		Limitations:      limitations(summaries, sessions),
	}
	result := Result{
		Bundle:          bundle,
		SourceSummaries: summaries,
		SourcesScanned:  len(summaries),
		SessionsFound:   sumSessionsFound(summaries),
		SessionsLinked:  len(sessions),
		SessionsSkipped: sumSessionsFound(summaries) - len(sessions),
		FieldsExtracted: receipt.FieldsExtracted,
		FieldsRedacted:  sumCounts(receipt.RedactedContent),
		FieldsBlocked:   sumCounts(receipt.BlockedContent),
	}
	if write {
		if !receipt.RedactionGuaranteed {
			return Result{}, fmt.Errorf("redaction cannot be guaranteed: %s", receipt.FailureReason)
		}
		outputDir, err := evidenceOutputDir(repo.Path, opts.Output, now)
		if err != nil {
			return Result{}, err
		}
		if err := os.MkdirAll(outputDir, 0o750); err != nil {
			return Result{}, fmt.Errorf("create evidence output directory: %w", err)
		}
		result.BundlePath = filepath.Join(outputDir, "ai-work-evidence.bundle.json")
		result.RedactionReceiptPath = filepath.Join(outputDir, "redaction-receipt.json")
		if err := writeJSON(result.BundlePath, result.Bundle); err != nil {
			return Result{}, err
		}
		if err := writeJSON(result.RedactionReceiptPath, result.Bundle.RedactionReceipt); err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

func collectRepoAnchor(ctx context.Context, repo gitrepo.Repo, opts Options) RepoAnchor {
	branch, _ := gitrepo.CurrentBranch(ctx, repo.Path)
	if branch == "" {
		branch = repo.DefaultBranch
	}
	commits := recentCommitSHAs(ctx, repo.Path, 50)
	if len(commits) == 0 && repo.HeadSHA != "" {
		commits = []string{repo.HeadSHA}
	}
	repoID := "repo_hash:" + shortHash(repo.ID+"|"+repo.RemoteURL)
	if opts.IncludeRepoName {
		repoID = repo.ID
	}
	anchor := RepoAnchor{
		RepoID:                repoID,
		RepoRemoteHash:        hashString(repo.RemoteURL),
		Branch:                branch,
		CurrentCommitSHAHash:  hashString(repo.HeadSHA),
		CommitSHAHashes:       hashStrings(commits),
		CurrentDiffFilesCount: currentDiffFileCount(ctx, repo.Path),
		rawCurrentCommitSHA:   repo.HeadSHA,
		rawCommitSHAs:         commits,
	}
	if opts.IncludeRepoName {
		anchor.RepoName = repo.Name
	}
	return anchor
}

func discoverSessions(ctx context.Context, repo gitrepo.Repo, anchor RepoAnchor, opts Options, receipt *RedactionReceipt) ([]WorkSession, []SourceScanSummary, []SourceLineage, []ConfidenceNote) {
	selected := selectedSources(opts.Sources)
	var sessions []WorkSession
	var summaries []SourceScanSummary
	var lineage []SourceLineage
	var notes []ConfidenceNote
	for _, source := range selected {
		sourcePath := opts.ClaudeDir
		sourceTool := sourceClaude
		if source == sourceCodex {
			sourcePath = opts.CodexDir
			sourceTool = sourceCodex
		}
		found, linked, unreadable, artifacts, sourceSessions, sourceNotes := scanSource(ctx, sourceTool, sourcePath, repo, anchor, opts, receipt)
		summary := SourceScanSummary{
			SourceTool:       sourceTool,
			SourceKind:       sourceKind,
			Path:             sourcePath,
			Status:           sourceStatus(sourcePath),
			ArtifactsScanned: artifacts,
			SessionsFound:    found,
			SessionsLinked:   linked,
			UnreadableCount:  unreadable,
		}
		summaries = append(summaries, summary)
		confidence := signals.ConfidenceLow
		if linked > 0 {
			confidence = signals.ConfidenceMedium
		}
		lineage = append(lineage, SourceLineage{
			ID:               "src_" + shortHash(sourceTool+"|"+sourcePath),
			SourceTool:       sourceTool,
			SourceKind:       sourceKind,
			SourcePathHash:   hashString(sourcePath),
			ArtifactsScanned: artifacts,
			SessionsFound:    found,
			SessionsLinked:   linked,
			UnreadableCount:  unreadable,
			ParserVersion:    1,
			Confidence:       confidence,
		})
		sessions = append(sessions, sourceSessions...)
		notes = append(notes, sourceNotes...)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt.Before(sessions[j].StartedAt)
	})
	return sessions, summaries, lineage, notes
}

func scanSource(ctx context.Context, sourceTool string, root string, repo gitrepo.Repo, anchor RepoAnchor, opts Options, receipt *RedactionReceipt) (int, int, int, int, []WorkSession, []ConfidenceNote) {
	if ctx.Err() != nil {
		return 0, 0, 0, 0, nil, nil
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return 0, 0, 0, 0, nil, nil
	}
	var found int
	var linked int
	var unreadable int
	var artifacts int
	var sessions []WorkSession
	var notes []ConfidenceNote
	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			unreadable++
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".jsonl" && ext != ".json" {
			return nil
		}
		if artifacts >= maxSessionArtifacts {
			return nil
		}
		artifacts++
		info, err := entry.Info()
		if err != nil || info.Size() > maxSessionArtifactBytes {
			unreadable++
			return nil
		}
		// #nosec G304,G122 -- evidence commands intentionally read opt-in local session artifacts discovered under the selected source root.
		data, err := os.ReadFile(path)
		if err != nil {
			unreadable++
			return nil
		}
		draft := parseSessionArtifact(sourceTool, path, data, receipt)
		if draft.eventCount == 0 {
			return nil
		}
		found++
		linkedSession, linkedNotes := finalizeSession(draft, repo, anchor, opts)
		if linkedSession == nil {
			return nil
		}
		linked++
		sessions = append(sessions, *linkedSession)
		notes = append(notes, linkedNotes...)
		return nil
	})
	if walkErr != nil {
		unreadable++
	}
	return found, linked, unreadable, artifacts, sessions, notes
}

type sessionDraft struct {
	sourceTool            string
	sourceFile            string
	sessionID             string
	startedAt             time.Time
	endedAt               time.Time
	branches              map[string]struct{}
	commits               map[string]struct{}
	prNumbers             map[int]struct{}
	issueKeys             map[string]struct{}
	filePaths             map[string]struct{}
	repoPaths             map[string]struct{}
	repoRemoteHashes      map[string]struct{}
	repoNames             map[string]struct{}
	intentSummary         string
	planSummary           string
	implementationSummary string
	humanSteeringCount    int
	correctionCount       int
	testDebugCount        int
	agentActionCount      int
	eventCount            int
	extractedFields       map[string]struct{}
}

func newSessionDraft(sourceTool string, sourceFile string) sessionDraft {
	return sessionDraft{
		sourceTool:       sourceTool,
		sourceFile:       sourceFile,
		branches:         map[string]struct{}{},
		commits:          map[string]struct{}{},
		prNumbers:        map[int]struct{}{},
		issueKeys:        map[string]struct{}{},
		filePaths:        map[string]struct{}{},
		repoPaths:        map[string]struct{}{},
		repoRemoteHashes: map[string]struct{}{},
		repoNames:        map[string]struct{}{},
		extractedFields:  map[string]struct{}{},
	}
}

func parseSessionArtifact(sourceTool string, path string, data []byte, receipt *RedactionReceipt) sessionDraft {
	draft := newSessionDraft(sourceTool, path)
	if strings.ToLower(filepath.Ext(path)) == ".jsonl" {
		receipt.block("raw_transcript_jsonl")
		for _, line := range bytes.Split(data, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			var value any
			if err := json.Unmarshal(line, &value); err != nil {
				continue
			}
			if object, ok := value.(map[string]any); ok {
				processEvent(&draft, object, receipt)
			}
		}
		return draft
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return draft
	}
	receipt.block("raw_transcript_jsonl")
	processJSONValue(&draft, value, receipt)
	return draft
}

func processJSONValue(draft *sessionDraft, value any, receipt *RedactionReceipt) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok {
				processEvent(draft, object, receipt)
			} else {
				inspectRawContent(item, nil, receipt)
			}
		}
	case map[string]any:
		if events, ok := firstArray(typed, "events", "messages", "entries", "records"); ok {
			for _, item := range events {
				if object, ok := item.(map[string]any); ok {
					processEvent(draft, object, receipt)
				}
			}
			return
		}
		processEvent(draft, typed, receipt)
	default:
		inspectRawContent(typed, nil, receipt)
	}
}

func processEvent(draft *sessionDraft, event map[string]any, receipt *RedactionReceipt) {
	draft.eventCount++
	inspectRawContent(event, nil, receipt)
	recordTimestamp(draft, event)
	recordRepoHints(draft, event)
	recordSummaries(draft, event, receipt)
	recordCounts(draft, event, receipt)
	for _, key := range []string{"timestamp", "type", "role", "cwd", "branch", "commit_sha", "repo_remote", "repo_name"} {
		if findValue(event, key) != nil {
			draft.extractedFields[key] = struct{}{}
			receipt.extract(key)
		}
	}
}

func recordTimestamp(draft *sessionDraft, event map[string]any) {
	value := firstString(event, "timestamp", "created_at", "createdAt", "time", "started_at")
	if value == "" {
		return
	}
	parsed, err := parseTime(value)
	if err != nil {
		return
	}
	if draft.startedAt.IsZero() || parsed.Before(draft.startedAt) {
		draft.startedAt = parsed
	}
	if parsed.After(draft.endedAt) {
		draft.endedAt = parsed
	}
}

func recordRepoHints(draft *sessionDraft, event map[string]any) {
	if sessionID := firstString(event, "session_id", "sessionId", "conversation_id", "trace_id", "run_id", "id"); sessionID != "" && draft.sessionID == "" {
		draft.sessionID = sessionID
	}
	for _, path := range collectStringsByKey(event, "cwd", "repo_path", "project_path", "workspace", "working_dir", "root") {
		if normalized := normalizeLocalPath(path); normalized != "" {
			draft.repoPaths[normalized] = struct{}{}
		}
	}
	for _, remote := range collectStringsByKey(event, "repo_remote", "remote_url", "repository_url", "git_remote", "origin") {
		if remote != "" {
			draft.repoRemoteHashes[hashString(privacy.RedactRemoteURL(remote))] = struct{}{}
		}
	}
	for _, name := range collectStringsByKey(event, "repo_name", "repository", "repository_name", "project") {
		if name = cleanRepoName(name); name != "" {
			draft.repoNames[name] = struct{}{}
		}
	}
	for _, branch := range collectStringsByKey(event, "branch", "git_branch", "current_branch") {
		if branch = cleanBranch(branch); branch != "" {
			draft.branches[branch] = struct{}{}
		}
	}
	for _, commit := range collectStringsByKey(event, "commit", "commit_sha", "head_sha", "sha") {
		if commit = cleanSHA(commit); commit != "" {
			draft.commits[commit] = struct{}{}
		}
	}
	for _, value := range allStrings(event) {
		for _, issue := range linearIssueIDPattern.FindAllString(value, -1) {
			draft.issueKeys[issue] = struct{}{}
		}
		for _, match := range githubIssuePattern.FindAllStringSubmatch(value, -1) {
			if len(match) > 1 && match[1] != "" {
				draft.issueKeys["#"+match[1]] = struct{}{}
			}
		}
		for _, match := range prNumberPattern.FindAllStringSubmatch(value, -1) {
			for _, group := range match[1:] {
				if group == "" {
					continue
				}
				if number, err := strconv.Atoi(group); err == nil {
					draft.prNumbers[number] = struct{}{}
				}
			}
		}
	}
	for _, number := range collectNumbersByKey(event, "pr_number", "pull_request_number", "pullRequestNumber") {
		if number > 0 {
			draft.prNumbers[number] = struct{}{}
		}
	}
	for _, path := range collectPathFields(event) {
		if normalized := normalizeRepoPath(path); normalized != "" {
			draft.filePaths[normalized] = struct{}{}
		}
	}
}

func recordSummaries(draft *sessionDraft, event map[string]any, receipt *RedactionReceipt) {
	if unsafeSummaryEvent(event) {
		return
	}
	for _, item := range collectSummaryFields(event) {
		summary := sanitizeSummary(item.value, receipt)
		if summary == "" {
			continue
		}
		switch item.key {
		case "intent_summary":
			if draft.intentSummary == "" {
				draft.intentSummary = summary
				receipt.extract("intent_summary")
			}
		case "plan_summary":
			if draft.planSummary == "" {
				draft.planSummary = summary
				receipt.extract("plan_summary")
			}
		case "implementation_summary":
			if draft.implementationSummary == "" {
				draft.implementationSummary = summary
				receipt.extract("implementation_summary")
			}
		}
	}
}

func recordCounts(draft *sessionDraft, event map[string]any, receipt *RedactionReceipt) {
	eventType := strings.ToLower(firstString(event, "type", "event", "kind"))
	role := strings.ToLower(firstString(event, "role"))
	if role == "" {
		role = strings.ToLower(firstStringFromMessage(event, "role"))
	}
	allText := strings.ToLower(strings.Join(allStrings(event), " "))
	switch {
	case role == "user" || eventType == "user" || strings.Contains(eventType, "user_message"):
		draft.humanSteeringCount++
		receipt.block("raw_prompt")
	case role == "assistant" || strings.Contains(eventType, "assistant") || strings.Contains(eventType, "agent"):
		draft.agentActionCount++
		receipt.block("raw_model_output")
	case strings.Contains(eventType, "tool") || strings.Contains(eventType, "exec") || strings.Contains(eventType, "apply_patch"):
		draft.agentActionCount++
	}
	if strings.Contains(allText, "test") ||
		strings.Contains(allText, "debug") ||
		strings.Contains(allText, "go test") ||
		strings.Contains(allText, "npm test") ||
		strings.Contains(allText, "pytest") ||
		strings.Contains(allText, "lint") {
		draft.testDebugCount++
		receipt.extract("test_debug_count")
	}
	if strings.Contains(allText, "fix") ||
		strings.Contains(allText, "correction") ||
		strings.Contains(allText, "retry") ||
		strings.Contains(allText, "failed") ||
		strings.Contains(allText, "error") ||
		strings.Contains(allText, "regression") {
		draft.correctionCount++
		receipt.extract("correction_count")
	}
}

func finalizeSession(draft sessionDraft, repo gitrepo.Repo, anchor RepoAnchor, opts Options) (*WorkSession, []ConfidenceNote) {
	confidence, linked, notes := linkSession(draft, repo, anchor)
	if !linked {
		return nil, nil
	}
	if draft.startedAt.IsZero() {
		draft.startedAt = evidenceNowUTC()
	}
	if draft.endedAt.IsZero() || draft.endedAt.Before(draft.startedAt) {
		draft.endedAt = draft.startedAt
	}
	branch := firstSortedString(draft.branches)
	if branch == "" {
		branch = anchor.Branch
	}
	commits := sortedStrings(draft.commits)
	if len(commits) == 0 && anchor.rawCurrentCommitSHA != "" {
		commits = []string{anchor.rawCurrentCommitSHA}
	}
	filePaths := sortedStrings(draft.filePaths)
	filePathHashes := make([]string, 0, len(filePaths))
	for _, path := range filePaths {
		filePathHashes = append(filePathHashes, hashString(path))
	}
	session := WorkSession{
		SessionIDHash:         hashString(draft.sessionID),
		SourceTool:            draft.sourceTool,
		SourceKind:            sourceKind,
		StartedAt:             draft.startedAt,
		EndedAt:               draft.endedAt,
		Branch:                branch,
		CommitSHAHashes:       hashStrings(commits),
		PRNumbers:             sortedInts(draft.prNumbers),
		IssueKeys:             sortedStrings(draft.issueKeys),
		IntentSummary:         defaultSummary(draft.intentSummary, "Intent summary unavailable from derived metadata."),
		PlanSummary:           defaultSummary(draft.planSummary, "Plan metadata unavailable; raw transcript content was blocked by default."),
		ImplementationSummary: defaultSummary(draft.implementationSummary, derivedImplementationSummary(draft)),
		HumanSteeringCount:    draft.humanSteeringCount,
		CorrectionCount:       draft.correctionCount,
		TestDebugCount:        draft.testDebugCount,
		AgentActionCount:      draft.agentActionCount,
		FilesTouchedCount:     len(filePaths),
		FilePathHashes:        filePathHashes,
		EvidenceExcerptCount:  0,
		ExportMode:            ExportModeOffline,
		UploadMode:            UploadModeDisabled,
		Confidence:            confidence,
		LinkageNotes:          notes,
	}
	if opts.IncludeFilePaths {
		session.FilePaths = filePaths
	}
	return &session, notes
}

func linkSession(draft sessionDraft, repo gitrepo.Repo, anchor RepoAnchor) (signals.Confidence, bool, []ConfidenceNote) {
	repoRoot := normalizeLocalPath(repo.Path)
	for path := range draft.repoPaths {
		if path == repoRoot || strings.HasPrefix(path, repoRoot+"/") || strings.HasPrefix(repoRoot, path+"/") {
			return signals.ConfidenceHigh, true, []ConfidenceNote{{Scope: "repo_path", Confidence: signals.ConfidenceHigh, Note: "Session cwd or project path matched the current repo root."}}
		}
	}
	if anchor.RepoRemoteHash != "" {
		if _, ok := draft.repoRemoteHashes[anchor.RepoRemoteHash]; ok {
			return signals.ConfidenceHigh, true, []ConfidenceNote{{Scope: "repo_remote", Confidence: signals.ConfidenceHigh, Note: "Session remote URL hash matched the current repo remote."}}
		}
	}
	repoCommits := map[string]struct{}{}
	for _, commit := range anchor.rawCommitSHAs {
		repoCommits[commit] = struct{}{}
		if len(commit) >= 7 {
			repoCommits[commit[:7]] = struct{}{}
		}
	}
	for commit := range draft.commits {
		if _, ok := repoCommits[commit]; ok {
			return signals.ConfidenceHigh, true, []ConfidenceNote{{Scope: "commit", Confidence: signals.ConfidenceHigh, Note: "Session commit anchor matched local git history."}}
		}
	}
	repoName := cleanRepoName(repo.Name)
	if repoName == "" && repo.GitHubRepo != "" {
		repoName = cleanRepoName(repo.GitHubRepo)
	}
	if repoName != "" {
		if _, ok := draft.repoNames[repoName]; ok {
			if anchor.Branch != "" {
				if _, branchOK := draft.branches[anchor.Branch]; branchOK {
					return signals.ConfidenceMedium, true, []ConfidenceNote{{Scope: "repo_name_branch", Confidence: signals.ConfidenceMedium, Note: "Session repo name and branch matched the current repo."}}
				}
			}
			return signals.ConfidenceLow, true, []ConfidenceNote{{Scope: "repo_name", Confidence: signals.ConfidenceLow, Note: "Session linked by repo name only; linkage is heuristic."}}
		}
	}
	if draft.sourceTool == sourceClaude && claudeProjectName(repo.Path) != "" && strings.Contains(filepath.ToSlash(draft.sourceFile), claudeProjectName(repo.Path)) {
		return signals.ConfidenceMedium, true, []ConfidenceNote{{Scope: "claude_project", Confidence: signals.ConfidenceMedium, Note: "Claude project directory resembled the current repo path."}}
	}
	return signals.ConfidenceLow, false, nil
}

func newRedactionReceipt(now time.Time, bundleID string, opts Options) RedactionReceipt {
	return RedactionReceipt{
		ID:                  "red_" + shortHash(bundleID+"|redaction"),
		CreatedAt:           now,
		BundleID:            bundleID,
		DerivedEvidenceOnly: true,
		BlockedContent:      map[string]int{},
		RedactedContent:     map[string]int{},
		PrivacyFlags:        privacyFlags(opts),
		UploadMode:          UploadModeDisabled,
		ExportMode:          ExportModeOffline,
	}
}

func (r *RedactionReceipt) block(class string) {
	if class == "" {
		return
	}
	r.BlockedContent[class]++
}

func (r *RedactionReceipt) redact(class string) {
	if class == "" {
		return
	}
	r.RedactedContent[class]++
}

func (r *RedactionReceipt) extract(field string) {
	if field == "" {
		return
	}
	for _, existing := range r.ExtractedFieldNames {
		if existing == field {
			return
		}
	}
	r.ExtractedFieldNames = append(r.ExtractedFieldNames, field)
}

func inspectRawContent(value any, keyPath []string, receipt *RedactionReceipt) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			inspectRawContent(child, append(keyPath, strings.ToLower(key)), receipt)
		}
	case []any:
		for _, child := range typed {
			inspectRawContent(child, keyPath, receipt)
		}
	case string:
		classifyStringContent(typed, keyPath, receipt)
	}
}

func classifyStringContent(value string, keyPath []string, receipt *RedactionReceipt) {
	lowerKey := strings.Join(keyPath, ".")
	lowerValue := strings.ToLower(value)
	switch {
	case strings.Contains(lowerKey, "prompt"):
		receipt.block("raw_prompt")
	case strings.Contains(lowerKey, "completion") || strings.Contains(lowerKey, "model_output") || strings.Contains(lowerKey, "assistant"):
		receipt.block("raw_model_output")
	case strings.Contains(lowerKey, "diff") || strings.Contains(lowerKey, "patch"):
		receipt.block("raw_diff")
	case strings.Contains(lowerKey, "stdout") || strings.Contains(lowerKey, "stderr") || strings.Contains(lowerKey, "terminal") || strings.Contains(lowerKey, "log"):
		receipt.block("raw_terminal_log")
	case strings.Contains(lowerKey, "file_content") || strings.Contains(lowerKey, "source") || strings.Contains(lowerKey, "code"):
		receipt.block("source_code")
	}
	if strings.Contains(lowerKey, "content") && looksLikeSourceCode(value) {
		receipt.block("source_code")
	}
	if privacy.ContainsSecretLikeValue(value) {
		receipt.redact("secret")
	}
	if privateKeyPattern.MatchString(value) {
		receipt.redact("private_key")
	}
	if envAssignmentPattern.MatchString(value) {
		receipt.redact("env_value")
	}
	if strings.Contains(value, "://") && privacy.RedactRemoteURL(value) != value {
		receipt.redact("credential_url")
	}
	if strings.Contains(lowerValue, "bearer ") {
		receipt.redact("secret")
	}
}

func sanitizeSummary(value string, receipt *RedactionReceipt) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "\n", " ")
	redacted := privacy.RedactSecretLikeText(value)
	if redacted != value {
		receipt.redact("secret")
		value = redacted
	}
	if privateKeyPattern.MatchString(value) {
		receipt.redact("private_key")
		return ""
	}
	if len(value) > 220 {
		value = value[:220]
	}
	if looksLikeSourceCode(value) {
		receipt.block("source_code")
		return ""
	}
	return value
}

func privacyFlags(opts Options) PrivacyFlags {
	return PrivacyFlags{
		PublicSafe:              true,
		DerivedEvidenceOnly:     true,
		FilePathsIncluded:       opts.IncludeFilePaths,
		FilePathHashesIncluded:  true,
		RepoNameIncluded:        opts.IncludeRepoName,
		RawPromptsIncluded:      false,
		RawModelOutputsIncluded: false,
		RawTranscriptsIncluded:  false,
		RawDiffsIncluded:        false,
		RawLogsIncluded:         false,
		SourceCodeIncluded:      false,
		SecretsIncluded:         false,
		EnvValuesIncluded:       false,
		PrivateKeysIncluded:     false,
		CredentialURLsIncluded:  false,
		LocalPathsIncluded:      false,
		UploadEnabled:           false,
	}
}

func withDefaultSourceDirs(opts Options) Options {
	home, _ := os.UserHomeDir()
	if opts.ClaudeDir == "" && home != "" {
		opts.ClaudeDir = filepath.Join(home, ".claude", "projects")
	}
	if opts.CodexDir == "" && home != "" {
		opts.CodexDir = filepath.Join(home, ".codex", "sessions")
	}
	return opts
}

func looksLikeGitURL(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "ssh://") ||
		strings.HasPrefix(lower, "git://") ||
		strings.HasPrefix(lower, "git@")
}

func selectedSources(values []string) []string {
	if len(values) == 0 {
		return []string{sourceClaude, sourceCodex}
	}
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "claude", "claude_code", "claude-code":
			if _, ok := seen[sourceClaude]; !ok {
				out = append(out, sourceClaude)
				seen[sourceClaude] = struct{}{}
			}
		case "codex", "codex_cli", "codex-cli":
			if _, ok := seen[sourceCodex]; !ok {
				out = append(out, sourceCodex)
				seen[sourceCodex] = struct{}{}
			}
		}
	}
	if len(out) == 0 {
		return []string{sourceClaude, sourceCodex}
	}
	return out
}

func evidenceOutputDir(repoPath string, output string, now time.Time) (string, error) {
	root := output
	if root == "" {
		cfg, _, err := config.Load(repoPath)
		if err != nil {
			return "", err
		}
		root = cfg.Reports.OutputDir
	}
	if root == "" {
		root = filepath.Join(repoPath, ".contribution", "reports")
	}
	if !filepath.IsAbs(root) {
		root = filepath.Join(repoPath, root)
	}
	return filepath.Join(root, timestamp(now)), nil
}

func recentCommitSHAs(ctx context.Context, repoPath string, limit int) []string {
	out, err := gitCommand(ctx, repoPath, "log", "-n", strconv.Itoa(limit), "--format=%H")
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var commits []string
	for _, line := range lines {
		if sha := cleanSHA(line); sha != "" {
			commits = append(commits, sha)
		}
	}
	return commits
}

func currentDiffFileCount(ctx context.Context, repoPath string) int {
	out, err := gitCommand(ctx, repoPath, "diff", "--name-only", "HEAD")
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func gitCommand(ctx context.Context, dir string, args ...string) (string, error) {
	// #nosec G204 -- arguments are fixed by the CLI; no shell is used.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", filepath.Base(path), err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func parseTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time %q", value)
}

func timestamp(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

func sourceStatus(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "missing"
	}
	if !info.IsDir() {
		return "not_directory"
	}
	return "available"
}

func scannedSources(summaries []SourceScanSummary) []string {
	out := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		out = append(out, summary.SourceTool)
	}
	return out
}

func sumSessionsFound(summaries []SourceScanSummary) int {
	total := 0
	for _, summary := range summaries {
		total += summary.SessionsFound
	}
	return total
}

func sumCounts(values map[string]int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func sortedCountKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if value > 0 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func bundleConfidence(sessions []WorkSession) signals.Confidence {
	if len(sessions) == 0 {
		return signals.ConfidenceLow
	}
	high := 0
	medium := 0
	for _, session := range sessions {
		switch session.Confidence {
		case signals.ConfidenceHigh:
			high++
		case signals.ConfidenceMedium:
			medium++
		}
	}
	if high == len(sessions) {
		return signals.ConfidenceHigh
	}
	if high+medium > 0 {
		return signals.ConfidenceMedium
	}
	return signals.ConfidenceLow
}

func limitations(summaries []SourceScanSummary, sessions []WorkSession) []string {
	var out []string
	for _, summary := range summaries {
		if summary.Status != "available" {
			out = append(out, fmt.Sprintf("%s source is %s.", summary.SourceTool, summary.Status))
		}
		if summary.UnreadableCount > 0 {
			out = append(out, fmt.Sprintf("%s had %d unreadable artifact(s).", summary.SourceTool, summary.UnreadableCount))
		}
	}
	if len(sessions) == 0 {
		out = append(out, "No local AI work sessions could be linked to the current repo.")
	}
	out = append(out, "Authenticated upload is disabled until the CLI consumes a finalized website receiving contract.")
	return out
}

func firstString(value any, keys ...string) string {
	for _, key := range keys {
		if found := findValue(value, key); found != nil {
			if text, ok := found.(string); ok {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}

func firstStringFromMessage(value any, key string) string {
	if message := findValue(value, "message"); message != nil {
		return firstString(message, key)
	}
	if payload := findValue(value, "payload"); payload != nil {
		return firstString(payload, key)
	}
	return ""
}

func firstArray(value map[string]any, keys ...string) ([]any, bool) {
	for _, key := range keys {
		if found := findValue(value, key); found != nil {
			if array, ok := found.([]any); ok {
				return array, true
			}
		}
	}
	return nil, false
}

func findValue(value any, key string) any {
	switch typed := value.(type) {
	case map[string]any:
		for k, v := range typed {
			if strings.EqualFold(k, key) {
				return v
			}
		}
		for _, v := range typed {
			if found := findValue(v, key); found != nil {
				return found
			}
		}
	case []any:
		for _, item := range typed {
			if found := findValue(item, key); found != nil {
				return found
			}
		}
	}
	return nil
}

func collectStringsByKey(value any, keys ...string) []string {
	keySet := map[string]struct{}{}
	for _, key := range keys {
		keySet[strings.ToLower(key)] = struct{}{}
	}
	var out []string
	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case map[string]any:
			for key, child := range typed {
				if _, ok := keySet[strings.ToLower(key)]; ok {
					if text, ok := child.(string); ok {
						out = append(out, strings.TrimSpace(text))
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return out
}

func collectNumbersByKey(value any, keys ...string) []int {
	keySet := map[string]struct{}{}
	for _, key := range keys {
		keySet[strings.ToLower(key)] = struct{}{}
	}
	var out []int
	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case map[string]any:
			for key, child := range typed {
				if _, ok := keySet[strings.ToLower(key)]; ok {
					switch value := child.(type) {
					case float64:
						out = append(out, int(value))
					case string:
						if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
							out = append(out, parsed)
						}
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return out
}

func allStrings(value any) []string {
	var out []string
	var walk func(any, string)
	walk = func(node any, key string) {
		switch typed := node.(type) {
		case map[string]any:
			for childKey, child := range typed {
				walk(child, childKey)
			}
		case []any:
			for _, child := range typed {
				walk(child, key)
			}
		case string:
			out = append(out, typed)
		}
	}
	walk(value, "")
	return out
}

type summaryField struct {
	key   string
	value string
}

func collectSummaryFields(value any) []summaryField {
	allowed := map[string]struct{}{
		"intent_summary":         {},
		"plan_summary":           {},
		"implementation_summary": {},
	}
	var out []summaryField
	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case map[string]any:
			for key, child := range typed {
				lower := strings.ToLower(key)
				if _, ok := allowed[lower]; ok {
					if text, ok := child.(string); ok {
						out = append(out, summaryField{key: lower, value: text})
					}
				}
				if sensitiveKey(lower) {
					continue
				}
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return out
}

func unsafeSummaryEvent(event map[string]any) bool {
	eventType := strings.ToLower(firstString(event, "type", "event", "kind"))
	role := strings.ToLower(firstString(event, "role"))
	if role == "" {
		role = strings.ToLower(firstStringFromMessage(event, "role"))
	}
	switch role {
	case "user", "assistant", "tool":
		return true
	}
	for _, marker := range []string{
		"user",
		"prompt",
		"assistant",
		"completion",
		"model_output",
		"tool",
		"exec",
		"apply_patch",
		"patch",
		"diff",
		"log",
		"terminal",
		"stdout",
		"stderr",
	} {
		if strings.Contains(eventType, marker) {
			return true
		}
	}
	return hasUnsafeSummaryKey(event)
}

func hasUnsafeSummaryKey(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if unsafeSummaryKey(key) || hasUnsafeSummaryKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if hasUnsafeSummaryKey(child) {
				return true
			}
		}
	}
	return false
}

func unsafeSummaryKey(key string) bool {
	lower := strings.ToLower(key)
	return lower == "content" ||
		lower == "message" ||
		lower == "messages" ||
		lower == "stdout" ||
		lower == "stderr" ||
		strings.Contains(lower, "prompt") ||
		strings.Contains(lower, "completion") ||
		strings.Contains(lower, "model_output") ||
		strings.Contains(lower, "assistant_output") ||
		strings.Contains(lower, "terminal") ||
		strings.Contains(lower, "diff") ||
		strings.Contains(lower, "patch") ||
		strings.Contains(lower, "log")
}

func collectPathFields(value any) []string {
	var out []string
	var walk func(any, string)
	walk = func(node any, key string) {
		switch typed := node.(type) {
		case map[string]any:
			for childKey, child := range typed {
				walk(child, childKey)
			}
		case []any:
			for _, child := range typed {
				walk(child, key)
			}
		case string:
			lower := strings.ToLower(key)
			if lower == "file_path" || lower == "filepath" || lower == "path" || lower == "file" {
				out = append(out, typed)
			}
		}
	}
	walk(value, "")
	return out
}

func sensitiveKey(key string) bool {
	return strings.Contains(key, "prompt") ||
		strings.Contains(key, "completion") ||
		strings.Contains(key, "output") ||
		strings.Contains(key, "content") ||
		strings.Contains(key, "text") ||
		strings.Contains(key, "diff") ||
		strings.Contains(key, "patch") ||
		strings.Contains(key, "log") ||
		strings.Contains(key, "stdout") ||
		strings.Contains(key, "stderr")
}

func looksLikeSourceCode(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "package ") ||
		strings.Contains(lower, "func ") ||
		strings.Contains(lower, "class ") ||
		strings.Contains(lower, "function ") ||
		strings.Contains(lower, "import ") ||
		strings.Contains(value, "#include ")
}

func cleanRepoName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, ".git")
	value = filepath.Base(value)
	return strings.ToLower(value)
}

func cleanBranch(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "refs/heads/")
	return value
}

func cleanSHA(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if !hexSHASevenPattern.MatchString(value) {
		return ""
	}
	return hexSHASevenPattern.FindString(value)
}

func normalizeLocalPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "~") {
		return ""
	}
	if !filepath.IsAbs(value) {
		return ""
	}
	cleaned, err := filepath.Abs(value)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(cleaned))
}

func normalizeRepoPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) || strings.HasPrefix(value, "~") {
		return ""
	}
	value = filepath.ToSlash(filepath.Clean(value))
	if value == "." || strings.HasPrefix(value, "../") || strings.Contains(value, "\n") {
		return ""
	}
	if !pathLikePattern.MatchString(value) {
		return ""
	}
	return value
}

func defaultSummary(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func derivedImplementationSummary(draft sessionDraft) string {
	return fmt.Sprintf("Derived counts: %d agent action(s), %d touched file reference(s), %d test/debug event(s).", draft.agentActionCount, len(draft.filePaths), draft.testDebugCount)
}

func firstSortedString(values map[string]struct{}) string {
	sorted := sortedStrings(values)
	if len(sorted) == 0 {
		return ""
	}
	return sorted[0]
}

func sortedStrings(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func sortedInts(values map[int]struct{}) []int {
	out := make([]int, 0, len(values))
	for value := range values {
		if value > 0 {
			out = append(out, value)
		}
	}
	sort.Ints(out)
	return out
}

func hashString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func hashStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		hash := hashString(value)
		if hash == "" {
			continue
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		out = append(out, hash)
	}
	sort.Strings(out)
	return out
}

func shortHash(value string) string {
	hash := hashString(value)
	if len(hash) < 16 {
		return hash
	}
	return hash[:16]
}

func claudeProjectName(repoPath string) string {
	value := normalizeLocalPath(repoPath)
	if value == "" {
		return ""
	}
	return "-" + strings.ReplaceAll(strings.Trim(value, "/"), "/", "-")
}
