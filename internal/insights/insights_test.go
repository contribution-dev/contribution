package insights

import (
	"strings"
	"testing"

	"github.com/contribution-dev/contribution/internal/signals"
)

func TestBuildPrioritizesFollowUpChurnAheadOfSetupGaps(t *testing.T) {
	report := baseAnalysis()
	report.WeaknessMap.Weaknesses = []signals.Finding{{
		Label:        "Some PRs needed post-merge follow-up",
		Evidence:     "6 imported PR(s) had later fix/revert-like commits touching their changed files.",
		Confidence:   signals.ConfidenceMedium,
		WhyItMatters: "This is stronger durability evidence than commit-message counts alone.",
		NextAction:   "Inspect those PRs before repeating the same shape.",
	}, {
		Label:      "Churn is concentrated in a few files",
		Evidence:   "High-churn files include apps/mobile/src/lib/realtime-voice-session.ts.",
		Confidence: signals.ConfidenceMedium,
		NextAction: "Add regression coverage around the changed behavior.",
	}}
	report.AgenticReadiness.TopActions = []string{"Run contribution doctor and install optional safety tools."}

	top := Build(report)
	if !strings.Contains(strings.ToLower(top.Headline), "post-merge follow-up") {
		t.Fatalf("headline = %q, want follow-up churn first", top.Headline)
	}
	if len(top.Findings) == 0 || top.Findings[0].ID != "pr_follow_up_churn" {
		t.Fatalf("findings = %+v, want PR follow-up churn first", top.Findings)
	}
	if got := strings.Join(top.NextPRPlan, "\n"); !strings.Contains(got, "Inspect those PRs") {
		t.Fatalf("next_pr_plan = %+v, want concrete churn action", top.NextPRPlan)
	}
}

func TestBuildPrioritizesRiskyNoTestWorkAheadOfGenericActions(t *testing.T) {
	report := baseAnalysis()
	report.WeaknessMap.Weaknesses = []signals.Finding{{
		Label:      "Behavior changes often lack test evidence",
		Evidence:   "3 source-changing commits did not touch test files.",
		Confidence: signals.ConfidenceMedium,
		NextAction: "For the next behavior-changing PR, add at least one adjacent test before review.",
	}, {
		Label:      "Risky paths need stronger proof",
		Evidence:   "2 security-sensitive commits had no adjacent test file changes.",
		Confidence: signals.ConfidenceMedium,
		NextAction: "Add targeted tests around security-sensitive edge cases before review.",
	}}
	report.DeepDives.NoTestArtifacts = []signals.NoTestArtifactDeepDive{{
		ChangedSourceFiles: []string{"apps/app/src/app/(public)/login/page.tsx"},
		Risk:               "Security-sensitive source files changed without test-file evidence.",
		NextAction:         "Add targeted tests around authorization, billing, session, token, or permission edge cases.",
		Confidence:         signals.ConfidenceMedium,
	}}

	top := Build(report)
	if !strings.Contains(top.Headline, "test evidence") {
		t.Fatalf("headline = %q, want test-evidence headline", top.Headline)
	}
	if len(top.Findings) == 0 || top.Findings[0].ID != "risky_no_test_work" {
		t.Fatalf("findings = %+v, want risky no-test work first", top.Findings)
	}
}

func TestBuildPrioritizesFailedChecksAndValidationGaps(t *testing.T) {
	report := baseAnalysis()
	report.WeaknessMap.Weaknesses = []signals.Finding{{
		Label:      "Checks failed on imported PRs",
		Evidence:   "3 imported PR(s) had failing or non-success check runs.",
		Confidence: signals.ConfidenceMedium,
		NextAction: "Run the same validation locally before review.",
	}}
	report.AgenticReadiness.Components = append(report.AgenticReadiness.Components, signals.ReadinessComponent{
		ID:         "validation_readiness",
		Label:      "Validation readiness",
		Score:      35,
		Confidence: signals.ConfidenceMedium,
		Evidence:   "No local validation command was detected.",
		NextAction: "Document one reliable local validation command agents can run.",
	})
	report.SourceCoverage.Sources = []signals.SourceCoverageItem{{
		ID:       "ai_spend_telemetry",
		Label:    "AI spend telemetry",
		Category: "spend",
		Status:   signals.SourceCoverageRequiresAdmin,
		Unlocks:  "Engineering ROI.",
	}}

	top := Build(report)
	if !strings.Contains(top.Headline, "checks") {
		t.Fatalf("headline = %q, want failed-check headline", top.Headline)
	}
	if len(top.Findings) < 2 {
		t.Fatalf("findings = %+v, want failed checks and validation gap", top.Findings)
	}
	if top.Findings[0].ID != "failed_checks" || top.Findings[1].ID != "missing_validation_command" {
		t.Fatalf("finding order = %+v, want failed checks then validation gap", top.Findings)
	}
	for _, finding := range top.Findings {
		if finding.ID == "ai_spend_telemetry" {
			t.Fatalf("future telemetry gap should not outrank readiness findings: %+v", top.Findings)
		}
	}
}

func TestBuildFallsBackToReadinessGrade(t *testing.T) {
	report := baseAnalysis()
	report.WeaknessMap.NextActions = []string{"Run contribution preflight before the next PR."}
	report.AgenticReadiness.TopActions = []string{"Keep instructions short and current."}

	top := Build(report)
	if !strings.Contains(top.Headline, "B-level agentic readiness") {
		t.Fatalf("headline = %q, want readiness fallback", top.Headline)
	}
	if len(top.NextPRPlan) == 0 || top.NextPRPlan[0] != "Run contribution preflight before the next PR." {
		t.Fatalf("next_pr_plan = %+v, want weakness-map fallback first", top.NextPRPlan)
	}
}

func TestBuildPrioritizesHighVolumeRepairLoopAheadOfSmallLargeWork(t *testing.T) {
	report := baseAnalysis()
	report.WeaknessMap.Weaknesses = []signals.Finding{{
		Label:      "Large changes create review risk",
		Evidence:   "1 recent commit changed more than 12 files.",
		Confidence: signals.ConfidenceMedium,
		NextAction: "Split broad refactors from behavior changes.",
	}}
	report.Trends.CurrentWindow.Commits = 20
	report.Trends.CurrentWindow.FixLikeCommits = 18

	top := Build(report)
	if len(top.Findings) == 0 || top.Findings[0].ID != "fix_like_repair_loop" {
		t.Fatalf("findings = %+v, want high-volume repair loop first", top.Findings)
	}
	if !strings.Contains(strings.ToLower(top.Headline), "repair loop") {
		t.Fatalf("headline = %q, want repair-loop headline", top.Headline)
	}
	if !strings.Contains(top.Findings[0].Evidence, "18 of 20") {
		t.Fatalf("evidence = %q, want magnitude-aware repair-loop evidence", top.Findings[0].Evidence)
	}
}

func TestBuildUsesSingularRepairLoopEvidence(t *testing.T) {
	report := baseAnalysis()
	report.Trends.CurrentWindow.Commits = 1
	report.Trends.CurrentWindow.FixLikeCommits = 1

	top := Build(report)
	if len(top.Findings) == 0 {
		t.Fatalf("findings = %+v, want repair-loop finding", top.Findings)
	}
	if strings.Contains(top.Findings[0].Evidence, "commit(s)") || !strings.Contains(top.Findings[0].Evidence, "1 recent commit matched") {
		t.Fatalf("evidence = %q, want singular grammar", top.Findings[0].Evidence)
	}
}

func baseAnalysis() signals.AnalysisReport {
	return signals.AnalysisReport{
		AgenticReadiness: signals.AgenticReadiness{
			Grade:      "B",
			Score:      82,
			Confidence: signals.ConfidenceMedium,
			Summary:    "Your repo is a B (82/100) for agentic readiness with medium confidence.",
			TopActions: []string{"Import coverage so test evidence is direct rather than inferred."},
		},
		WeaknessMap: signals.WeaknessMap{
			NextActions: []string{"Keep the next behavior-changing PR small enough to review in one pass."},
			Confidence:  signals.ConfidenceMedium,
		},
	}
}
