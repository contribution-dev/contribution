// Package valuepipeline builds deterministic readiness and attribution evidence.
package valuepipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/contribution-dev/contribution/internal/config"
	gitrepo "github.com/contribution-dev/contribution/internal/git"
	"github.com/contribution-dev/contribution/internal/github"
	"github.com/contribution-dev/contribution/internal/signals"
)

const maxArtifactBytes = 256 * 1024

var (
	linearIssuePattern = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-\d+\b`)
	ghIssuePattern     = regexp.MustCompile(`(?:^|[\s(])#\d+\b`)
)

// Input contains the local evidence needed to build value-pipeline fields.
type Input struct {
	GeneratedAt          time.Time
	Repo                 gitrepo.Repo
	Config               config.Config
	Inventory            signals.FileSummary
	History              gitrepo.History
	GitHub               github.Metadata
	Coverage             signals.CoverageSummary
	Tooling              signals.ToolingReport
	GitHubTokenAvailable bool
	ExternalToolsAllowed bool
	AgentArtifacts       []signals.AgentArtifactMetadata
	WorkUnitMarkers      []signals.WorkUnitMarker
}

// Output stores all additive value-pipeline fields for analysis.json and bundles.
type Output struct {
	AgenticReadiness       signals.AgenticReadiness
	SourceCoverage         signals.SourceCoverage
	DataGaps               []signals.DataGap
	RecommendedConnections []signals.RecommendedConnection
	AttributionReadiness   signals.AttributionReadiness
	WorkUnitCandidates     []signals.WorkUnitCandidate
	Limitations            []string
}

type repoEvidence struct {
	instructionFiles []string
	validation       validationEvidence
	hasCI            bool
	hasReadme        bool
}

type validationEvidence struct {
	commands []string
	reason   string
}

// Build computes deterministic value-pipeline evidence.
func Build(input Input) Output {
	evidence := inspectRepo(input.Repo.Path, input.Config)
	attribution, candidates := buildAttribution(input)
	sourceCoverage, gaps, recommendations := buildSourceCoverage(input, evidence, attribution)
	readiness := buildReadiness(input, evidence, attribution, sourceCoverage)
	return Output{
		AgenticReadiness:       readiness,
		SourceCoverage:         sourceCoverage,
		DataGaps:               gaps,
		RecommendedConnections: recommendations,
		AttributionReadiness:   attribution,
		WorkUnitCandidates:     candidates,
		Limitations:            readiness.Limitations,
	}
}

// InspectAgentArtifacts imports metadata only from explicitly supplied artifacts.
func InspectAgentArtifacts(paths []string, include bool, repoPath string) ([]signals.AgentArtifactMetadata, []string, error) {
	if len(paths) > 0 && !include {
		return nil, nil, fmt.Errorf("--include-agent-artifacts is required when --agent-artifact is provided")
	}
	if !include {
		return nil, nil, nil
	}
	if len(paths) == 0 {
		return nil, []string{"Agent artifact metadata import was requested, but no --agent-artifact path was provided."}, nil
	}
	out := make([]signals.AgentArtifactMetadata, 0, len(paths))
	for _, path := range paths {
		out = append(out, inspectAgentArtifact(path, repoPath))
	}
	return out, nil, nil
}

func inspectRepo(repoPath string, cfg config.Config) repoEvidence {
	return repoEvidence{
		instructionFiles: detectInstructionFiles(repoPath),
		validation:       detectValidation(repoPath, cfg),
		hasCI:            hasPath(repoPath, ".github/workflows"),
		hasReadme:        hasAnyPath(repoPath, "README.md", "README", "docs/README.md"),
	}
}

func buildSourceCoverage(input Input, evidence repoEvidence, attribution signals.AttributionReadiness) (signals.SourceCoverage, []signals.DataGap, []signals.RecommendedConnection) {
	items := []signals.SourceCoverageItem{
		localGitCoverage(input),
		githubCoverage(input),
		issueTrackerCoverage(),
		coverageCoverage(input),
		ciCoverage(evidence),
		instructionCoverage(evidence),
		validationCoverage(evidence),
		optionalToolCoverage(input),
		aiSpendCoverage(input),
		agentSessionCoverage(input),
		deploymentProductCoverage(),
	}
	available := 0
	partial := 0
	for _, item := range items {
		switch item.Status {
		case signals.SourceCoverageAvailable:
			available++
		case signals.SourceCoveragePartial:
			partial++
		}
	}
	confidence := signals.ConfidenceLow
	if available >= 6 {
		confidence = signals.ConfidenceHigh
	} else if available+partial >= 4 {
		confidence = signals.ConfidenceMedium
	}
	var next []string
	for _, item := range items {
		if item.NextAction != "" && item.Status != signals.SourceCoverageAvailable {
			next = append(next, item.NextAction)
		}
		if len(next) >= 5 {
			break
		}
	}
	summary := fmt.Sprintf("%d of %d evidence sources are available; the report stays at %s confidence where spend or attribution data is missing.", available, len(items), confidence)
	if attribution.Confidence == signals.ConfidenceHigh {
		summary = fmt.Sprintf("%d of %d evidence sources are available; attribution evidence is strong enough for work-unit grouping.", available, len(items))
	}
	coverage := signals.SourceCoverage{
		GeneratedAt: input.GeneratedAt,
		Summary:     summary,
		Confidence:  confidence,
		Sources:     items,
		NextActions: next,
	}
	return coverage, dataGaps(items), recommendedConnections(items)
}

func localGitCoverage(input Input) signals.SourceCoverageItem {
	status := signals.SourceCoverageAvailable
	evidence := fmt.Sprintf("%d recent commit artifact(s) and %d unique changed file(s) were collected.", len(input.History.Commits), len(input.History.FileTouchCount))
	confidence := signals.ConfidenceHigh
	if len(input.History.Commits) == 0 {
		status = signals.SourceCoveragePartial
		evidence = "Local git is available, but no commits were found in the analysis window."
		confidence = signals.ConfidenceMedium
	}
	return coverageItem("local_git_history", "Local git history", "repo", status, evidence, "Shows local change scope, churn, and durability proxies.", "Local engineering outcome evidence.", "", confidence)
}

func githubCoverage(input Input) signals.SourceCoverageItem {
	if input.GitHub.Available && len(input.GitHub.PRs) > 0 {
		return coverageItem("github_metadata", "GitHub metadata", "engineering", signals.SourceCoverageAvailable, fmt.Sprintf("%d merged PR(s) were imported.", len(input.GitHub.PRs)), "Adds PR, review, check, and merge evidence.", "Review burden and PR-level durability.", "", signals.ConfidenceHigh)
	}
	if input.GitHubTokenAvailable {
		reason := input.GitHub.Reason
		if reason == "" {
			reason = "GitHub metadata was requested but did not return merged PR evidence."
		}
		return coverageItem("github_metadata", "GitHub metadata", "engineering", signals.SourceCoveragePartial, reason, "Adds PR, review, check, and merge evidence.", "Review burden and PR-level durability.", "Fix GitHub metadata access or widen the analysis window.", signals.ConfidenceMedium)
	}
	return coverageItem("github_metadata", "GitHub metadata", "engineering", signals.SourceCoverageRequiresWebConnection, "GitHub metadata was not connected for this run.", "Adds PR, review, check, and merge evidence.", "Review burden and PR-level durability.", "Connect GitHub in the web app or run analyze/probe with --github-token gh.", signals.ConfidenceLow)
}

func issueTrackerCoverage() signals.SourceCoverageItem {
	return coverageItem("issue_tracker", "Issue tracker", "intent", signals.SourceCoverageRequiresWebConnection, "Linear, Jira, GitHub Issues, or project metadata is not available to the local CLI.", "Connects stated intent to shipped work.", "Higher-confidence work-unit grouping.", "Connect the issue tracker in the web app.", signals.ConfidenceLow)
}

func coverageCoverage(input Input) signals.SourceCoverageItem {
	if input.Coverage.Status == "available" {
		return coverageItem("coverage_report", "Coverage report", "validation", signals.SourceCoverageAvailable, fmt.Sprintf("Imported coverage covers %.1f%% of executable lines.", input.Coverage.Percent), "Adds direct test coverage evidence.", "Validation confidence.", "", signals.ConfidenceMedium)
	}
	reason := input.Coverage.Reason
	if reason == "" {
		reason = "No coverage report was imported."
	}
	return coverageItem("coverage_report", "Coverage report", "validation", signals.SourceCoverageMissing, reason, "Adds direct test coverage evidence.", "Validation confidence.", "Import Go or LCOV coverage with --coverage.", signals.ConfidenceLow)
}

func ciCoverage(evidence repoEvidence) signals.SourceCoverageItem {
	if evidence.hasCI {
		return coverageItem("ci_configuration", "CI/test configuration", "validation", signals.SourceCoverageAvailable, "Repository contains .github/workflows.", "Shows repeatable validation exists.", "CI-backed readiness evidence.", "", signals.ConfidenceMedium)
	}
	return coverageItem("ci_configuration", "CI/test configuration", "validation", signals.SourceCoverageMissing, "No .github/workflows directory was detected.", "Shows repeatable validation exists.", "CI-backed readiness evidence.", "Add or connect CI checks so validation outcomes are visible.", signals.ConfidenceLow)
}

func instructionCoverage(evidence repoEvidence) signals.SourceCoverageItem {
	if len(evidence.instructionFiles) > 0 {
		return coverageItem("repo_instructions", "Repo instructions", "readiness", signals.SourceCoverageAvailable, "Found "+strings.Join(evidence.instructionFiles, ", ")+".", "Agent instructions reduce context thrash.", "Agentic readiness scoring.", "", signals.ConfidenceHigh)
	}
	return coverageItem("repo_instructions", "Repo instructions", "readiness", signals.SourceCoverageMissing, "No repo-level agent instruction file was detected.", "Agent instructions reduce context thrash.", "Agentic readiness scoring.", "Add AGENTS.md or equivalent repo instructions.", signals.ConfidenceLow)
}

func validationCoverage(evidence repoEvidence) signals.SourceCoverageItem {
	if len(evidence.validation.commands) > 0 {
		return coverageItem("validation_commands", "Local validation commands", "validation", signals.SourceCoverageAvailable, "Detected "+strings.Join(evidence.validation.commands, ", ")+".", "Gives agents a clear way to check their work.", "Agent workflow efficiency.", "", signals.ConfidenceMedium)
	}
	return coverageItem("validation_commands", "Local validation commands", "validation", signals.SourceCoverageMissing, evidence.validation.reason, "Gives agents a clear way to check their work.", "Agent workflow efficiency.", "Document one local test/check command in package scripts, Makefile, or .contribution.yml.", signals.ConfidenceLow)
}

func optionalToolCoverage(input Input) signals.SourceCoverageItem {
	if !input.ExternalToolsAllowed {
		return coverageItem("optional_static_tools", "Optional static/security tools", "safety", signals.SourceCoverageNotRequested, "Optional external tool discovery was skipped.", "Adds secret, vulnerability, dependency, and static-analysis signals.", "Safety confidence.", "Run without --no-external-tools or install optional tools.", signals.ConfidenceLow)
	}
	available := 0
	total := 0
	for _, tool := range input.Tooling.Tools {
		if tool.Required {
			continue
		}
		total++
		if tool.Available {
			available++
		}
	}
	if available > 0 {
		return coverageItem("optional_static_tools", "Optional static/security tools", "safety", signals.SourceCoveragePartial, fmt.Sprintf("%d of %d optional tool(s) are available.", available, total), "Adds secret, vulnerability, dependency, and static-analysis signals.", "Safety confidence.", "Install remaining optional analyzers for broader safety evidence.", signals.ConfidenceMedium)
	}
	return coverageItem("optional_static_tools", "Optional static/security tools", "safety", signals.SourceCoverageMissing, "No optional static/security tools were available.", "Adds secret, vulnerability, dependency, and static-analysis signals.", "Safety confidence.", "Run contribution doctor for optional analyzer setup.", signals.ConfidenceLow)
}

func aiSpendCoverage(input Input) signals.SourceCoverageItem {
	for _, artifact := range input.AgentArtifacts {
		if artifact.Status == "available" && (artifact.TokenCount > 0 || artifact.CostUSD > 0) {
			return coverageItem("ai_spend_telemetry", "AI spend telemetry", "spend", signals.SourceCoveragePartial, "Explicitly supplied agent artifact metadata included token or cost fields.", "Connects AI spend to engineering outcomes.", "Early spend confidence.", "Connect provider admin APIs or gateway telemetry for complete spend coverage.", signals.ConfidenceMedium)
		}
	}
	return coverageItem("ai_spend_telemetry", "AI spend telemetry", "spend", signals.SourceCoverageRequiresAdmin, "No provider, gateway, OpenTelemetry, or artifact token/cost metadata is available.", "Connects AI spend to engineering outcomes.", "Engineering ROI.", "Connect provider admin usage APIs, gateway telemetry, or OTel ingestion in the web app.", signals.ConfidenceLow)
}

func agentSessionCoverage(input Input) signals.SourceCoverageItem {
	if len(input.AgentArtifacts) == 0 {
		return coverageItem("agent_session_telemetry", "Agent session telemetry", "spend", signals.SourceCoverageFutureInstrumentation, "No agent session telemetry was imported.", "Links prompt/session work to commits and PRs.", "Session-to-work attribution.", "Emit OpenTelemetry or pass metadata artifacts with --include-agent-artifacts --agent-artifact.", signals.ConfidenceLow)
	}
	available := 0
	for _, artifact := range input.AgentArtifacts {
		if artifact.Status == "available" {
			available++
		}
	}
	if available > 0 {
		return coverageItem("agent_session_telemetry", "Agent session telemetry", "spend", signals.SourceCoveragePartial, fmt.Sprintf("%d metadata artifact(s) were imported without prompt or completion content.", available), "Links prompt/session work to commits and PRs.", "Session-to-work attribution.", "Use stable work-unit anchors or OTel for stronger attribution.", signals.ConfidenceMedium)
	}
	return coverageItem("agent_session_telemetry", "Agent session telemetry", "spend", signals.SourceCoveragePartial, "Agent artifact paths were supplied, but no supported metadata was imported.", "Links prompt/session work to commits and PRs.", "Session-to-work attribution.", "Export metadata-only JSON artifacts or configure OpenTelemetry.", signals.ConfidenceLow)
}

func deploymentProductCoverage() signals.SourceCoverageItem {
	return coverageItem("deployment_product_telemetry", "Deployment/product telemetry", "business", signals.SourceCoverageRequiresWebConnection, "Deployments, incidents, analytics, support, and billing are outside the local CLI.", "Connects engineering output to business outcomes.", "Product ROI.", "Connect deployments, incidents, analytics, support, and billing in the web app when Phase 3 is enabled.", signals.ConfidenceLow)
}

func coverageItem(id, label, category string, status signals.SourceCoverageStatus, evidence, why, unlocks, next string, confidence signals.Confidence) signals.SourceCoverageItem {
	return signals.SourceCoverageItem{
		ID:         id,
		Label:      label,
		Category:   category,
		Status:     status,
		Evidence:   evidence,
		Why:        why,
		Unlocks:    unlocks,
		NextAction: next,
		Confidence: confidence,
	}
}

func dataGaps(items []signals.SourceCoverageItem) []signals.DataGap {
	out := make([]signals.DataGap, 0, len(items))
	for _, item := range items {
		if item.Status == signals.SourceCoverageAvailable {
			continue
		}
		if item.NextAction == "" {
			continue
		}
		out = append(out, signals.DataGap{
			ID:               item.ID,
			Label:            item.Label,
			Status:           item.Status,
			Why:              item.Why,
			Unlocks:          item.Unlocks,
			NextAction:       item.NextAction,
			ConfidenceImpact: confidenceImpact(item.Status),
		})
	}
	return out
}

func recommendedConnections(items []signals.SourceCoverageItem) []signals.RecommendedConnection {
	out := make([]signals.RecommendedConnection, 0, len(items))
	for _, item := range items {
		if item.Status == signals.SourceCoverageAvailable || item.NextAction == "" {
			continue
		}
		connection := signals.RecommendedConnection{
			ID:       item.ID,
			Label:    item.NextAction,
			Category: item.Category,
			Why:      item.Why,
			Unlocks:  item.Unlocks,
		}
		switch item.ID {
		case "github_metadata":
			connection.Command = "contribution probe --repo . --github-token gh --output /tmp/contribution-probe"
		case "coverage_report":
			connection.Command = "contribution analyze --repo . --coverage coverage.out --coverage-format go"
		case "ai_spend_telemetry":
			connection.RequiresAdmin = true
		}
		out = append(out, connection)
	}
	return out
}

func confidenceImpact(status signals.SourceCoverageStatus) string {
	switch status {
	case signals.SourceCoverageRequiresAdmin, signals.SourceCoverageRequiresWebConnection, signals.SourceCoverageFutureInstrumentation:
		return "high"
	case signals.SourceCoverageMissing, signals.SourceCoveragePartial:
		return "medium"
	default:
		return "low"
	}
}

func buildReadiness(input Input, evidence repoEvidence, attribution signals.AttributionReadiness, sourceCoverage signals.SourceCoverage) signals.AgenticReadiness {
	components := []signals.ReadinessComponent{
		instructionScore(evidence),
		validationScore(evidence),
		testingScore(input),
		contextScore(input),
		architectureScore(input, evidence),
		attributionScore(attribution),
		safetyScore(input),
	}
	totalWeight := 0
	weighted := 0
	for _, component := range components {
		totalWeight += component.Weight
		weighted += component.Score * component.Weight
	}
	score := 0
	if totalWeight > 0 {
		score = weighted / totalWeight
	}
	topActions := readinessActions(components)
	confidence := sourceCoverage.Confidence
	if input.GitHub.Available && len(input.GitHub.PRs) > 0 && input.Coverage.Status == "available" {
		confidence = signals.ConfidenceHigh
	} else if confidence == signals.ConfidenceHigh && attribution.Confidence != signals.ConfidenceHigh {
		confidence = signals.ConfidenceMedium
	}
	var limitations []string
	if !input.GitHubTokenAvailable {
		limitations = append(limitations, "GitHub metadata was not connected, so review and PR workflow evidence are limited.")
	}
	if input.Coverage.Status != "available" {
		limitations = append(limitations, "No coverage report was imported, so testing confidence uses file-touch and repo-shape evidence.")
	}
	if attribution.Confidence == signals.ConfidenceLow {
		limitations = append(limitations, "Attribution evidence is weak; work-unit ROI should stay coarse until stronger anchors are connected.")
	}
	return signals.AgenticReadiness{
		Score:      score,
		Grade:      grade(score),
		Confidence: confidence,
		Summary:    fmt.Sprintf("Your repo is a %s (%d/100) for agentic readiness with %s confidence.", grade(score), score, confidence),
		Components: components,
		TopActions: topActions,
		Evidence: []string{
			fmt.Sprintf("%d source file(s), %d test file(s), and %d recent commit artifact(s) were inspected.", input.Inventory.SourceFiles, input.Inventory.TestFiles, len(input.History.Commits)),
			fmt.Sprintf("%d source coverage item(s) were evaluated.", len(sourceCoverage.Sources)),
		},
		Limitations: limitations,
	}
}

func instructionScore(evidence repoEvidence) signals.ReadinessComponent {
	if len(evidence.instructionFiles) == 0 {
		return component("instruction_quality", "Instruction quality", 35, 18, signals.ConfidenceMedium, "No repo-level agent instruction file was detected.", "Add AGENTS.md with repo-specific commands, architecture notes, and safety rules.")
	}
	score := 75
	if len(evidence.instructionFiles) >= 2 {
		score = 85
	}
	return component("instruction_quality", "Instruction quality", score, 18, signals.ConfidenceHigh, "Found "+strings.Join(evidence.instructionFiles, ", ")+".", "Keep instructions short, current, and explicit about validation.")
}

func validationScore(evidence repoEvidence) signals.ReadinessComponent {
	if len(evidence.validation.commands) == 0 {
		return component("validation_readiness", "Validation readiness", 35, 18, signals.ConfidenceMedium, evidence.validation.reason, "Document one reliable local validation command agents can run.")
	}
	score := 75
	if len(evidence.validation.commands) >= 2 && evidence.hasCI {
		score = 90
	}
	return component("validation_readiness", "Validation readiness", score, 18, signals.ConfidenceMedium, "Detected "+strings.Join(evidence.validation.commands, ", ")+".", "")
}

func testingScore(input Input) signals.ReadinessComponent {
	if input.Coverage.Status == "available" {
		score := 65
		if input.Coverage.Percent >= 80 {
			score = 90
		} else if input.Coverage.Percent >= 60 {
			score = 80
		}
		return component("testing_evidence", "Testing evidence", score, 16, signals.ConfidenceMedium, fmt.Sprintf("Coverage import reports %.1f%% line coverage.", input.Coverage.Percent), "Keep coverage import current for readiness and ROI confidence.")
	}
	if input.Inventory.TestFiles > 0 {
		return component("testing_evidence", "Testing evidence", 65, 16, signals.ConfidenceLow, fmt.Sprintf("Repository has %d test file(s), but no coverage report was imported.", input.Inventory.TestFiles), "Import coverage so test evidence is direct rather than inferred.")
	}
	return component("testing_evidence", "Testing evidence", 25, 16, signals.ConfidenceMedium, "No test files or coverage report were detected.", "Add tests around behavior-changing code and import coverage.")
}

func contextScore(input Input) signals.ReadinessComponent {
	files := input.Inventory.TotalFiles
	score := 85
	evidence := fmt.Sprintf("Repository inventory found %d file(s).", files)
	switch {
	case files == 0:
		score = 40
	case files > 5000:
		score = 45
		evidence += " Large repos usually need tighter agent context routing."
	case files > 1500:
		score = 65
		evidence += " Context may need filtering for efficient agent work."
	}
	if input.Inventory.GeneratedFiles+input.Inventory.VendorFiles > input.Inventory.SourceFiles && input.Inventory.SourceFiles > 0 {
		score -= 10
		evidence += " Generated/vendor files outnumber source files."
	}
	return component("context_efficiency", "Context efficiency", clamp(score), 12, signals.ConfidenceMedium, evidence, "Add focused docs, ignore generated context, or split agent tasks when context is large.")
}

func architectureScore(input Input, evidence repoEvidence) signals.ReadinessComponent {
	score := 55
	parts := []string{}
	if evidence.hasReadme {
		score += 15
		parts = append(parts, "README")
	}
	if input.Inventory.DocsFiles > 0 {
		score += 10
		parts = append(parts, fmt.Sprintf("%d docs file(s)", input.Inventory.DocsFiles))
	}
	if input.Inventory.ConfigFiles > 0 {
		score += 10
		parts = append(parts, fmt.Sprintf("%d config file(s)", input.Inventory.ConfigFiles))
	}
	if input.Inventory.SourceFiles > 0 && input.Inventory.DocsFiles == 0 {
		score -= 10
	}
	evidenceText := "Architecture legibility uses repo docs, README, and configuration shape."
	if len(parts) > 0 {
		evidenceText = "Detected " + strings.Join(parts, ", ") + "."
	}
	return component("architecture_legibility", "Architecture legibility", clamp(score), 12, signals.ConfidenceLow, evidenceText, "Document the main architecture boundaries agents should preserve.")
}

func attributionScore(attribution signals.AttributionReadiness) signals.ReadinessComponent {
	score := 35
	switch attribution.Confidence {
	case signals.ConfidenceHigh:
		score = 90
	case signals.ConfidenceMedium:
		score = 70
	}
	return component("attribution_readiness", "Attribution readiness", score, 14, attribution.Confidence, attribution.Summary, attribution.NextAction)
}

func safetyScore(input Input) signals.ReadinessComponent {
	available := 0
	for _, tool := range input.Tooling.Tools {
		if !tool.Required && tool.Available {
			available++
		}
	}
	if available > 0 {
		return component("safety_privacy_readiness", "Safety/privacy readiness", 75, 10, signals.ConfidenceMedium, fmt.Sprintf("%d optional safety tool(s) are available and reports avoid raw code, diffs, credentials, and author emails.", available), "Install remaining optional analyzers for broader safety evidence.")
	}
	return component("safety_privacy_readiness", "Safety/privacy readiness", 55, 10, signals.ConfidenceMedium, "Reports avoid raw code, diffs, credentials, and author emails, but optional safety analyzers are unavailable.", "Run contribution doctor and install optional safety tools.")
}

func component(id, label string, score, weight int, confidence signals.Confidence, evidence, next string) signals.ReadinessComponent {
	return signals.ReadinessComponent{
		ID:         id,
		Label:      label,
		Score:      clamp(score),
		Weight:     weight,
		Confidence: confidence,
		Evidence:   evidence,
		NextAction: next,
	}
}

func readinessActions(components []signals.ReadinessComponent) []string {
	items := append([]signals.ReadinessComponent{}, components...)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score < items[j].Score
		}
		return items[i].Weight > items[j].Weight
	})
	var out []string
	for _, item := range items {
		if item.NextAction == "" {
			continue
		}
		out = append(out, item.NextAction)
		if len(out) == 5 {
			break
		}
	}
	return out
}

func buildAttribution(input Input) (signals.AttributionReadiness, []signals.WorkUnitCandidate) {
	commitIssues := map[string]int{}
	for _, commit := range input.History.Commits {
		for _, issue := range issueRefs(commit.Subject) {
			commitIssues[issue]++
		}
	}
	prIssueCount := 0
	for _, pr := range input.GitHub.PRs {
		if len(issueRefs(pr.Title)) > 0 {
			prIssueCount++
		}
	}
	patterns := []signals.AnchorPattern{}
	if len(input.WorkUnitMarkers) > 0 {
		patterns = append(patterns, signals.AnchorPattern{ID: "manual_marker", Label: "Manual work-unit marker", Count: len(input.WorkUnitMarkers), Confidence: signals.ConfidenceHigh, Evidence: "Repo-local work-unit marker files are present."})
	}
	if len(input.GitHub.PRs) > 0 {
		confidence := signals.ConfidenceMedium
		if prIssueCount > 0 && prIssueCount*2 >= len(input.GitHub.PRs) {
			confidence = signals.ConfidenceHigh
		}
		patterns = append(patterns, signals.AnchorPattern{ID: "pr", Label: "PR anchor", Count: len(input.GitHub.PRs), Confidence: confidence, Evidence: fmt.Sprintf("%d of %d PR title(s) include issue references.", prIssueCount, len(input.GitHub.PRs))})
	}
	if len(commitIssues) > 0 {
		count := 0
		for _, value := range commitIssues {
			count += value
		}
		patterns = append(patterns, signals.AnchorPattern{ID: "issue_key", Label: "Issue key in commit text", Count: count, Confidence: signals.ConfidenceMedium, Evidence: fmt.Sprintf("%d recent commit subject(s) include issue-style references.", count)})
	}
	if len(input.History.Commits) > 0 {
		patterns = append(patterns, signals.AnchorPattern{ID: "commit_batch", Label: "Commit batch", Count: len(input.History.Commits), Confidence: signals.ConfidenceLow, Evidence: "Local commits can be grouped coarsely by time window when stronger anchors are missing."})
	}
	pattern, confidence, summary, next := choosePattern(input, prIssueCount, len(commitIssues))
	missing := missingAttributionEvidence(input, confidence)
	attribution := signals.AttributionReadiness{
		Pattern:         pattern,
		Confidence:      confidence,
		Summary:         summary,
		Evidence:        attributionEvidence(patterns),
		MissingEvidence: missing,
		NextAction:      next,
		AnchorPatterns:  patterns,
	}
	return attribution, workUnitCandidates(input, pattern, confidence, commitIssues)
}

func choosePattern(input Input, prIssueCount int, commitIssueKeys int) (string, signals.Confidence, string, string) {
	switch {
	case len(input.WorkUnitMarkers) > 0:
		return "manual_marker", signals.ConfidenceHigh, fmt.Sprintf("%d manual work-unit marker(s) provide explicit intent anchors.", len(input.WorkUnitMarkers)), "Keep using work-unit markers or connect issue tracker metadata for confirmation."
	case len(input.GitHub.PRs) > 0 && prIssueCount > 0 && prIssueCount*2 >= len(input.GitHub.PRs):
		return "issue-per-pr", signals.ConfidenceHigh, "PRs commonly include issue references, so work can be grouped with high confidence once issue metadata is connected.", "Connect the issue tracker so PR issue keys become durable work-unit anchors."
	case len(input.GitHub.PRs) > 0:
		return "pr-only", signals.ConfidenceMedium, "PRs are available, but issue intent is not consistently visible.", "Put the issue key in PR titles or connect issue tracker metadata."
	case commitIssueKeys > 0:
		return "commit-anchored", signals.ConfidenceMedium, "Commit subjects include issue references, but PR/review context is unavailable.", "Connect GitHub metadata or use work-unit markers for cleaner grouping."
	case len(input.History.Commits) > 0:
		return "commit-batch", signals.ConfidenceLow, "Only local commit batches are visible, so work-unit grouping is coarse.", "Connect GitHub and issue tracker metadata or run contribution work-unit start before agentic tasks."
	default:
		return "unknown", signals.ConfidenceLow, "No stable work-unit anchors were found in the available evidence.", "Connect GitHub and issue tracker metadata or create work-unit markers."
	}
}

func missingAttributionEvidence(input Input, confidence signals.Confidence) []string {
	var missing []string
	if !input.GitHub.Available || len(input.GitHub.PRs) == 0 {
		missing = append(missing, "PR metadata")
	}
	missing = append(missing, "issue tracker metadata")
	if len(input.AgentArtifacts) == 0 {
		missing = append(missing, "agent session telemetry")
	}
	if len(input.WorkUnitMarkers) == 0 && confidence == signals.ConfidenceLow {
		missing = append(missing, "manual work-unit markers")
	}
	return uniqueStrings(missing)
}

func attributionEvidence(patterns []signals.AnchorPattern) []string {
	out := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		if pattern.Evidence != "" {
			out = append(out, pattern.Evidence)
		}
	}
	if len(out) == 0 {
		out = append(out, "No work-unit anchors were visible.")
	}
	return out
}

func workUnitCandidates(input Input, pattern string, confidence signals.Confidence, commitIssues map[string]int) []signals.WorkUnitCandidate {
	var out []signals.WorkUnitCandidate
	for _, marker := range input.WorkUnitMarkers {
		anchors := []signals.WorkUnitAnchor{
			{Type: "manual_marker", ID: marker.ID, Label: marker.Goal, Confidence: signals.ConfidenceHigh},
		}
		evidence := []string{"Local marker contains explicit goal metadata."}
		if marker.Branch != "" {
			anchors = append(anchors, signals.WorkUnitAnchor{Type: "branch", ID: marker.Branch, Label: marker.Branch, Confidence: signals.ConfidenceMedium})
			evidence = append(evidence, "Marker records the branch visible when it was created.")
		}
		if marker.Commit != "" {
			anchors = append(anchors, signals.WorkUnitAnchor{Type: "commit", ID: marker.Commit, Label: gitrepo.ShortSHA(marker.Commit), Confidence: signals.ConfidenceMedium})
			evidence = append(evidence, "Marker records the commit visible when it was created.")
		}
		out = append(out, signals.WorkUnitCandidate{
			ID:         marker.ID,
			Title:      marker.Goal,
			Pattern:    "manual_marker",
			Confidence: signals.ConfidenceHigh,
			Summary:    "Manual marker created before or during agentic work.",
			Anchors:    anchors,
			Evidence:   evidence,
		})
	}
	for _, pr := range input.GitHub.PRs {
		id := fmt.Sprintf("pr-%d", pr.Number)
		anchors := []signals.WorkUnitAnchor{
			{Type: "pr", ID: fmt.Sprintf("%d", pr.Number), Label: fmt.Sprintf("PR #%d", pr.Number), Confidence: signals.ConfidenceHigh},
		}
		if pr.MergeCommitSHA != "" {
			anchors = append(anchors, signals.WorkUnitAnchor{Type: "commit", ID: pr.MergeCommitSHA, Label: gitrepo.ShortSHA(pr.MergeCommitSHA), Confidence: signals.ConfidenceMedium})
		}
		out = append(out, signals.WorkUnitCandidate{
			ID:         id,
			Title:      pr.Title,
			Pattern:    "pr",
			Confidence: confidenceForPRCandidate(pr),
			Summary:    fmt.Sprintf("PR #%d changed %d file(s) with %d review(s).", pr.Number, pr.ChangedFiles, pr.ReviewCount),
			Anchors:    anchors,
			Evidence:   issueRefs(pr.Title),
		})
		if len(out) >= 12 {
			return out
		}
	}
	issues := sortedIssueCounts(commitIssues)
	for _, issue := range issues {
		out = append(out, signals.WorkUnitCandidate{
			ID:         "issue-" + strings.ToLower(strings.TrimPrefix(issue.key, "#")),
			Title:      issue.key,
			Pattern:    "issue_key",
			Confidence: signals.ConfidenceMedium,
			Summary:    fmt.Sprintf("%d recent commit subject(s) reference %s.", issue.count, issue.key),
			Anchors: []signals.WorkUnitAnchor{
				{Type: "issue", ID: issue.key, Label: issue.key, Confidence: signals.ConfidenceMedium},
			},
		})
		if len(out) >= 12 {
			return out
		}
	}
	if len(out) == 0 && len(input.History.Commits) > 0 {
		out = append(out, signals.WorkUnitCandidate{
			ID:         "local-commit-window",
			Title:      "Recent local commit window",
			Pattern:    pattern,
			Confidence: confidence,
			Summary:    fmt.Sprintf("%d recent commit artifact(s) grouped by the analysis window.", len(input.History.Commits)),
			Anchors: []signals.WorkUnitAnchor{
				{Type: "time_window", Label: "analysis window", Confidence: signals.ConfidenceLow},
			},
			Limitations: []string{"This is useful for repo readiness, but too coarse for precise AI spend ROI."},
		})
	}
	return out
}

func confidenceForPRCandidate(pr github.PullRequest) signals.Confidence {
	if len(issueRefs(pr.Title)) > 0 && pr.ReviewCount+pr.CheckRuns > 0 {
		return signals.ConfidenceHigh
	}
	if pr.Number > 0 {
		return signals.ConfidenceMedium
	}
	return signals.ConfidenceLow
}

type issueCount struct {
	key   string
	count int
}

func sortedIssueCounts(values map[string]int) []issueCount {
	out := make([]issueCount, 0, len(values))
	for key, count := range values {
		out = append(out, issueCount{key: key, count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		return out[i].key < out[j].key
	})
	return out
}

func issueRefs(value string) []string {
	seen := map[string]bool{}
	var out []string
	for _, match := range linearIssuePattern.FindAllString(value, -1) {
		if !seen[match] {
			seen[match] = true
			out = append(out, match)
		}
	}
	for _, match := range ghIssuePattern.FindAllString(value, -1) {
		match = strings.TrimSpace(match)
		match = strings.TrimPrefix(match, "(")
		if !seen[match] {
			seen[match] = true
			out = append(out, match)
		}
	}
	return out
}

func detectInstructionFiles(repoPath string) []string {
	candidates := []string{
		"AGENTS.md",
		"CLAUDE.md",
		".github/copilot-instructions.md",
		".cursor/rules",
		"docs/agent-system.md",
	}
	var out []string
	for _, candidate := range candidates {
		if hasPath(repoPath, candidate) {
			out = append(out, candidate)
		}
	}
	return out
}

func detectValidation(repoPath string, cfg config.Config) validationEvidence {
	var commands []string
	if strings.TrimSpace(cfg.Coverage.Command) != "" {
		commands = append(commands, cfg.Coverage.Command)
	}
	if hasPath(repoPath, "go.mod") {
		commands = append(commands, "go test ./...")
	}
	if scripts := packageScripts(repoPath); len(scripts) > 0 {
		for _, name := range []string{"checks:changed", "test", "lint", "typecheck"} {
			if _, ok := scripts[name]; ok {
				commands = append(commands, "pnpm "+name)
			}
		}
	}
	if hasPath(repoPath, "Makefile") {
		commands = append(commands, "make test")
	}
	commands = uniqueStrings(commands)
	if len(commands) == 0 {
		return validationEvidence{reason: "No local validation command was detected in .contribution.yml, go.mod, package.json, or Makefile."}
	}
	return validationEvidence{commands: commands}
}

func packageScripts(repoPath string) map[string]string {
	path := filepath.Join(repoPath, "package.json")
	// #nosec G304 -- repo-local package manifest is safe to inspect for script keys.
	data, err := os.ReadFile(path)
	if err != nil || len(data) > maxArtifactBytes {
		return nil
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}
	return pkg.Scripts
}

func hasAnyPath(root string, paths ...string) bool {
	for _, path := range paths {
		if hasPath(root, path) {
			return true
		}
	}
	return false
}

func hasPath(root string, relative string) bool {
	if root == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative)))
	return err == nil && (info.IsDir() || info.Mode().IsRegular())
}

func inspectAgentArtifact(path string, repoPath string) signals.AgentArtifactMetadata {
	path = strings.TrimSpace(path)
	if path == "" {
		return artifactStatus(path, "unsupported", "empty artifact path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return artifactStatus(path, "unsupported", "artifact path could not be read")
	}
	if info.IsDir() {
		return artifactStatus(path, "unsupported", "artifact path is a directory; pass a metadata JSON file")
	}
	if info.Size() > maxArtifactBytes {
		return artifactStatus(path, "unsupported", "artifact exceeds the metadata-only size limit")
	}
	if strings.ToLower(filepath.Ext(path)) != ".json" {
		return artifactStatus(path, "unsupported", "only metadata JSON artifacts are supported")
	}
	// #nosec G304 -- user explicitly supplied the metadata artifact path.
	data, err := os.ReadFile(path)
	if err != nil {
		return artifactStatus(path, "unsupported", "artifact could not be read")
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return artifactStatus(path, "unsupported", "artifact is not valid JSON metadata")
	}
	source := firstString(raw, "provider", "tool", "agent", "source", "model_provider")
	session := firstString(raw, "session_id", "sessionId", "conversation_id", "trace_id", "run_id")
	tokenCount := int(firstNumber(raw, "total_tokens", "tokens", "token_count"))
	if tokenCount == 0 {
		tokenCount = int(firstNumber(raw, "input_tokens", "prompt_tokens")) + int(firstNumber(raw, "output_tokens", "completion_tokens"))
	}
	cost := firstNumber(raw, "cost_usd", "total_cost_usd", "amount_usd")
	branch := firstString(raw, "branch", "git_branch")
	commit := firstString(raw, "commit", "commit_sha", "head_sha")
	repoMatched := repoPath != "" && jsonContainsString(raw, repoPath)
	if source == "" && session == "" && tokenCount == 0 && cost == 0 && branch == "" && commit == "" && !repoMatched {
		return artifactStatus(path, "unsupported", "no supported metadata fields were found")
	}
	return signals.AgentArtifactMetadata{
		Path:               path,
		Source:             source,
		Status:             "available",
		SessionFingerprint: fingerprint(session),
		RepoMatched:        repoMatched,
		Branch:             branch,
		Commit:             commit,
		TokenCount:         tokenCount,
		CostUSD:            cost,
		Confidence:         signals.ConfidenceMedium,
	}
}

func artifactStatus(path, status, reason string) signals.AgentArtifactMetadata {
	return signals.AgentArtifactMetadata{
		Path:       path,
		Status:     status,
		Reason:     reason,
		Confidence: signals.ConfidenceLow,
	}
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := findValue(raw, key); value != nil {
			if text, ok := value.(string); ok {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}

func firstNumber(raw map[string]any, keys ...string) float64 {
	for _, key := range keys {
		if value := findValue(raw, key); value != nil {
			switch typed := value.(type) {
			case float64:
				return typed
			case int:
				return float64(typed)
			case json.Number:
				parsed, _ := typed.Float64()
				return parsed
			}
		}
	}
	return 0
}

func findValue(value any, key string) any {
	switch typed := value.(type) {
	case map[string]any:
		for k, v := range typed {
			if strings.EqualFold(k, key) {
				return v
			}
		}
		for k, v := range typed {
			if sensitiveArtifactKey(k) {
				continue
			}
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

func jsonContainsString(value any, needle string) bool {
	if needle == "" {
		return false
	}
	switch typed := value.(type) {
	case map[string]any:
		for k, v := range typed {
			if sensitiveArtifactKey(k) {
				continue
			}
			if jsonContainsString(v, needle) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if jsonContainsString(item, needle) {
				return true
			}
		}
	case string:
		return strings.Contains(typed, needle)
	}
	return false
}

func sensitiveArtifactKey(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "prompt") ||
		strings.Contains(key, "completion") ||
		strings.Contains(key, "message") ||
		strings.Contains(key, "transcript") ||
		strings.Contains(key, "content") ||
		strings.Contains(key, "text")
}

func fingerprint(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}

func grade(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 80:
		return "B"
	case score >= 70:
		return "C"
	case score >= 60:
		return "D"
	default:
		return "F"
	}
}

func clamp(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
