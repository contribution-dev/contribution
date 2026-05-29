package cli

import (
	"testing"
	"time"

	"github.com/contribution-dev/contribution/internal/signals"
)

func TestApplyFeedbackAddsSignalsAndFindings(t *testing.T) {
	analysis := signals.AnalysisReport{
		Repo:        signals.RepoMetadata{ID: "local:test"},
		GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Profile:     signals.ProfileSummary{Confidence: signals.ConfidenceMedium},
	}
	feedback := []signals.FriendFeedbackExport{{
		Version:      1,
		PacketID:     "pkt-example",
		SubmittedAt:  time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		OverallTrust: "high",
		Confidence:   signals.ConfidenceMedium,
		Answers: []signals.FriendFeedbackAnswer{
			{QuestionID: "problem_fit", Answer: "This solves the stated problem with a narrow enough implementation."},
			{QuestionID: "test_evidence", Answer: "The regression test covers the changed behavior well enough."},
		},
		PublicSafe: true,
	}}

	got := applyFeedback(analysis, feedback, time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC))

	if len(got.Signals) != 2 {
		t.Fatalf("signals = %d, want 2", len(got.Signals))
	}
	if got.Signals[0].Source != "friend_feedback" {
		t.Fatalf("signal source = %q, want friend_feedback", got.Signals[0].Source)
	}
	if len(got.WeaknessMap.Strengths) == 0 {
		t.Fatalf("strengths missing feedback finding: %+v", got.WeaknessMap)
	}
	if len(got.Profile.ImprovementTrends) == 0 {
		t.Fatalf("profile trends missing feedback trend: %+v", got.Profile)
	}
}

func TestValidateFeedbackRejectsUnsafeExport(t *testing.T) {
	err := validateFeedback(signals.FriendFeedbackExport{
		Version:      1,
		PacketID:     "pkt-example",
		SubmittedAt:  time.Now(),
		OverallTrust: "medium",
		Confidence:   signals.ConfidenceMedium,
		Answers:      []signals.FriendFeedbackAnswer{{QuestionID: "trust", Answer: "Looks fine."}},
		PublicSafe:   false,
	})
	if err == nil {
		t.Fatal("validateFeedback() error = nil, want public_safe rejection")
	}
}
