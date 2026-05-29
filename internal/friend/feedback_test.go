package friend

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

	got := ApplyFeedback(analysis, feedback, time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC))

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
	err := ValidateFeedback(signals.FriendFeedbackExport{
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

func TestValidateFeedbackRejectsUnknownDuplicateAndBlankAnswers(t *testing.T) {
	base := signals.FriendFeedbackExport{
		Version:      1,
		PacketID:     "pkt-example",
		SubmittedAt:  time.Now(),
		OverallTrust: "medium",
		Confidence:   signals.ConfidenceMedium,
		PublicSafe:   true,
	}

	tests := []struct {
		name    string
		answers []signals.FriendFeedbackAnswer
	}{
		{name: "unknown", answers: []signals.FriendFeedbackAnswer{{QuestionID: "unknown", Answer: "This answer is specific enough."}}},
		{name: "duplicate", answers: []signals.FriendFeedbackAnswer{{QuestionID: "trust", Answer: "Looks fine."}, {QuestionID: "trust", Answer: "Still fine."}}},
		{name: "blank", answers: []signals.FriendFeedbackAnswer{{QuestionID: "trust", Answer: "   "}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			feedback := base
			feedback.Answers = tt.answers
			if err := ValidateFeedback(feedback); err == nil {
				t.Fatal("ValidateFeedback() error = nil, want validation error")
			}
		})
	}
}
