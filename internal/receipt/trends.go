package receipt

import (
	"fmt"
	"math"
	"time"

	gitrepo "github.com/contribution-dev/contribution/internal/git"
	"github.com/contribution-dev/contribution/internal/signals"
)

func buildTrendComparison(input Input) signals.TrendComparison {
	current := trendWindow("recent", input.CurrentWindowStart, input.CurrentWindowEnd, input.History)
	prior := trendWindow("prior", input.PriorWindowStart, input.PriorWindowEnd, input.PriorHistory)
	confidence := trendConfidence(current, prior)
	comparison := signals.TrendComparison{
		Status:        "available",
		CurrentWindow: current,
		PriorWindow:   prior,
		Metrics:       []signals.TrendMetric{},
		Findings:      []signals.Finding{},
		Confidence:    confidence,
	}
	switch {
	case current.Commits == 0:
		comparison.Status = "unavailable"
		comparison.Reason = "No commits were found in the recent analysis window, so there is no current behavior to compare."
		comparison.Findings = []signals.Finding{{
			Label:        "Trend unavailable",
			Evidence:     comparison.Reason,
			Confidence:   signals.ConfidenceHigh,
			WhyItMatters: "Trend evidence needs at least one recent artifact.",
			NextAction:   "Run analysis again after the next meaningful change.",
		}}
		return comparison
	case prior.Commits == 0:
		comparison.Status = "baseline"
		comparison.Reason = "No commits were found in the prior comparison window, so this run establishes a trend baseline."
		comparison.Metrics = buildTrendMetrics(current, prior, confidence)
		comparison.Findings = []signals.Finding{{
			Label:        "Trend baseline established",
			Evidence:     fmt.Sprintf("The recent window has %d analyzed commit artifact(s); the prior window has none.", current.Commits),
			Confidence:   signals.ConfidenceMedium,
			WhyItMatters: "The next run can compare against this baseline instead of relying only on one-window patterns.",
			NextAction:   "Use `contribution analyze --repo . --format all` again after the next few meaningful commits.",
		}}
		return comparison
	}
	comparison.Metrics = buildTrendMetrics(current, prior, confidence)
	comparison.Findings = trendFindings(comparison.Metrics, confidence)
	return comparison
}

func trendWindow(label string, since time.Time, until time.Time, history gitrepo.History) signals.TrendWindow {
	window := signals.TrendWindow{
		Label:          label,
		Since:          since.UTC(),
		Until:          until.UTC(),
		Commits:        len(history.Commits),
		HighChurnFiles: len(history.HighChurnFiles),
	}
	for _, commit := range history.Commits {
		lineCount := gitrepo.TotalChangedLines(commit.Files)
		if commit.SourceTouched {
			window.SourceCommits++
			if !commit.TestsTouched {
				window.SourceWithoutTestsCommits++
			}
		}
		if commit.TestsTouched {
			window.TestTouchedCommits++
		}
		if len(commit.Files) > 12 || lineCount > 800 {
			window.LargeCommits++
		}
		if commit.RiskyTouched && !commit.TestsTouched {
			window.RiskyWithoutTestsCommits++
		}
		if commit.IsFollowUpFix || commit.IsRevert {
			window.FixLikeCommits++
		}
	}
	return window
}

func trendConfidence(current signals.TrendWindow, prior signals.TrendWindow) signals.Confidence {
	switch {
	case current.Commits == 0:
		return signals.ConfidenceLow
	case current.Commits < 3 || prior.Commits < 3:
		return signals.ConfidenceLow
	default:
		return signals.ConfidenceMedium
	}
}

func buildTrendMetrics(current signals.TrendWindow, prior signals.TrendWindow, confidence signals.Confidence) []signals.TrendMetric {
	return []signals.TrendMetric{
		rateTrendMetric(
			"source_test_evidence_rate",
			"Source changes with test evidence",
			float64(current.SourceCommits-current.SourceWithoutTestsCommits),
			float64(current.SourceCommits),
			float64(prior.SourceCommits-prior.SourceWithoutTestsCommits),
			float64(prior.SourceCommits),
			true,
			10,
			fmt.Sprintf("Source commits with test-file evidence changed from %s in the prior window to %s in the recent window.", formatPercent(rate(prior.SourceCommits-prior.SourceWithoutTestsCommits, prior.SourceCommits)), formatPercent(rate(current.SourceCommits-current.SourceWithoutTestsCommits, current.SourceCommits))),
			"Test-file evidence is not proof, but it gives future you and reviewers a stronger safety net for behavior changes.",
			"For behavior-changing work, keep pairing source changes with nearby tests or explain why tests were not practical.",
			confidence,
		),
		rateTrendMetric(
			"large_change_rate",
			"Large change rate",
			float64(current.LargeCommits),
			float64(current.Commits),
			float64(prior.LargeCommits),
			float64(prior.Commits),
			false,
			10,
			fmt.Sprintf("Large commits changed from %d/%d in the prior window to %d/%d in the recent window.", prior.LargeCommits, prior.Commits, current.LargeCommits, current.Commits),
			"Large changes are harder to review and easier to churn.",
			"Split broad refactors from behavior changes before opening review.",
			confidence,
		),
		rateTrendMetric(
			"fix_like_rate",
			"Fix/revert-like churn rate",
			float64(current.FixLikeCommits),
			float64(current.Commits),
			float64(prior.FixLikeCommits),
			float64(prior.Commits),
			false,
			10,
			fmt.Sprintf("Fix/revert-like commits changed from %d/%d in the prior window to %d/%d in the recent window.", prior.FixLikeCommits, prior.Commits, current.FixLikeCommits, current.Commits),
			"Message heuristics are low-confidence, but a rising fix/revert pattern is useful durability smoke.",
			"Inspect repeat fix patterns and convert the next repeated fix into a regression test.",
			confidence,
		),
		countTrendMetric(
			"risky_without_tests_count",
			"Risky path changes without test evidence",
			float64(current.RiskyWithoutTestsCommits),
			float64(prior.RiskyWithoutTestsCommits),
			float64(current.Commits),
			float64(prior.Commits),
			true,
			1,
			fmt.Sprintf("Risky commits without test-file evidence changed from %d in the prior window to %d in the recent window.", prior.RiskyWithoutTestsCommits, current.RiskyWithoutTestsCommits),
			"Auth, billing, token, session, and permission changes need sharper verification than ordinary source edits.",
			"Treat risky-path source changes as test-required unless there is a clear reason tests are not practical.",
			confidence,
		),
		countTrendMetric(
			"high_churn_files",
			"High-churn file concentration",
			float64(current.HighChurnFiles),
			float64(prior.HighChurnFiles),
			float64(current.Commits),
			float64(prior.Commits),
			true,
			1,
			fmt.Sprintf("High-churn files changed from %d in the prior window to %d in the recent window.", prior.HighChurnFiles, current.HighChurnFiles),
			"Concentrated churn can point to unstable boundaries or incomplete fixes.",
			"Before editing a high-churn file again, inspect recent touches and add regression coverage around the behavior being changed.",
			confidence,
		),
	}
}

func rateTrendMetric(id string, label string, currentNumerator float64, currentDenominator float64, priorNumerator float64, priorDenominator float64, higherIsBetter bool, threshold float64, evidence string, why string, next string, confidence signals.Confidence) signals.TrendMetric {
	current := percentValue(currentNumerator, currentDenominator)
	prior := percentValue(priorNumerator, priorDenominator)
	direction := trendDirection(current, prior, currentDenominator, priorDenominator, higherIsBetter, threshold)
	return signals.TrendMetric{
		ID:           id,
		Label:        label,
		CurrentValue: current,
		PriorValue:   prior,
		Delta:        current - prior,
		Unit:         "percent",
		Direction:    direction,
		Evidence:     evidence,
		Confidence:   confidenceForDirection(confidence, direction),
		WhyItMatters: why,
		NextAction:   next,
	}
}

func countTrendMetric(id string, label string, current float64, prior float64, currentSample float64, priorSample float64, lowerIsBetter bool, threshold float64, evidence string, why string, next string, confidence signals.Confidence) signals.TrendMetric {
	higherIsBetter := !lowerIsBetter
	direction := trendDirection(current, prior, currentSample, priorSample, higherIsBetter, threshold)
	return signals.TrendMetric{
		ID:           id,
		Label:        label,
		CurrentValue: current,
		PriorValue:   prior,
		Delta:        current - prior,
		Unit:         "count",
		Direction:    direction,
		Evidence:     evidence,
		Confidence:   confidenceForDirection(confidence, direction),
		WhyItMatters: why,
		NextAction:   next,
	}
}

func trendDirection(current float64, prior float64, currentSample float64, priorSample float64, higherIsBetter bool, threshold float64) string {
	if priorSample == 0 && currentSample == 0 {
		return "unknown"
	}
	if currentSample == 0 {
		return "unknown"
	}
	if priorSample == 0 {
		return "baseline"
	}
	delta := current - prior
	if math.Abs(delta) < threshold {
		return "steady"
	}
	if higherIsBetter {
		if delta > 0 {
			return "improved"
		}
		return "regressed"
	}
	if delta < 0 {
		return "improved"
	}
	return "regressed"
}

func confidenceForDirection(confidence signals.Confidence, direction string) signals.Confidence {
	if direction == "baseline" || direction == "unknown" {
		return signals.ConfidenceLow
	}
	return confidence
}

func trendFindings(metrics []signals.TrendMetric, confidence signals.Confidence) []signals.Finding {
	findings := []signals.Finding{}
	for _, metric := range metrics {
		switch metric.Direction {
		case "improved":
			findings = append(findings, positiveTrendFinding(metric))
		case "regressed":
			findings = append(findings, regressionTrendFinding(metric))
		}
		if len(findings) >= 4 {
			break
		}
	}
	if len(findings) > 0 {
		return findings
	}
	return []signals.Finding{{
		Label:        "Trend is steady",
		Evidence:     "The recent and prior windows did not show a meaningful directional change in test evidence, large changes, fix/revert-like churn, risky untested changes, or high-churn concentration.",
		Confidence:   confidence,
		WhyItMatters: "A steady trend means the current weakness map is the better guide for what to change next.",
		NextAction:   "Use the next preflight run to intentionally move one pattern, such as test evidence on behavior changes.",
	}}
}

func positiveTrendFinding(metric signals.TrendMetric) signals.Finding {
	switch metric.ID {
	case "source_test_evidence_rate":
		return trendFinding("Test evidence improved", metric)
	case "large_change_rate":
		return trendFinding("Large-change rate came down", metric)
	case "fix_like_rate":
		return trendFinding("Fix/revert-like churn came down", metric)
	case "risky_without_tests_count":
		return trendFinding("Risky untested changes came down", metric)
	case "high_churn_files":
		return trendFinding("Churn concentration came down", metric)
	default:
		return trendFinding(metric.Label+" improved", metric)
	}
}

func regressionTrendFinding(metric signals.TrendMetric) signals.Finding {
	switch metric.ID {
	case "source_test_evidence_rate":
		return trendFinding("Test evidence slipped", metric)
	case "large_change_rate":
		return trendFinding("Large changes increased", metric)
	case "fix_like_rate":
		return trendFinding("Fix/revert-like churn increased", metric)
	case "risky_without_tests_count":
		return trendFinding("Risky untested changes increased", metric)
	case "high_churn_files":
		return trendFinding("Churn concentration increased", metric)
	default:
		return trendFinding(metric.Label+" regressed", metric)
	}
}

func trendFinding(label string, metric signals.TrendMetric) signals.Finding {
	return signals.Finding{
		Label:        label,
		Evidence:     metric.Evidence,
		Confidence:   metric.Confidence,
		WhyItMatters: metric.WhyItMatters,
		NextAction:   metric.NextAction,
	}
}

func rate(numerator int, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator) * 100
}

func percentValue(numerator float64, denominator float64) float64 {
	if denominator <= 0 {
		return 0
	}
	return numerator / denominator * 100
}

func formatPercent(value float64) string {
	return fmt.Sprintf("%.1f%%", value)
}
