// Package publicsafe transforms private analysis data into public-safe artifacts.
package publicsafe

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/contribution-dev/contribution/internal/privacy"
	"github.com/contribution-dev/contribution/internal/signals"
)

type pathReplacement struct {
	private string
	public  string
}

var (
	commitSHAPattern     = regexp.MustCompile(`\b[0-9a-fA-F]{7,40}\b`)
	pathCandidatePattern = regexp.MustCompile(`(?:[A-Za-z]:)?(?:[./~]?[\w.-]+[/\\])+[\w.@+-]+`)
)

const publicRepoID = "private-repository"

// Analysis returns an analysis report suitable for public-safe output.
func Analysis(analysis signals.AnalysisReport) signals.AnalysisReport {
	sourceRepoID := analysis.Repo.ID
	pathReplacements := pathReplacementsForAnalysis(analysis)
	analysis.Repo = Repo(analysis.Repo)
	analysis.Config.PublicSafe = true
	analysis.Config.OutputDirectory = ""
	analysis.Config.GitHubMetadataConfigured = false
	analysis.Config.SelfReportedAITools = nil
	analysis.Config.SelfReportedAIModes = nil
	analysis.Privacy.PublicSafe = true
	analysis.Privacy.RawCodeIncluded = false
	analysis.Privacy.RawDiffsIncluded = false
	analysis.Privacy.PrivatePathsIncludedInPublicExport = false
	analysis.Privacy.AuthorEmailsIncluded = false
	analysis.PRCards = cards(analysis.PRCards, len(analysis.PRCards), pathReplacements)
	analysis.WeaknessMap = weaknessMap(analysis.WeaknessMap, pathReplacements)
	analysis.Trends = trends(analysis.Trends, pathReplacements)
	analysis.Coverage = coverage(analysis.Coverage, pathReplacements)
	analysis.AnalyzerFindings = analyzerFindings(analysis.AnalyzerFindings, pathReplacements)
	analysis.DeepDives = deepDives(analysis.DeepDives, pathReplacements)
	analysis.Profile.Strengths = findings(analysis.Profile.Strengths, len(analysis.Profile.Strengths), pathReplacements)
	analysis.Profile.ImprovementTrends = findings(analysis.Profile.ImprovementTrends, len(analysis.Profile.ImprovementTrends), pathReplacements)
	analysis.Profile.Headline = redactCommitLikeText(redactText(analysis.Profile.Headline, pathReplacements))
	analysis.Profile.DisplayName = redactCommitLikeText(redactText(analysis.Profile.DisplayName, pathReplacements))
	analysis.SetupActions = setupActions(analysis.SetupActions, pathReplacements)
	analysis.Limitations = redactStrings(analysis.Limitations, pathReplacements)
	for i := range analysis.Signals {
		analysis.Signals[i] = signal(analysis.Signals[i], sourceRepoID, pathReplacements)
	}
	for i := range analysis.Tooling.Tools {
		analysis.Tooling.Tools[i].Version = redactText(analysis.Tooling.Tools[i].Version, pathReplacements)
		analysis.Tooling.Tools[i].Reason = redactText(analysis.Tooling.Tools[i].Reason, pathReplacements)
	}
	analysis.Tooling.Limitations = redactStrings(analysis.Tooling.Limitations, pathReplacements)
	return analysis
}

// Repo removes private repository identity from metadata.
func Repo(repo signals.RepoMetadata) signals.RepoMetadata {
	repo.ID = publicRepoID
	repo.Name = "private repository"
	repo.Root = ""
	repo.RemoteURL = ""
	repo.HeadSHA = ""
	repo.GitHubOwner = ""
	repo.GitHubRepo = ""
	return repo
}

// Card returns a single neutral, public-safe card.
func Card(card signals.PRQualityCard, ordinal int) signals.PRQualityCard {
	return cardWithReplacements(card, ordinal)
}

func findings(findings []signals.Finding, limit int, replacements ...[]pathReplacement) []signals.Finding {
	if len(findings) < limit {
		limit = len(findings)
	}
	out := make([]signals.Finding, 0, limit)
	for i := 0; i < limit; i++ {
		f := findings[i]
		f.Label = redactCommitLikeText(redactText(f.Label, replacements...))
		f.Evidence = redactCommitLikeText(redactText(f.Evidence, replacements...))
		f.NextAction = ""
		f.WhyItMatters = ""
		out = append(out, f)
	}
	return out
}

func cards(cards []signals.PRQualityCard, limit int, replacements ...[]pathReplacement) []signals.PRQualityCard {
	if len(cards) < limit {
		limit = len(cards)
	}
	out := make([]signals.PRQualityCard, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, cardWithReplacements(cards[i], i+1, replacements...))
	}
	return out
}

func cardWithReplacements(card signals.PRQualityCard, ordinal int, replacements ...[]pathReplacement) signals.PRQualityCard {
	card.Title = artifactTitle(card, ordinal)
	card.URL = ""
	card.Summary = redactCommitLikeText(redactText(card.Summary, replacements...))
	card.Scope = redactText(card.Scope, replacements...)
	card.TestEvidence = redactText(card.TestEvidence, replacements...)
	card.ReviewBurden = redactText(card.ReviewBurden, replacements...)
	card.Durability = redactText(card.Durability, replacements...)
	card.MainRisk = ""
	card.Strengths = nil
	card.Risks = nil
	card.Evidence = nil
	card.NextAction = ""
	return card
}

func artifactTitle(card signals.PRQualityCard, ordinal int) string {
	if card.PRNumber > 0 {
		return fmt.Sprintf("PR #%d", card.PRNumber)
	}
	if ordinal <= 0 {
		ordinal = 1
	}
	return fmt.Sprintf("Artifact %d", ordinal)
}

func weaknessMap(value signals.WeaknessMap, replacements ...[]pathReplacement) signals.WeaknessMap {
	value.Strengths = redactFindings(value.Strengths, replacements...)
	value.Weaknesses = redactFindings(value.Weaknesses, replacements...)
	value.WatchItems = redactFindings(value.WatchItems, replacements...)
	value.NextActions = redactStrings(value.NextActions, replacements...)
	return value
}

func trends(value signals.TrendComparison, replacements ...[]pathReplacement) signals.TrendComparison {
	value.Findings = redactFindings(value.Findings, replacements...)
	value.Reason = redactCommitLikeText(redactText(value.Reason, replacements...))
	for i := range value.Metrics {
		value.Metrics[i].Label = redactCommitLikeText(redactText(value.Metrics[i].Label, replacements...))
		value.Metrics[i].Evidence = redactCommitLikeText(redactText(value.Metrics[i].Evidence, replacements...))
		value.Metrics[i].WhyItMatters = redactCommitLikeText(redactText(value.Metrics[i].WhyItMatters, replacements...))
		value.Metrics[i].NextAction = redactCommitLikeText(redactText(value.Metrics[i].NextAction, replacements...))
	}
	return value
}

func coverage(value signals.CoverageSummary, replacements ...[]pathReplacement) signals.CoverageSummary {
	for i := range value.Files {
		value.Files[i].Path = privacy.RedactPath(redactText(value.Files[i].Path, replacements...), false)
	}
	for i := range value.LowCoverageFiles {
		value.LowCoverageFiles[i].Path = privacy.RedactPath(redactText(value.LowCoverageFiles[i].Path, replacements...), false)
	}
	value.Sources = redactStrings(value.Sources, replacements...)
	value.Reason = redactText(value.Reason, replacements...)
	return value
}

func analyzerFindings(findings []signals.AnalyzerFinding, replacements ...[]pathReplacement) []signals.AnalyzerFinding {
	out := make([]signals.AnalyzerFinding, 0, len(findings))
	for _, finding := range findings {
		finding.FilePath = privacy.RedactPath(redactText(finding.FilePath, replacements...), false)
		finding.Message = "Private analyzer finding redacted."
		finding.RuleID = redactCommitLikeText(redactText(finding.RuleID, replacements...))
		finding.PublicSafe = true
		out = append(out, finding)
	}
	return out
}

func deepDives(value signals.AnalysisDeepDives, replacements ...[]pathReplacement) signals.AnalysisDeepDives {
	ordinal := 1
	for i := range value.HighChurn {
		value.HighChurn[i].Path = privacy.RedactPath(redactText(value.HighChurn[i].Path, replacements...), false)
		value.HighChurn[i].NextAction = redactCommitLikeText(redactText(value.HighChurn[i].NextAction, replacements...))
		for j := range value.HighChurn[i].Artifacts {
			value.HighChurn[i].Artifacts[j] = deepDiveArtifact(value.HighChurn[i].Artifacts[j], &ordinal, replacements...)
		}
	}
	for i := range value.NoTestArtifacts {
		value.NoTestArtifacts[i].Artifact = deepDiveArtifact(value.NoTestArtifacts[i].Artifact, &ordinal, replacements...)
		value.NoTestArtifacts[i].Risk = redactCommitLikeText(redactText(value.NoTestArtifacts[i].Risk, replacements...))
		value.NoTestArtifacts[i].NextAction = redactCommitLikeText(redactText(value.NoTestArtifacts[i].NextAction, replacements...))
		for j := range value.NoTestArtifacts[i].ChangedSourceFiles {
			value.NoTestArtifacts[i].ChangedSourceFiles[j] = privacy.RedactPath(redactText(value.NoTestArtifacts[i].ChangedSourceFiles[j], replacements...), false)
		}
	}
	return value
}

func deepDiveArtifact(value signals.DeepDiveArtifact, ordinal *int, replacements ...[]pathReplacement) signals.DeepDiveArtifact {
	label := value.Label
	if !strings.HasPrefix(label, "PR #") {
		label = fmt.Sprintf("Artifact %d", *ordinal)
		(*ordinal)++
	}
	value.ID = ""
	value.Label = label
	value.Title = ""
	value.Scope = redactText(value.Scope, replacements...)
	value.TestEvidence = redactText(value.TestEvidence, replacements...)
	value.MainRisk = ""
	value.NextAction = redactCommitLikeText(redactText(value.NextAction, replacements...))
	return value
}

func setupActions(actions []signals.SetupAction, replacements ...[]pathReplacement) []signals.SetupAction {
	out := make([]signals.SetupAction, 0, len(actions))
	for _, action := range actions {
		action.Label = redactCommitLikeText(redactText(action.Label, replacements...))
		action.Command = redactCommitLikeText(redactText(action.Command, replacements...))
		action.Why = redactCommitLikeText(redactText(action.Why, replacements...))
		out = append(out, action)
	}
	return out
}

func signal(sig signals.Signal, sourceRepoID string, replacements ...[]pathReplacement) signals.Signal {
	privateSubjectID := sig.SubjectID
	if sig.RepoID == sourceRepoID {
		sig.RepoID = publicRepoID
	}
	if sig.SubjectID == sourceRepoID {
		sig.SubjectID = publicRepoID
	}
	if sig.SubjectType == "commit" {
		sig.SubjectID = ""
	}
	if sig.SubjectType == "file" && sig.SubjectID != "" {
		sig.SubjectID = privacy.RedactPath(sig.SubjectID, false)
	}
	if sig.FilePath != "" {
		sig.FilePath = privacy.RedactPath(sig.FilePath, false)
	}
	if !sig.PublicSafe && sig.Type == "analyzer_finding" {
		sig.Message = "Private analyzer finding redacted."
	} else {
		sig.Message = redactCommitLikeText(redactText(sig.Message, replacements...), privateSubjectID)
	}
	sig.Evidence.ToolVersion = redactText(sig.Evidence.ToolVersion, replacements...)
	sig.PublicSafe = true
	return sig
}

func redactFindings(findings []signals.Finding, replacements ...[]pathReplacement) []signals.Finding {
	out := make([]signals.Finding, 0, len(findings))
	for _, finding := range findings {
		finding.Label = redactCommitLikeText(redactText(finding.Label, replacements...))
		finding.Evidence = redactCommitLikeText(redactText(finding.Evidence, replacements...))
		finding.WhyItMatters = redactCommitLikeText(redactText(finding.WhyItMatters, replacements...))
		finding.NextAction = redactCommitLikeText(redactText(finding.NextAction, replacements...))
		out = append(out, finding)
	}
	return out
}

func redactStrings(values []string, replacements ...[]pathReplacement) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, redactCommitLikeText(redactText(value, replacements...)))
	}
	return out
}

func redactText(value string, replacements ...[]pathReplacement) string {
	if len(replacements) > 0 {
		for _, replacement := range replacements[0] {
			value = strings.ReplaceAll(value, replacement.private, replacement.public)
		}
	}
	return privacy.RedactSecretLikeText(value)
}

func redactCommitLikeText(value string, shas ...string) string {
	for _, sha := range shas {
		sha = strings.TrimSpace(sha)
		if sha == "" {
			continue
		}
		value = strings.ReplaceAll(value, sha, "commit")
		if len(sha) > 8 {
			value = strings.ReplaceAll(value, sha[:8], "commit")
		}
	}
	return commitSHAPattern.ReplaceAllString(value, "commit")
}

func pathReplacementsForAnalysis(analysis signals.AnalysisReport) []pathReplacement {
	var replacements []pathReplacement
	seen := map[string]bool{}
	add := func(values ...string) {
		for _, value := range values {
			addPathReplacementsFromText(&replacements, seen, value)
		}
	}
	add(analysis.Repo.ID, analysis.Repo.Name, analysis.Repo.Root, analysis.Repo.RemoteURL, analysis.Repo.HeadSHA, analysis.Repo.GitHubOwner, analysis.Repo.GitHubRepo)
	add(analysis.Config.OutputDirectory)
	for _, tool := range analysis.Tooling.Tools {
		add(tool.Name, tool.Version, tool.Reason)
	}
	add(analysis.Tooling.Limitations...)
	for _, sig := range analysis.Signals {
		add(sig.RepoID, sig.SubjectID, sig.FilePath, sig.Message, sig.Evidence.ToolVersion)
	}
	for _, file := range analysis.Coverage.Files {
		add(file.Path)
	}
	for _, file := range analysis.Coverage.LowCoverageFiles {
		add(file.Path)
	}
	add(analysis.Coverage.Sources...)
	add(analysis.Coverage.Reason)
	for _, finding := range analysis.AnalyzerFindings {
		add(finding.RuleID, finding.FilePath, finding.Message)
	}
	for _, card := range analysis.PRCards {
		add(card.Title, card.URL, card.Summary, card.Scope, card.TestEvidence, card.ReviewBurden, card.Durability, card.MainRisk, card.NextAction)
		for _, finding := range append(append([]signals.Finding{}, card.Strengths...), card.Risks...) {
			addFindingText(add, finding)
		}
		for _, evidence := range card.Evidence {
			add(evidence.ID, evidence.Message)
		}
	}
	for _, finding := range analysis.WeaknessMap.Strengths {
		addFindingText(add, finding)
	}
	for _, finding := range analysis.WeaknessMap.Weaknesses {
		addFindingText(add, finding)
	}
	for _, finding := range analysis.WeaknessMap.WatchItems {
		addFindingText(add, finding)
	}
	add(analysis.WeaknessMap.NextActions...)
	add(analysis.Trends.Status, analysis.Trends.Reason)
	addTrendWindowText(add, analysis.Trends.CurrentWindow)
	addTrendWindowText(add, analysis.Trends.PriorWindow)
	for _, metric := range analysis.Trends.Metrics {
		add(metric.ID, metric.Label, metric.Direction, metric.Evidence, metric.WhyItMatters, metric.NextAction)
	}
	for _, finding := range analysis.Trends.Findings {
		addFindingText(add, finding)
	}
	add(analysis.Profile.DisplayName, analysis.Profile.Headline)
	for _, finding := range analysis.Profile.Strengths {
		addFindingText(add, finding)
	}
	for _, finding := range analysis.Profile.ImprovementTrends {
		addFindingText(add, finding)
	}
	for _, badge := range analysis.Profile.BadgeCandidates {
		add(badge.ID, badge.Label)
	}
	for _, dive := range analysis.DeepDives.HighChurn {
		add(dive.Path, dive.NextAction)
		for _, artifact := range dive.Artifacts {
			addDeepDiveArtifactText(add, artifact)
		}
	}
	for _, dive := range analysis.DeepDives.NoTestArtifacts {
		addDeepDiveArtifactText(add, dive.Artifact)
		add(dive.ChangedSourceFiles...)
		add(dive.Risk, dive.NextAction)
	}
	for _, action := range analysis.SetupActions {
		add(action.ID, action.Label, action.Command, action.Why, action.ConfidenceImpact)
	}
	add(analysis.Limitations...)
	sort.Slice(replacements, func(i, j int) bool {
		return len(replacements[i].private) > len(replacements[j].private)
	})
	return replacements
}

func addFindingText(add func(...string), finding signals.Finding) {
	add(finding.Label, finding.Evidence, finding.WhyItMatters, finding.NextAction)
}

func addDeepDiveArtifactText(add func(...string), artifact signals.DeepDiveArtifact) {
	add(artifact.ID, artifact.Label, artifact.Title, artifact.Scope, artifact.TestEvidence, artifact.MainRisk, artifact.NextAction)
}

func addTrendWindowText(add func(...string), window signals.TrendWindow) {
	add(window.Label)
}

func addPathReplacementsFromText(replacements *[]pathReplacement, seen map[string]bool, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if isPathCandidate(value) {
		addPathReplacement(replacements, seen, value)
	}
	for _, candidate := range pathCandidatePattern.FindAllString(value, -1) {
		candidate = strings.Trim(candidate, ".,;:()[]{}<>\"'`")
		if isPathCandidate(candidate) {
			addPathReplacement(replacements, seen, candidate)
		}
	}
}

func isPathCandidate(value string) bool {
	return strings.Contains(value, "/") || strings.Contains(value, "\\")
}

func addPathReplacement(replacements *[]pathReplacement, seen map[string]bool, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	public := privacy.RedactPath(value, false)
	add := func(private string) {
		if private == "" || private == public || seen[private] {
			return
		}
		*replacements = append(*replacements, pathReplacement{private: private, public: public})
		seen[private] = true
	}
	add(value)
	add(filepath.ToSlash(value))
}
