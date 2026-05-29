package friend

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/contribution-dev/contribution/internal/signals"
)

func TestFriendReviewPacketJSONContract(t *testing.T) {
	packet := BuildPacket(
		signals.RepoMetadata{ID: "owner/private", Name: "private"},
		signals.PRQualityCard{
			PRNumber:     123,
			Title:        "Improve contract tests",
			Label:        "strong",
			Confidence:   signals.ConfidenceMedium,
			Summary:      "Small tested change.",
			TestEvidence: "Go tests cover the behavior.",
			ReviewBurden: "Low",
			Durability:   "Durable",
		},
		true,
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	)
	object := marshalFriendContractObject(t, packet)
	assertFriendContractKeys(t, object, []string{
		"version",
		"generated_at",
		"packet_id",
		"repo",
		"pr_number",
		"artifact_label",
		"context",
		"card",
		"evidence",
		"rubric",
		"confidence",
		"public_safe",
	})
}

func TestFriendFeedbackExportJSONContract(t *testing.T) {
	feedback := signals.FriendFeedbackExport{
		Version:       1,
		PacketID:      "pkt-example",
		SubmittedAt:   time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		ReviewerLabel: "reviewer",
		OverallTrust:  "high",
		Confidence:    signals.ConfidenceMedium,
		Answers: []signals.FriendFeedbackAnswer{{
			QuestionID: "trust",
			Question:   "Would you trust this developer with similar work?",
			Answer:     "Yes, the change is narrow and tested.",
		}},
		PublicSafe: true,
	}
	object := marshalFriendContractObject(t, feedback)
	assertFriendContractKeys(t, object, []string{
		"version",
		"packet_id",
		"submitted_at",
		"reviewer_label",
		"overall_trust",
		"confidence",
		"answers",
		"public_safe",
	})
}

func marshalFriendContractObject(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return object
}

func assertFriendContractKeys(t *testing.T, object map[string]any, want []string) {
	t.Helper()
	got := friendContractSortedKeys(object)
	want = append([]string{}, want...)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON keys = %v, want %v", got, want)
	}
}

func friendContractSortedKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
