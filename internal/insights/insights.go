// Package insights builds deterministic first-read report conclusions.
package insights

import (
	"fmt"
	"strings"

	"github.com/contribution-dev/contribution/internal/signals"
)

const (
	maxTopFindings = 5
	maxNextPRPlan  = 5
)

type candidate struct {
	priority int
	finding  signals.TopFinding
}

// Build creates the deterministic report-first summary from existing evidence.
func Build(report signals.AnalysisReport) signals.TopRead {
	candidates := collectCandidates(report)
	findings := rankedFindings(candidates)
	nextPlan := nextPRPlan(findings, report)
	headline := headline(report, findings)
	summary := summary(report, findings)
	confidence := report.AgenticReadiness.Confidence
	if confidence == "" {
		confidence = report.WeaknessMap.Confidence
	}
	if confidence == "" {
		confidence = signals.ConfidenceLow
	}
	return signals.TopRead{
		Headline:   headline,
		Summary:    summary,
		Findings:   findings,
		NextPRPlan: nextPlan,
		Confidence: confidence,
	}
}

func collectCandidates(report signals.AnalysisReport) []candidate {
	var out []candidate
	for _, finding := range report.WeaknessMap.Weaknesses {
		label := strings.ToLower(finding.Label)
		switch {
		case strings.Contains(label, "post-merge follow-up"):
			out = append(out, topFinding(10, "pr_follow_up_churn", signals.SeverityHigh, "weakness_map", finding))
		case strings.Contains(label, "checks failed"):
			out = append(out, topFinding(20, "failed_checks", signals.SeverityHigh, "weakness_map", finding))
		case strings.Contains(label, "risky paths"):
			out = append(out, topFinding(30, "risky_no_test_work", signals.SeverityHigh, "weakness_map", finding))
		case strings.Contains(label, "test evidence"):
			out = append(out, topFinding(35, "no_test_evidence", signals.SeverityMedium, "weakness_map", finding))
		case strings.Contains(label, "large changes"):
			out = append(out, topFinding(40, "large_work_units", signals.SeverityMedium, "weakness_map", finding))
		case strings.Contains(label, "churn is concentrated"):
			out = append(out, topFinding(50, "high_churn_files", signals.SeverityMedium, "weakness_map", finding))
		}
	}
	if report.Trends.CurrentWindow.FixLikeCommits > 0 {
		priority := 60
		if highVolumeRepairLoop(report.Trends.CurrentWindow) {
			priority = 38
		}
		out = append(out, candidate{
			priority: priority,
			finding: signals.TopFinding{
				ID:           "fix_like_repair_loop",
				Label:        "Fix-like repair loop",
				Evidence:     repairLoopEvidence(report.Trends.CurrentWindow),
				Severity:     signals.SeverityMedium,
				Confidence:   signals.ConfidenceLow,
				WhyItMatters: "Message heuristics are weak, but repeated fix-like language is useful durability smoke.",
				NextAction:   "Inspect repeat fix patterns and convert the next repeated fix into a regression test.",
				Source:       "trends",
			},
		})
	}
	for _, component := range report.AgenticReadiness.Components {
		switch component.ID {
		case "validation_readiness":
			if component.Score > 0 && component.Score < 60 {
				out = append(out, candidate{
					priority: 70,
					finding: signals.TopFinding{
						ID:           "missing_validation_command",
						Label:        "Validation command gap",
						Evidence:     component.Evidence,
						Severity:     signals.SeverityMedium,
						Confidence:   component.Confidence,
						WhyItMatters: "Agents need one reliable command to check their work before review.",
						NextAction:   component.NextAction,
						Source:       "agentic_readiness",
					},
				})
			}
		case "context_efficiency":
			if component.Score > 0 && component.Score < 80 {
				out = append(out, candidate{
					priority: 80,
					finding: signals.TopFinding{
						ID:           "context_bloat",
						Label:        "Context efficiency risk",
						Evidence:     component.Evidence,
						Severity:     signals.SeverityMedium,
						Confidence:   component.Confidence,
						WhyItMatters: "Large or noisy context makes agent work slower and less predictable.",
						NextAction:   component.NextAction,
						Source:       "agentic_readiness",
					},
				})
			}
		}
	}
	if report.AttributionReadiness.Confidence == signals.ConfidenceLow && report.AttributionReadiness.NextAction != "" {
		out = append(out, candidate{
			priority: 90,
			finding: signals.TopFinding{
				ID:           "attribution_gap",
				Label:        "Work-unit attribution gap",
				Evidence:     report.AttributionReadiness.Summary,
				Severity:     signals.SeverityLow,
				Confidence:   report.AttributionReadiness.Confidence,
				WhyItMatters: "Feature-level AI ROI stays weak until work can be tied to intent.",
				NextAction:   report.AttributionReadiness.NextAction,
				Source:       "attribution_readiness",
			},
		})
	}
	for _, gap := range report.DataGaps {
		if gap.NextAction == "" || futureTelemetryGap(gap) {
			continue
		}
		out = append(out, candidate{
			priority: 100,
			finding: signals.TopFinding{
				ID:           "setup_gap_" + gap.ID,
				Label:        gap.Label + " gap",
				Evidence:     gap.Unlocks,
				Severity:     signals.SeverityLow,
				Confidence:   signals.ConfidenceLow,
				WhyItMatters: gap.Why,
				NextAction:   gap.NextAction,
				Source:       "source_coverage",
			},
		})
	}
	return out
}

func topFinding(priority int, id string, severity signals.Severity, source string, finding signals.Finding) candidate {
	return candidate{
		priority: priority,
		finding: signals.TopFinding{
			ID:           id,
			Label:        finding.Label,
			Evidence:     finding.Evidence,
			Severity:     severity,
			Confidence:   finding.Confidence,
			WhyItMatters: finding.WhyItMatters,
			NextAction:   finding.NextAction,
			Source:       source,
		},
	}
}

func rankedFindings(candidates []candidate) []signals.TopFinding {
	out := make([]signals.TopFinding, 0, min(len(candidates), maxTopFindings))
	seen := map[string]bool{}
	for _, priority := range []int{10, 20, 30, 35, 38, 40, 50, 60, 70, 80, 90, 100} {
		for _, candidate := range candidates {
			if candidate.priority != priority || seen[candidate.finding.ID] {
				continue
			}
			if strings.TrimSpace(candidate.finding.Evidence) == "" {
				continue
			}
			out = append(out, candidate.finding)
			seen[candidate.finding.ID] = true
			if len(out) >= maxTopFindings {
				return out
			}
		}
	}
	return out
}

func highVolumeRepairLoop(window signals.TrendWindow) bool {
	if window.FixLikeCommits >= 5 {
		return true
	}
	if window.FixLikeCommits < 3 || window.Commits <= 0 {
		return false
	}
	return float64(window.FixLikeCommits)/float64(window.Commits) >= 0.5
}

func repairLoopEvidence(window signals.TrendWindow) string {
	if window.Commits > 0 && window.Commits != window.FixLikeCommits {
		return fmt.Sprintf("%d of %d recent %s matched fix/revert-like message heuristics.", window.FixLikeCommits, window.Commits, plural(window.Commits, "commit", "commits"))
	}
	return fmt.Sprintf("%s matched fix/revert-like message heuristics.", countNoun(window.FixLikeCommits, "recent commit", "recent commits"))
}

func countNoun(count int, singular string, pluralValue string) string {
	return fmt.Sprintf("%d %s", count, plural(count, singular, pluralValue))
}

func plural(count int, singular string, pluralValue string) string {
	if count == 1 {
		return singular
	}
	return pluralValue
}

func nextPRPlan(findings []signals.TopFinding, report signals.AnalysisReport) []string {
	actions := make([]string, 0, maxNextPRPlan)
	for _, finding := range findings {
		actions = appendUniqueAction(actions, finding.NextAction)
		if len(actions) >= maxNextPRPlan {
			return actions
		}
	}
	for _, action := range report.WeaknessMap.NextActions {
		actions = appendUniqueAction(actions, action)
		if len(actions) >= maxNextPRPlan {
			return actions
		}
	}
	for _, action := range report.AgenticReadiness.TopActions {
		actions = appendUniqueAction(actions, action)
		if len(actions) >= maxNextPRPlan {
			return actions
		}
	}
	for _, action := range report.SetupActions {
		text := action.Label
		if action.Command != "" {
			text = fmt.Sprintf("%s: %s", action.Label, action.Command)
		}
		actions = appendUniqueAction(actions, text)
		if len(actions) >= maxNextPRPlan {
			return actions
		}
	}
	if len(actions) == 0 {
		actions = append(actions, "Run contribution preflight before the next behavior-changing PR.")
	}
	return actions
}

func headline(report signals.AnalysisReport, findings []signals.TopFinding) string {
	if len(findings) == 0 {
		if report.AgenticReadiness.Grade != "" {
			return fmt.Sprintf("%s-level agentic readiness is established; keep improving the next PR loop.", report.AgenticReadiness.Grade)
		}
		return "Agentic readiness baseline is established."
	}
	switch findings[0].ID {
	case "pr_follow_up_churn":
		return "Post-merge follow-up churn is the first thing to fix."
	case "failed_checks":
		return "Failing checks are the first agentic-readiness drag."
	case "risky_no_test_work", "no_test_evidence":
		return "Behavior and risky-path changes need stronger test evidence before review."
	case "large_work_units":
		return "Large work units are creating review risk."
	case "high_churn_files":
		return "High-churn files are the next review hot spot."
	case "fix_like_repair_loop":
		return "Recent fix-like commits suggest a repair loop to inspect."
	case "missing_validation_command":
		return "Agents need one reliable validation command before more work."
	case "context_bloat":
		return "Context size is likely slowing agent work."
	case "attribution_gap":
		return "Work-unit attribution is the main insight gap."
	default:
		return findings[0].Label + " is the top read."
	}
}

func summary(report signals.AnalysisReport, findings []signals.TopFinding) string {
	parts := []string{}
	if report.AgenticReadiness.Grade != "" {
		parts = append(parts, fmt.Sprintf("Readiness: %s (%d/100), %s confidence.", report.AgenticReadiness.Grade, report.AgenticReadiness.Score, report.AgenticReadiness.Confidence))
	}
	if len(findings) > 0 {
		parts = append(parts, findings[0].Evidence)
	} else if report.AgenticReadiness.Summary != "" {
		parts = append(parts, report.AgenticReadiness.Summary)
	}
	if len(parts) == 0 {
		return "This deterministic summary is based on the available local evidence."
	}
	return strings.Join(parts, " ")
}

func appendUniqueAction(actions []string, action string) []string {
	action = strings.TrimSpace(action)
	if action == "" {
		return actions
	}
	for _, existing := range actions {
		if existing == action {
			return actions
		}
	}
	return append(actions, action)
}

func futureTelemetryGap(gap signals.DataGap) bool {
	switch gap.ID {
	case "ai_spend_telemetry", "agent_session_telemetry", "deployment_product_telemetry":
		return true
	}
	return false
}
