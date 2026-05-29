// Package scoring turns normalized evidence into artifact labels and coaching.
package scoring

import (
	"fmt"
	"strings"
	"time"

	gitrepo "github.com/contribution-dev/contribution/internal/git"
	"github.com/contribution-dev/contribution/internal/github"
	"github.com/contribution-dev/contribution/internal/signals"
)

// Input is the deterministic evidence available to V1 scoring.
type Input struct {
	Repo               signals.RepoMetadata
	History            gitrepo.History
	PriorHistory       gitrepo.History
	GitHub             github.Metadata
	Inventory          signals.FileSummary
	Coverage           signals.CoverageSummary
	AnalyzerFindings   []signals.AnalyzerFinding
	Signals            []signals.Signal
	CurrentWindowStart time.Time
	CurrentWindowEnd   time.Time
	PriorWindowStart   time.Time
	PriorWindowEnd     time.Time
	SinceDays          int
	MaxCards           int
	DisplayName        string
	AITools            []string
	AIModes            []string
}

// Output is the labeled report state.
type Output struct {
	Cards       []signals.PRQualityCard
	WeaknessMap signals.WeaknessMap
	Trends      signals.TrendComparison
	DeepDives   signals.AnalysisDeepDives
	Profile     signals.ProfileSummary
	Limitations []string
}

// Build creates cards, weakness map, and profile summary.
func Build(input Input) Output {
	cards := buildCards(input)
	weaknessMap := buildWeaknessMap(input, cards)
	trends := buildTrendComparison(input)
	deepDives := buildDeepDives(input, cards)
	profile := buildProfile(input, weaknessMap, trends, len(cards))
	return Output{Cards: cards, WeaknessMap: weaknessMap, Trends: trends, DeepDives: deepDives, Profile: profile, Limitations: buildLimitations(input, cards)}
}

func buildCards(input Input) []signals.PRQualityCard {
	if input.MaxCards <= 0 {
		input.MaxCards = 20
	}
	if input.GitHub.Available && len(input.GitHub.PRs) > 0 {
		cards := make([]signals.PRQualityCard, 0, min(len(input.GitHub.PRs), input.MaxCards))
		associatedCommits := associatedPRCommitSHAs(input.GitHub.PRs)
		for _, pr := range input.GitHub.PRs {
			cards = append(cards, cardFromPR(pr, input.History))
			if len(cards) >= input.MaxCards {
				break
			}
		}
		for _, commit := range input.History.Commits {
			if associatedCommits[normalizeSHA(commit.SHA)] {
				continue
			}
			cards = append(cards, cardFromCommit(commit))
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

func associatedPRCommitSHAs(prs []github.PullRequest) map[string]bool {
	out := map[string]bool{}
	for _, pr := range prs {
		if sha := normalizeSHA(pr.MergeCommitSHA); sha != "" {
			out[sha] = true
		}
	}
	return out
}

func buildLimitations(input Input, cards []signals.PRQualityCard) []string {
	var limitations []string
	if input.GitHub.Available && len(input.GitHub.PRs) > 0 {
		var localCards int
		for _, card := range cards {
			if card.PRNumber == 0 {
				localCards++
			}
		}
		if localCards > 0 {
			limitations = append(limitations, fmt.Sprintf("%d local commit card(s) were included because they did not match imported GitHub PR merge commits. This covers direct merges and sparse PR metadata, but squash or rebase workflows can still duplicate work when merge SHAs are unavailable.", localCards))
		}
	}
	return limitations
}

func normalizeSHA(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func cardFromPR(pr github.PullRequest, history gitrepo.History) signals.PRQualityCard {
	totalLines := pr.Additions + pr.Deletions
	label := "mixed"
	confidence := signals.ConfidenceMedium
	mainRisk := "Test and review details are limited to currently imported GitHub metadata."
	nextAction := "Use preflight on the current diff to add file-level test and risk evidence before review."
	var strengths []signals.Finding
	var risks []signals.Finding
	sourceFiles, testFiles, riskyFiles := classifyPaths(pr.Files)
	durability := prDurabilityContext(pr, history)
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
	if pr.RequestedChanges > 0 {
		risks = append(risks, signals.Finding{
			Label:        "Requested changes during review",
			Evidence:     fmt.Sprintf("PR #%d had %d requested-change review(s).", pr.Number, pr.RequestedChanges),
			Confidence:   signals.ConfidenceMedium,
			WhyItMatters: "Requested changes are useful review signal, but repeated rounds can indicate unclear scope or missing preflight checks.",
			NextAction:   "Use preflight to call out risky paths, tests, and review focus before opening comparable PRs.",
		})
		if label == "strong" {
			label = "mixed"
		}
		mainRisk = "Review requested changes before merge."
		nextAction = "Preflight comparable changes and spell out review boundaries before opening review."
	}
	if pr.FailedChecks > 0 {
		risks = append(risks, signals.Finding{
			Label:        "Checks did not all pass",
			Evidence:     fmt.Sprintf("PR #%d had %d failing or non-success check run(s).", pr.Number, pr.FailedChecks),
			Confidence:   signals.ConfidenceMedium,
			WhyItMatters: "Failing checks before merge are churn and reviewer-load signals.",
			NextAction:   "Run the same validation locally before opening the next comparable PR.",
		})
		label = "risky"
		mainRisk = "Checks failed before merge."
		nextAction = "Run local validation before review and include failures in the PR notes if they are expected."
	}
	if durability.FollowUpArtifacts > 0 {
		risks = append(risks, signals.Finding{
			Label:        "Post-merge follow-up churn",
			Evidence:     fmt.Sprintf("After PR #%d merged, %d fix/revert-like commit(s) touched files from the PR.", pr.Number, durability.FollowUpArtifacts),
			Confidence:   signals.ConfidenceMedium,
			WhyItMatters: "Follow-up fixes on the same files are direct durability evidence, though message heuristics remain imperfect.",
			NextAction:   "Inspect the follow-up commits before repeating this PR shape.",
		})
		label = "risky"
		mainRisk = "Post-merge follow-up churn touched this PR's files."
		nextAction = "Inspect later fixes on these files and add regression coverage before a similar change."
	} else if pr.CheckRuns > 0 && pr.FailedChecks == 0 && len(durability.HighChurnFiles) == 0 {
		strengths = append(strengths, signals.Finding{
			Label:      "Durability evidence",
			Evidence:   fmt.Sprintf("PR #%d had no matching fix/revert follow-up in local history and imported checks passed or were neutral/skipped.", pr.Number),
			Confidence: signals.ConfidenceMedium,
		})
	}
	if sourceFiles > 0 && testFiles == 0 {
		risks = append(risks, signals.Finding{
			Label:        "No test files visible",
			Evidence:     fmt.Sprintf("PR #%d changed %d source file(s), and imported file metadata showed no test file changes.", pr.Number, sourceFiles),
			Confidence:   signals.ConfidenceMedium,
			WhyItMatters: "Reviewers and future changes have less protection when behavior changes lack test evidence.",
			NextAction:   "Add at least one nearby regression test before review when behavior changes.",
		})
		if label == "strong" {
			label = "mixed"
		}
		mainRisk = "Source changes had no visible test-file evidence."
		nextAction = "Add nearby tests for the changed behavior or document why tests are not practical."
	}
	if riskyFiles > 0 && testFiles == 0 {
		label = "risky"
		mainRisk = "Security-sensitive paths changed without visible test-file evidence."
		nextAction = "Add targeted tests around authorization, billing, session, token, or permission edge cases."
	}
	testEvidence := prTestEvidence(pr, sourceFiles, testFiles)
	reviewBurden := prReviewBurden(pr)
	return signals.PRQualityCard{
		PRNumber:     pr.Number,
		Title:        pr.Title,
		URL:          pr.URL,
		Label:        label,
		Confidence:   confidence,
		Summary:      fmt.Sprintf("Merged PR touching %d files with %d additions and %d deletions.", pr.ChangedFiles, pr.Additions, pr.Deletions),
		Scope:        scopeDescription(pr.ChangedFiles, totalLines),
		TestEvidence: testEvidence,
		ReviewBurden: reviewBurden,
		Durability:   prDurability(pr, durability),
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
	var requestedChangePRs, failedCheckPRs, followUpPRs int
	for _, pr := range input.GitHub.PRs {
		if pr.RequestedChanges > 0 {
			requestedChangePRs++
		}
		if pr.FailedChecks > 0 {
			failedCheckPRs++
		}
		if prDurabilityContext(pr, input.History).FollowUpArtifacts > 0 {
			followUpPRs++
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
	if followUpPRs > 0 {
		weaknesses = append(weaknesses, signals.Finding{
			Label:        "Some PRs needed post-merge follow-up",
			Evidence:     fmt.Sprintf("%d imported PR(s) had later fix/revert-like commits touching their changed files.", followUpPRs),
			Confidence:   signals.ConfidenceMedium,
			WhyItMatters: "This is stronger durability evidence than commit-message counts alone because it ties follow-up churn back to the PR's files.",
			NextAction:   "Inspect those PRs before repeating the same shape, especially if they also changed high-churn files.",
		})
	}
	if requestedChangePRs > 0 {
		weaknesses = append(weaknesses, signals.Finding{
			Label:        "Review requested changes on imported PRs",
			Evidence:     fmt.Sprintf("%d imported PR(s) had requested-change reviews.", requestedChangePRs),
			Confidence:   signals.ConfidenceMedium,
			WhyItMatters: "Requested changes are useful, but repeated review rounds can point to unclear scope or missing preflight checks.",
			NextAction:   "Use preflight to call out scope, risky paths, and test evidence before opening comparable PRs.",
		})
	}
	if failedCheckPRs > 0 {
		weaknesses = append(weaknesses, signals.Finding{
			Label:        "Checks failed on imported PRs",
			Evidence:     fmt.Sprintf("%d imported PR(s) had failing or non-success check runs.", failedCheckPRs),
			Confidence:   signals.ConfidenceMedium,
			WhyItMatters: "Failing checks before merge add churn and reviewer load.",
			NextAction:   "Run the same validation locally before review and keep CI-only failures visible in the PR notes.",
		})
	}
	if len(input.AnalyzerFindings) > 0 {
		weaknesses = append(weaknesses, signals.Finding{
			Label:        "Optional analyzers found issues",
			Evidence:     fmt.Sprintf("%d optional analyzer finding(s) were imported.", len(input.AnalyzerFindings)),
			Confidence:   signals.ConfidenceMedium,
			WhyItMatters: "Static, secret, dependency, and vulnerability findings are not proof of broken code, but they are concrete risk evidence to triage before review.",
			NextAction:   "Triage the analyzer findings and separate new risk from inherited backlog before sharing the report.",
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

	var watchItems []signals.Finding
	if input.Coverage.Status == "available" {
		watchItems = append(watchItems, signals.Finding{
			Label:        "Coverage was imported",
			Evidence:     fmt.Sprintf("Imported coverage covers %.1f%% of executable lines in %d file(s).", input.Coverage.Percent, len(input.Coverage.Files)),
			Confidence:   signals.ConfidenceMedium,
			WhyItMatters: "Coverage evidence strengthens test conclusions beyond file-touch heuristics.",
			NextAction:   "Use changed-line coverage in preflight for the next behavior-changing PR.",
		})
	} else {
		watchItems = append(watchItems, signals.Finding{
			Label:        "Coverage is unavailable",
			Evidence:     "No coverage report was imported for this analysis, so test conclusions use file-touch evidence only.",
			Confidence:   signals.ConfidenceHigh,
			WhyItMatters: "Test-file evidence is useful but cannot prove changed-line coverage.",
			NextAction:   "Export coverage later if you want stronger verification evidence.",
		})
	}
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

func buildDeepDives(input Input, cards []signals.PRQualityCard) signals.AnalysisDeepDives {
	cardByCommit := map[string]signals.PRQualityCard{}
	for _, card := range cards {
		if len(card.Evidence) == 0 {
			continue
		}
		for _, evidence := range card.Evidence {
			if evidence.ID != "" {
				cardByCommit[evidence.ID] = card
			}
		}
	}
	dives := signals.AnalysisDeepDives{
		HighChurn:       []signals.HighChurnDeepDive{},
		NoTestArtifacts: []signals.NoTestArtifactDeepDive{},
	}
	for _, file := range input.History.HighChurnFiles {
		dive := signals.HighChurnDeepDive{
			Path:       file,
			Touches:    input.History.FileTouchCount[file],
			NextAction: fmt.Sprintf("Before editing %s again, inspect the recent touches and add regression coverage around the behavior you are changing.", file),
			Confidence: signals.ConfidenceMedium,
		}
		for _, commit := range input.History.Commits {
			if !commitTouchesPath(commit, file) {
				continue
			}
			card := cardByCommit[commit.SHA]
			dive.Artifacts = append(dive.Artifacts, artifactFromCommit(commit, card))
			if len(dive.Artifacts) >= 4 {
				break
			}
		}
		dives.HighChurn = append(dives.HighChurn, dive)
	}
	for _, commit := range input.History.Commits {
		if !commit.SourceTouched || commit.TestsTouched {
			continue
		}
		card := cardByCommit[commit.SHA]
		risk := "Source files changed without test-file evidence."
		nextAction := "Add at least one nearby test for the changed behavior before review."
		if commit.RiskyTouched {
			risk = "Security-sensitive source files changed without test-file evidence."
			nextAction = "Add targeted tests around authorization, billing, session, token, or permission edge cases."
		}
		dives.NoTestArtifacts = append(dives.NoTestArtifacts, signals.NoTestArtifactDeepDive{
			Artifact:           artifactFromCommit(commit, card),
			ChangedSourceFiles: sourcePaths(commit.Files),
			Risk:               risk,
			NextAction:         nextAction,
			Confidence:         signals.ConfidenceMedium,
		})
		if len(dives.NoTestArtifacts) >= 5 {
			break
		}
	}
	if input.GitHub.Available {
		for _, pr := range input.GitHub.PRs {
			sourceFiles, testFiles, riskyFiles := classifyPaths(pr.Files)
			if sourceFiles == 0 || testFiles > 0 {
				continue
			}
			risk := "Imported PR file metadata showed source changes without test-file changes."
			nextAction := "Add nearby tests for the changed behavior or document why tests are not practical."
			if riskyFiles > 0 {
				risk = "Imported PR file metadata showed risky source changes without test-file changes."
				nextAction = "Add targeted tests around security-sensitive edge cases before review."
			}
			dives.NoTestArtifacts = append(dives.NoTestArtifacts, signals.NoTestArtifactDeepDive{
				Artifact: signals.DeepDiveArtifact{
					ID:           fmt.Sprintf("pr-%d", pr.Number),
					Label:        fmt.Sprintf("PR #%d", pr.Number),
					Title:        pr.Title,
					Scope:        scopeDescription(pr.ChangedFiles, pr.Additions+pr.Deletions),
					TestEvidence: prTestEvidence(pr, sourceFiles, testFiles),
					MainRisk:     risk,
					NextAction:   nextAction,
					Confidence:   signals.ConfidenceMedium,
				},
				ChangedSourceFiles: sourcePathStrings(pr.Files),
				Risk:               risk,
				NextAction:         nextAction,
				Confidence:         signals.ConfidenceMedium,
			})
			if len(dives.NoTestArtifacts) >= 5 {
				break
			}
		}
	}
	return dives
}

func buildProfile(input Input, weaknessMap signals.WeaknessMap, comparison signals.TrendComparison, analyzed int) signals.ProfileSummary {
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
	for _, finding := range comparison.Findings {
		if isPositiveTrendFinding(finding) {
			trends = append(trends, signals.Finding{
				Label:      finding.Label,
				Evidence:   finding.Evidence,
				Confidence: finding.Confidence,
			})
		}
		if len(trends) >= 2 {
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

func isPositiveTrendFinding(finding signals.Finding) bool {
	text := strings.ToLower(finding.Label + " " + finding.Evidence)
	return strings.Contains(text, "improv") || strings.Contains(text, "stronger") || strings.Contains(text, "down")
}

func prTestEvidence(pr github.PullRequest, sourceFiles int, testFiles int) string {
	switch {
	case len(pr.Files) == 0:
		return "Changed-file metadata was unavailable, so test-file evidence is unknown."
	case sourceFiles > 0 && testFiles == 0:
		return fmt.Sprintf("No test files visible across %d imported changed file(s).", len(pr.Files))
	case testFiles > 0:
		return fmt.Sprintf("%d test file(s) changed with %d source file(s).", testFiles, sourceFiles)
	default:
		return "No behavior-changing source files detected in imported file metadata."
	}
}

func prReviewBurden(pr github.PullRequest) string {
	parts := []string{}
	if pr.ReviewCount > 0 {
		parts = append(parts, fmt.Sprintf("%d reviews", pr.ReviewCount))
	}
	if pr.RequestedChanges > 0 {
		parts = append(parts, fmt.Sprintf("%d requested-change reviews", pr.RequestedChanges))
	}
	if pr.ReviewComments > 0 {
		parts = append(parts, fmt.Sprintf("%d review comments", pr.ReviewComments))
	}
	if pr.IssueComments > 0 {
		parts = append(parts, fmt.Sprintf("%d issue comments", pr.IssueComments))
	}
	if len(parts) == 0 {
		return "No detailed review burden metadata was imported for this PR."
	}
	return strings.Join(parts, ", ") + "."
}

type prDurabilityEvidence struct {
	FollowUpArtifacts int
	HighChurnFiles    []string
}

func prDurabilityContext(pr github.PullRequest, history gitrepo.History) prDurabilityEvidence {
	if len(pr.Files) == 0 {
		return prDurabilityEvidence{}
	}
	prFiles := map[string]bool{}
	for _, file := range pr.Files {
		prFiles[file] = true
	}
	evidence := prDurabilityEvidence{}
	for _, file := range history.HighChurnFiles {
		if prFiles[file] {
			evidence.HighChurnFiles = append(evidence.HighChurnFiles, file)
		}
	}
	for _, commit := range history.Commits {
		if !pr.MergedAt.IsZero() && !commit.Date.IsZero() && !commit.Date.After(pr.MergedAt) {
			continue
		}
		if !commit.IsFollowUpFix && !commit.IsRevert {
			continue
		}
		for _, file := range commit.Files {
			if prFiles[file.Path] {
				evidence.FollowUpArtifacts++
				break
			}
		}
	}
	return evidence
}

func prDurability(pr github.PullRequest, evidence prDurabilityEvidence) string {
	switch {
	case evidence.FollowUpArtifacts > 0 && len(evidence.HighChurnFiles) > 0:
		return fmt.Sprintf("%d later fix/revert-like commit(s) touched files from this PR; high-churn overlap: %s.", evidence.FollowUpArtifacts, strings.Join(evidence.HighChurnFiles, ", "))
	case evidence.FollowUpArtifacts > 0:
		return fmt.Sprintf("%d later fix/revert-like commit(s) touched files from this PR.", evidence.FollowUpArtifacts)
	case len(evidence.HighChurnFiles) > 0:
		return fmt.Sprintf("No later fix/revert-like commit matched this PR, but changed files overlap high-churn files: %s.", strings.Join(evidence.HighChurnFiles, ", "))
	case pr.CheckRuns == 0:
		return "Post-merge churn is limited to local history; check-run metadata was not imported."
	case pr.FailedChecks > 0:
		return fmt.Sprintf("%d of %d imported check runs did not pass.", pr.FailedChecks, pr.CheckRuns)
	default:
		return fmt.Sprintf("No matching fix/revert follow-up was found in local history; %d imported check runs passed or were neutral/skipped.", pr.CheckRuns)
	}
}

func classifyPaths(paths []string) (sourceFiles int, testFiles int, riskyFiles int) {
	for _, path := range paths {
		class := gitrepo.ClassifyPath(path)
		if class.IsSource {
			sourceFiles++
		}
		if class.IsTest {
			testFiles++
		}
		if class.IsSecurityRelated {
			riskyFiles++
		}
	}
	return sourceFiles, testFiles, riskyFiles
}

func commitTouchesPath(commit gitrepo.Commit, path string) bool {
	for _, file := range commit.Files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func artifactFromCommit(commit gitrepo.Commit, card signals.PRQualityCard) signals.DeepDiveArtifact {
	scope := scopeDescription(len(commit.Files), gitrepo.TotalChangedLines(commit.Files))
	testEvidence := testEvidenceLabel(commit)
	mainRisk := "No specific risk recorded."
	nextAction := "Inspect this artifact before repeating the same pattern."
	confidence := signals.ConfidenceMedium
	if card.Scope != "" {
		scope = card.Scope
	}
	if card.TestEvidence != "" {
		testEvidence = card.TestEvidence
	}
	if card.MainRisk != "" {
		mainRisk = card.MainRisk
	}
	if card.NextAction != "" {
		nextAction = card.NextAction
	}
	if card.Confidence != "" {
		confidence = card.Confidence
	}
	return signals.DeepDiveArtifact{
		ID:           commit.SHA,
		Label:        "commit " + gitrepo.ShortSHA(commit.SHA),
		Title:        commitTitle(commit),
		Scope:        scope,
		TestEvidence: testEvidence,
		MainRisk:     mainRisk,
		NextAction:   nextAction,
		Confidence:   confidence,
	}
}

func sourcePaths(files []gitrepo.ChangedFile) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		class := gitrepo.ClassifyPath(file.Path)
		if class.IsSource {
			paths = append(paths, file.Path)
		}
		if len(paths) >= 6 {
			break
		}
	}
	return paths
}

func sourcePathStrings(files []string) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		class := gitrepo.ClassifyPath(file)
		if class.IsSource {
			paths = append(paths, file)
		}
		if len(paths) >= 6 {
			break
		}
	}
	return paths
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
