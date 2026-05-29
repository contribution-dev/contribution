// Package scoring turns normalized evidence into artifact labels and coaching.
package scoring

import (
	"fmt"
	"strings"

	gitrepo "github.com/contribution-dev/contribution/internal/git"
	"github.com/contribution-dev/contribution/internal/github"
	"github.com/contribution-dev/contribution/internal/signals"
)

// Input is the deterministic evidence available to V1 scoring.
type Input struct {
	Repo        signals.RepoMetadata
	History     gitrepo.History
	GitHub      github.Metadata
	Inventory   signals.FileSummary
	Signals     []signals.Signal
	SinceDays   int
	MaxCards    int
	DisplayName string
	AITools     []string
	AIModes     []string
}

// Output is the labeled report state.
type Output struct {
	Cards       []signals.PRQualityCard
	WeaknessMap signals.WeaknessMap
	Profile     signals.ProfileSummary
	Limitations []string
}

// Build creates cards, weakness map, and profile summary.
func Build(input Input) Output {
	cards := buildCards(input)
	weaknessMap := buildWeaknessMap(input, cards)
	profile := buildProfile(input, weaknessMap, len(cards))
	return Output{Cards: cards, WeaknessMap: weaknessMap, Profile: profile}
}

func buildCards(input Input) []signals.PRQualityCard {
	if input.MaxCards <= 0 {
		input.MaxCards = 20
	}
	if input.GitHub.Available && len(input.GitHub.PRs) > 0 {
		cards := make([]signals.PRQualityCard, 0, min(len(input.GitHub.PRs), input.MaxCards))
		for _, pr := range input.GitHub.PRs {
			cards = append(cards, cardFromPR(pr))
			if len(cards) >= input.MaxCards {
				break
			}
		}
		return cards
	}
	cards := make([]signals.PRQualityCard, 0, min(len(input.History.Commits), input.MaxCards))
	for _, commit := range input.History.Commits {
		cards = append(cards, cardFromCommit(commit))
		if len(cards) >= input.MaxCards {
			break
		}
	}
	return cards
}

func cardFromPR(pr github.PullRequest) signals.PRQualityCard {
	totalLines := pr.Additions + pr.Deletions
	label := "mixed"
	confidence := signals.ConfidenceMedium
	mainRisk := "Test and review details are limited to currently imported GitHub metadata."
	nextAction := "Use preflight on the current diff to add file-level test and risk evidence before review."
	var strengths []signals.Finding
	var risks []signals.Finding
	if pr.ChangedFiles <= 5 && totalLines <= 300 {
		label = "strong"
		strengths = append(strengths, signals.Finding{
			Label:      "Focused scope",
			Evidence:   fmt.Sprintf("PR #%d changed %d files and %d lines.", pr.Number, pr.ChangedFiles, totalLines),
			Confidence: signals.ConfidenceMedium,
		})
		mainRisk = "No major scope risk was visible from imported GitHub metadata."
		nextAction = "Repeat this focused PR shape and keep tests adjacent to behavior changes."
	}
	if pr.ChangedFiles > 15 || totalLines > 800 {
		label = "risky"
		risks = append(risks, signals.Finding{
			Label:        "Large review surface",
			Evidence:     fmt.Sprintf("PR #%d changed %d files and %d lines.", pr.Number, pr.ChangedFiles, totalLines),
			Confidence:   signals.ConfidenceMedium,
			WhyItMatters: "Large PRs are harder to review and easier to churn after merge.",
			NextAction:   "Split broad changes into smaller behavior-preserving PRs.",
		})
		mainRisk = "Large review surface."
		nextAction = "Split the next comparable change before opening review."
	}
	return signals.PRQualityCard{
		PRNumber:     pr.Number,
		Title:        pr.Title,
		URL:          pr.URL,
		Label:        label,
		Confidence:   confidence,
		Summary:      fmt.Sprintf("Merged PR touching %d files with %d additions and %d deletions.", pr.ChangedFiles, pr.Additions, pr.Deletions),
		Scope:        scopeDescription(pr.ChangedFiles, totalLines),
		TestEvidence: "Unavailable from imported PR list metadata.",
		ReviewBurden: "GitHub PR metadata available; detailed review comments were not imported for this analysis.",
		Durability:   "Post-merge churn requires file-level PR data and is not available for this card.",
		MainRisk:     mainRisk,
		Strengths:    strengths,
		Risks:        risks,
		Evidence: []signals.SignalRef{{
			ID:      fmt.Sprintf("github-pr-%d", pr.Number),
			Message: fmt.Sprintf("GitHub merged PR metadata for #%d.", pr.Number),
		}},
		NextAction: nextAction,
	}
}

func cardFromCommit(commit gitrepo.Commit) signals.PRQualityCard {
	fileCount := len(commit.Files)
	lineCount := gitrepo.TotalChangedLines(commit.Files)
	label := "mixed"
	confidence := signals.ConfidenceMedium
	mainRisk := "Source changes had limited test evidence."
	nextAction := "For behavior-changing commits, add an adjacent test before opening review."
	var strengths []signals.Finding
	var risks []signals.Finding
	if fileCount == 0 {
		label = "insufficient_data"
		confidence = signals.ConfidenceLow
		mainRisk = "No changed files were visible for this commit."
		nextAction = "Run analyze on a range with richer history."
	} else if fileCount <= 5 && (!commit.SourceTouched || commit.TestsTouched) && !commit.RiskyTouched {
		label = "strong"
		mainRisk = "No major local-history risk was visible."
		nextAction = "Repeat this small, reviewable change shape."
		strengths = append(strengths, signals.Finding{
			Label:      "Focused change",
			Evidence:   fmt.Sprintf("Commit %s changed %s.", gitrepo.ShortSHA(commit.SHA), scopeDescription(fileCount, lineCount)),
			Confidence: signals.ConfidenceHigh,
		})
	}
	if commit.SourceTouched && !commit.TestsTouched {
		risks = append(risks, signals.Finding{
			Label:        "No adjacent tests",
			Evidence:     fmt.Sprintf("Commit %s changed source files without test files.", gitrepo.ShortSHA(commit.SHA)),
			Confidence:   signals.ConfidenceMedium,
			WhyItMatters: "Reviewers and future changes have less protection when behavior changes lack test evidence.",
			NextAction:   "Add at least one nearby test for behavior changes.",
		})
	}
	if fileCount > 12 || lineCount > 800 {
		label = "risky"
		risks = append(risks, signals.Finding{
			Label:        "Large scope",
			Evidence:     fmt.Sprintf("Commit %s changed %s.", gitrepo.ShortSHA(commit.SHA), scopeDescription(fileCount, lineCount)),
			Confidence:   signals.ConfidenceHigh,
			WhyItMatters: "Broad changes are harder to review and easier to churn.",
			NextAction:   "Split broad work into smaller commits or PRs.",
		})
		mainRisk = "Large review surface."
		nextAction = "Split the next broad change before review."
	}
	if commit.RiskyTouched && !commit.TestsTouched {
		label = "risky"
		mainRisk = "Security-sensitive paths changed without adjacent tests."
		nextAction = "Add focused tests around auth, billing, session, or permission edge cases."
	}
	if commit.TestsTouched {
		strengths = append(strengths, signals.Finding{
			Label:      "Test evidence",
			Evidence:   fmt.Sprintf("Commit %s touched test files.", gitrepo.ShortSHA(commit.SHA)),
			Confidence: signals.ConfidenceHigh,
		})
		if label != "risky" {
			mainRisk = "No major test evidence gap was visible from local history."
			nextAction = "Keep pairing behavior changes with adjacent tests."
		}
	}
	if len(risks) == 0 && label == "mixed" {
		mainRisk = "Local commit grouping has less context than PR metadata."
		nextAction = "Run with GitHub metadata for review burden and PR-level confidence."
	}
	return signals.PRQualityCard{
		Title:        commitTitle(commit),
		Label:        label,
		Confidence:   confidence,
		Summary:      fmt.Sprintf("Commit-group card based on %s.", scopeDescription(fileCount, lineCount)),
		Scope:        scopeDescription(fileCount, lineCount),
		TestEvidence: testEvidenceLabel(commit),
		ReviewBurden: "Unavailable without GitHub metadata.",
		Durability:   durabilityLabel(commit),
		MainRisk:     mainRisk,
		Strengths:    strengths,
		Risks:        risks,
		Evidence: []signals.SignalRef{{
			ID:      commit.SHA,
			Message: fmt.Sprintf("Local git commit %s.", gitrepo.ShortSHA(commit.SHA)),
		}},
		NextAction: nextAction,
	}
}

func buildWeaknessMap(input Input, _ []signals.PRQualityCard) signals.WeaknessMap {
	var focused, testTouched, sourceNoTest, large, riskyNoTest, docsTouched, fixLike int
	for _, commit := range input.History.Commits {
		lineCount := gitrepo.TotalChangedLines(commit.Files)
		if len(commit.Files) <= 5 && len(commit.Files) > 0 {
			focused++
		}
		if commit.TestsTouched {
			testTouched++
		}
		if commit.SourceTouched && !commit.TestsTouched {
			sourceNoTest++
		}
		if len(commit.Files) > 12 || lineCount > 800 {
			large++
		}
		if commit.RiskyTouched && !commit.TestsTouched {
			riskyNoTest++
		}
		if commit.DocsTouched {
			docsTouched++
		}
		if commit.IsFollowUpFix || commit.IsRevert {
			fixLike++
		}
	}

	confidence := signals.ConfidenceMedium
	if input.GitHub.Available && len(input.History.Commits) >= 10 {
		confidence = signals.ConfidenceHigh
	}
	if len(input.History.Commits) < 3 {
		confidence = signals.ConfidenceLow
	}

	var strengths []signals.Finding
	if focused > 0 {
		strengths = append(strengths, signals.Finding{
			Label:        "Focused local changes",
			Evidence:     fmt.Sprintf("%d recent commits changed five or fewer files.", focused),
			Confidence:   confidence,
			WhyItMatters: "Smaller changes are easier to review and easier to make durable.",
		})
	}
	if testTouched > 0 {
		strengths = append(strengths, signals.Finding{
			Label:        "Some adjacent test evidence",
			Evidence:     fmt.Sprintf("%d recent commits touched test files.", testTouched),
			Confidence:   confidence,
			WhyItMatters: "Adjacent tests give reviewers and future changes better protection.",
		})
	}
	if docsTouched > 0 {
		strengths = append(strengths, signals.Finding{
			Label:        "Documentation support appears in recent work",
			Evidence:     fmt.Sprintf("%d recent commits touched docs.", docsTouched),
			Confidence:   confidence,
			WhyItMatters: "Docs changes help make contribution value durable beyond the diff.",
		})
	}
	if len(strengths) == 0 {
		strengths = append(strengths, signals.Finding{
			Label:        "Local evidence collected",
			Evidence:     fmt.Sprintf("%d commits and %d files were analyzed locally.", len(input.History.Commits), input.Inventory.TotalFiles),
			Confidence:   confidence,
			WhyItMatters: "The report is grounded in repo artifacts and did not require code upload.",
		})
	}

	var weaknesses []signals.Finding
	if sourceNoTest > 0 {
		weaknesses = append(weaknesses, signals.Finding{
			Label:        "Behavior changes often lack test evidence",
			Evidence:     fmt.Sprintf("%d source-changing commits did not touch test files.", sourceNoTest),
			Confidence:   confidence,
			WhyItMatters: "This does not prove the code is wrong, but it gives reviewers less protection.",
			NextAction:   "For the next behavior-changing PR, add at least one adjacent test before review.",
		})
	}
	if large > 0 {
		weaknesses = append(weaknesses, signals.Finding{
			Label:        "Large changes create review risk",
			Evidence:     fmt.Sprintf("%d recent commits changed more than 12 files.", large),
			Confidence:   confidence,
			WhyItMatters: "Large changes are harder to review and easier to churn after merge.",
			NextAction:   "Split broad refactors from behavior changes.",
		})
	}
	if riskyNoTest > 0 {
		weaknesses = append(weaknesses, signals.Finding{
			Label:        "Risky paths need stronger proof",
			Evidence:     fmt.Sprintf("%d security-sensitive commits had no adjacent test file changes.", riskyNoTest),
			Confidence:   signals.ConfidenceMedium,
			WhyItMatters: "Auth, billing, session, token, and permission changes need sharper verification.",
			NextAction:   "Add targeted tests around security-sensitive edge cases before review.",
		})
	}
	if len(input.History.HighChurnFiles) > 0 {
		weaknesses = append(weaknesses, signals.Finding{
			Label:        "Churn is concentrated in a few files",
			Evidence:     fmt.Sprintf("High-churn files include %s.", strings.Join(input.History.HighChurnFiles, ", ")),
			Confidence:   signals.ConfidenceMedium,
			WhyItMatters: "Repeated edits to the same files can indicate unstable boundaries or incomplete fixes.",
			NextAction:   "Before editing these files again, inspect recent commits and add regression tests around the changed behavior.",
		})
	}
	if len(weaknesses) == 0 {
		weaknesses = append(weaknesses, signals.Finding{
			Label:        "PR-level review burden is unavailable",
			Evidence:     "No detailed PR review metadata was imported for this report.",
			Confidence:   signals.ConfidenceHigh,
			WhyItMatters: "Local git history cannot show review rounds, requested changes, or CI failures before merge.",
			NextAction:   "Provide a GitHub token when you want review burden and PR-level confidence.",
		})
	}

	watchItems := []signals.Finding{{
		Label:        "Coverage is unavailable",
		Evidence:     "No coverage report was imported for this analysis, so test conclusions use file-touch evidence only.",
		Confidence:   signals.ConfidenceHigh,
		WhyItMatters: "Test-file evidence is useful but cannot prove changed-line coverage.",
		NextAction:   "Export coverage later if you want stronger verification evidence.",
	}}
	if fixLike > 0 {
		watchItems = append(watchItems, signals.Finding{
			Label:        "Follow-up fix language appears in history",
			Evidence:     fmt.Sprintf("%d commits matched low-confidence fix or revert message heuristics.", fixLike),
			Confidence:   signals.ConfidenceLow,
			WhyItMatters: "Message heuristics are weak, but repeated fix language can point to durability risk.",
			NextAction:   "Inspect those commits before drawing conclusions.",
		})
	}
	if len(input.AITools) > 0 || len(input.AIModes) > 0 {
		watchItems = append(watchItems, signals.Finding{
			Label:        "AI workflow evidence is self-reported",
			Evidence:     "AI usage came from configuration, not provenance or telemetry.",
			Confidence:   signals.ConfidenceLow,
			WhyItMatters: "The CLI does not detect AI-authored code or calculate token efficiency.",
			NextAction:   "Use AI for adversarial review and test generation before opening PRs.",
		})
	}

	nextActions := []string{}
	for _, weakness := range weaknesses {
		if weakness.NextAction != "" {
			nextActions = append(nextActions, weakness.NextAction)
		}
		if len(nextActions) == 3 {
			break
		}
	}
	for len(nextActions) < 3 {
		fallbacks := []string{
			"Run `contribution preflight --base " + defaultBranch(input.Repo.DefaultBranch) + " --head HEAD` before the next PR.",
			"Keep the next behavior-changing PR small enough to review in one pass.",
			"Pair source changes with nearby tests or explicitly document why tests were not practical.",
		}
		nextActions = append(nextActions, fallbacks[len(nextActions)])
	}

	return signals.WeaknessMap{
		Strengths:   strengths,
		Weaknesses:  weaknesses,
		WatchItems:  watchItems,
		NextActions: nextActions,
		Confidence:  confidence,
	}
}

func buildProfile(input Input, weaknessMap signals.WeaknessMap, analyzed int) signals.ProfileSummary {
	strengths := publicFindings(weaknessMap.Strengths, 3)
	trends := []signals.Finding{}
	for _, strength := range strengths {
		if strings.Contains(strings.ToLower(strength.Label), "test") {
			trends = append(trends, signals.Finding{
				Label:      "Testing discipline visible",
				Evidence:   strength.Evidence,
				Confidence: strength.Confidence,
			})
			break
		}
	}
	if len(trends) == 0 && analyzed > 0 {
		trends = append(trends, signals.Finding{
			Label:      "Contribution quality baseline established",
			Evidence:   fmt.Sprintf("%d recent artifacts analyzed locally.", analyzed),
			Confidence: weaknessMap.Confidence,
		})
	}
	var badges []signals.BadgeCandidate
	for _, strength := range strengths {
		switch {
		case strings.Contains(strings.ToLower(strength.Label), "focused"):
			badges = append(badges, signals.BadgeCandidate{ID: "small_pr_operator", Label: "Small PR operator", Confidence: strength.Confidence})
		case strings.Contains(strings.ToLower(strength.Label), "test"):
			badges = append(badges, signals.BadgeCandidate{ID: "test_discipline_visible", Label: "Test discipline visible", Confidence: strength.Confidence})
		case strings.Contains(strings.ToLower(strength.Label), "documentation"):
			badges = append(badges, signals.BadgeCandidate{ID: "docs_supporter", Label: "Docs supporter", Confidence: strength.Confidence})
		}
	}
	return signals.ProfileSummary{
		DisplayName:        input.DisplayName,
		Headline:           "AI-native contribution profile",
		AnalyzedPRs:        analyzed,
		AnalysisWindowDays: input.SinceDays,
		Confidence:         weaknessMap.Confidence,
		Strengths:          strengths,
		ImprovementTrends:  trends,
		BadgeCandidates:    badges,
	}
}

func publicFindings(findings []signals.Finding, limit int) []signals.Finding {
	if len(findings) < limit {
		limit = len(findings)
	}
	out := make([]signals.Finding, 0, limit)
	for i := 0; i < limit; i++ {
		f := findings[i]
		f.NextAction = ""
		f.WhyItMatters = ""
		out = append(out, f)
	}
	return out
}

func commitTitle(commit gitrepo.Commit) string {
	title := strings.TrimSpace(commit.Subject)
	if title == "" {
		return "Commit " + gitrepo.ShortSHA(commit.SHA)
	}
	return title
}

func testEvidenceLabel(commit gitrepo.Commit) string {
	if commit.TestsTouched {
		return "Tests touched"
	}
	if commit.SourceTouched {
		return "No test files touched"
	}
	return "No behavior-changing source files detected"
}

func durabilityLabel(commit gitrepo.Commit) string {
	switch {
	case commit.IsRevert:
		return "Revert detected"
	case commit.IsFollowUpFix:
		return "Fix-like message heuristic"
	default:
		return "No direct churn signal on this artifact"
	}
}

func scopeDescription(files, lines int) string {
	fileLabel := "files"
	if files == 1 {
		fileLabel = "file"
	}
	if lines > 0 {
		lineLabel := "lines"
		if lines == 1 {
			lineLabel = "line"
		}
		return fmt.Sprintf("%d %s and %d %s", files, fileLabel, lines, lineLabel)
	}
	return fmt.Sprintf("%d %s", files, fileLabel)
}

func defaultBranch(branch string) string {
	if branch == "" {
		return "main"
	}
	return branch
}
