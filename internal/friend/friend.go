// Package friend builds friend-review packets and imports friend feedback.
package friend

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/contribution-dev/contribution/internal/report"
	"github.com/contribution-dev/contribution/internal/signals"
)

var reviewRubric = []signals.ReviewRubricQuestion{
	{ID: "problem_fit", Prompt: "Does this change solve the intended problem cleanly?", Focus: "scope and correctness"},
	{ID: "maintainability", Prompt: "Is the implementation maintainable?", Focus: "boundaries, readability, and future changes"},
	{ID: "test_evidence", Prompt: "Are tests appropriate for the changed behavior?", Focus: "missing or weak verification"},
	{ID: "main_risk", Prompt: "What is the biggest risk?", Focus: "security, data, durability, or review burden"},
	{ID: "next_improvement", Prompt: "What should the author improve next?", Focus: "one concrete next action"},
	{ID: "trust", Prompt: "Would you trust this developer with similar work?", Focus: "overall trust signal"},
	{ID: "confidence", Prompt: "How confident are you in this feedback?", Focus: "low, medium, or high"},
}

// ReviewRubric returns the canonical packet and feedback rubric.
func ReviewRubric() []signals.ReviewRubricQuestion {
	return append([]signals.ReviewRubricQuestion{}, reviewRubric...)
}

// FindPRCard finds a quality card by PR number.
func FindPRCard(cards []signals.PRQualityCard, pr int) (signals.PRQualityCard, bool) {
	for _, card := range cards {
		if card.PRNumber == pr {
			return card, true
		}
	}
	return signals.PRQualityCard{}, false
}

// BuildPacket creates a V2 friend-review packet.
func BuildPacket(repo signals.RepoMetadata, card signals.PRQualityCard, publicSafe bool, now time.Time) signals.FriendReviewPacket {
	artifactLabel := card.Title
	if publicSafe {
		repo = report.PublicSafeRepo(repo)
		card = report.PublicSafeCard(card, 1)
		artifactLabel = card.Title
	}
	evidence := []string{
		card.Summary,
		"Label: " + card.Label,
		"Test evidence: " + card.TestEvidence,
		"Review burden: " + card.ReviewBurden,
		"Durability: " + card.Durability,
	}
	return signals.FriendReviewPacket{
		Version:       2,
		GeneratedAt:   now,
		PacketID:      PacketID(repo.ID, card.PRNumber, artifactLabel),
		Repo:          repo,
		PRNumber:      card.PRNumber,
		ArtifactLabel: artifactLabel,
		Context:       fmt.Sprintf("%s was flagged as %s confidence with a %s artifact label. The packet omits raw diffs by default.", artifactLabel, card.Confidence, card.Label),
		Card:          card,
		Evidence:      evidence,
		Rubric:        ReviewRubric(),
		Confidence:    card.Confidence,
		PublicSafe:    publicSafe,
	}
}

// PacketID returns the stable friend-review packet id for an artifact.
func PacketID(repoID string, pr int, label string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s", repoID, pr, label)))
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(h[:])
	return "pkt-" + strings.ToLower(encoded[:16])
}

// ReadFeedbackExports reads one feedback export file or every JSON file in a directory.
func ReadFeedbackExports(path string) ([]signals.FriendFeedbackExport, error) {
	files, err := feedbackFiles(path)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no feedback JSON files found in %s", path)
	}
	out := make([]signals.FriendFeedbackExport, 0, len(files))
	for _, file := range files {
		feedback, err := readFeedbackExport(file)
		if err != nil {
			return nil, err
		}
		out = append(out, feedback)
	}
	return out, nil
}

func feedbackFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read feedback path: %w", err)
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	var files []string
	if err := filepath.WalkDir(path, func(file string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".json") {
			files = append(files, file)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk feedback directory: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

func readFeedbackExport(path string) (signals.FriendFeedbackExport, error) {
	var feedback signals.FriendFeedbackExport
	// #nosec G304 -- feedback path is explicit CLI input.
	data, err := os.ReadFile(path)
	if err != nil {
		return feedback, fmt.Errorf("read feedback %s: %w", filepath.Base(path), err)
	}
	if err := json.Unmarshal(data, &feedback); err != nil {
		return feedback, fmt.Errorf("parse feedback %s: %w", filepath.Base(path), err)
	}
	if err := ValidateFeedback(feedback); err != nil {
		return feedback, fmt.Errorf("invalid feedback %s: %w", filepath.Base(path), err)
	}
	return feedback, nil
}

// ValidateFeedback validates the public-safe friend-feedback import contract.
func ValidateFeedback(feedback signals.FriendFeedbackExport) error {
	if feedback.Version != 1 {
		return fmt.Errorf("version %d is not supported", feedback.Version)
	}
	if strings.TrimSpace(feedback.PacketID) == "" {
		return fmt.Errorf("packet_id is required")
	}
	if feedback.SubmittedAt.IsZero() {
		return fmt.Errorf("submitted_at is required")
	}
	if !feedback.PublicSafe {
		return fmt.Errorf("public_safe must be true")
	}
	switch feedback.OverallTrust {
	case "low", "medium", "high":
	default:
		return fmt.Errorf("overall_trust must be low, medium, or high")
	}
	switch feedback.Confidence {
	case signals.ConfidenceLow, signals.ConfidenceMedium, signals.ConfidenceHigh:
	default:
		return fmt.Errorf("confidence must be low, medium, or high")
	}
	if len(feedback.Answers) == 0 {
		return fmt.Errorf("answers must not be empty")
	}
	known := reviewRubricIDs()
	seen := map[string]bool{}
	for _, answer := range feedback.Answers {
		id := strings.TrimSpace(answer.QuestionID)
		if id == "" {
			return fmt.Errorf("answer question_id is required")
		}
		if !known[id] {
			return fmt.Errorf("unknown question_id %q", id)
		}
		if seen[id] {
			return fmt.Errorf("duplicate question_id %q", id)
		}
		if strings.TrimSpace(answer.Answer) == "" {
			return fmt.Errorf("answer for %q must not be empty", id)
		}
		seen[id] = true
	}
	return nil
}

func reviewRubricIDs() map[string]bool {
	ids := make(map[string]bool, len(reviewRubric))
	for _, question := range reviewRubric {
		ids[question.ID] = true
	}
	return ids
}

// ApplyFeedback adds validated feedback signals and summary findings to an analysis.
func ApplyFeedback(analysis signals.AnalysisReport, feedback []signals.FriendFeedbackExport, now time.Time) signals.AnalysisReport {
	analysis.GeneratedAt = now
	for _, item := range feedback {
		analysis.Signals = append(analysis.Signals, feedbackSignals(analysis.Repo.ID, item, now)...)
	}
	analysis.Limitations = uniqueStrings(append(analysis.Limitations, fmt.Sprintf("Imported %d public-safe friend feedback export(s).", len(feedback))))
	analysis.WeaknessMap = updateWeaknessMapWithFeedback(analysis.WeaknessMap, feedback)
	analysis.Profile = updateProfileWithFeedback(analysis.Profile, feedback)
	return analysis
}

func feedbackSignals(repoID string, feedback signals.FriendFeedbackExport, now time.Time) []signals.Signal {
	score := feedbackUsefulness(feedback)
	direction := signals.DirectionNeutral
	severity := signals.SeverityInfo
	switch feedback.OverallTrust {
	case "high":
		direction = signals.DirectionPositive
	case "low":
		direction = signals.DirectionNegative
		severity = signals.SeverityMedium
	}
	return []signals.Signal{
		signals.New(repoID, "friend_feedback", "friend_feedback_trust", "packet", feedback.PacketID, severity, direction, feedback.Confidence, trustValue(feedback.OverallTrust), "score", fmt.Sprintf("Friend feedback reported %s overall trust with %s confidence.", feedback.OverallTrust, feedback.Confidence), true, now),
		signals.New(repoID, "friend_feedback", "friend_feedback_usefulness", "packet", feedback.PacketID, signals.SeverityInfo, signals.DirectionNeutral, signals.ConfidenceMedium, score, "score", fmt.Sprintf("Friend feedback usefulness scored %.0f/100 from specificity and completeness.", score), true, now),
	}
}

func updateWeaknessMapWithFeedback(value signals.WeaknessMap, feedback []signals.FriendFeedbackExport) signals.WeaknessMap {
	avg := averageFeedbackUsefulness(feedback)
	value.WatchItems = append(value.WatchItems, signals.Finding{
		Label:        "Friend feedback imported",
		Evidence:     fmt.Sprintf("%d public-safe feedback export(s) averaged %.0f/100 usefulness from specificity and completeness.", len(feedback), avg),
		Confidence:   signals.ConfidenceMedium,
		WhyItMatters: "External review signals are useful when they are specific and complete, not because of reviewer identity.",
		NextAction:   "Use specific feedback items to tighten the next preflight before review.",
	})
	for _, item := range feedback {
		switch item.OverallTrust {
		case "high":
			value.Strengths = append(value.Strengths, signals.Finding{
				Label:      "Friend feedback supports review readiness",
				Evidence:   "Imported friend feedback reported high overall trust.",
				Confidence: item.Confidence,
			})
		case "low":
			value.Weaknesses = append(value.Weaknesses, signals.Finding{
				Label:        "Friend feedback raised review concerns",
				Evidence:     "Imported friend feedback reported low overall trust.",
				Confidence:   item.Confidence,
				WhyItMatters: "Low trust feedback before PR submission is a chance to reduce review risk.",
				NextAction:   "Address the feedback before asking for another review.",
			})
			value.NextActions = append([]string{"Address low-trust friend feedback before asking for another review."}, value.NextActions...)
		}
	}
	value.NextActions = firstStrings(uniqueStrings(value.NextActions), 3)
	return value
}

func updateProfileWithFeedback(profile signals.ProfileSummary, feedback []signals.FriendFeedbackExport) signals.ProfileSummary {
	for _, item := range feedback {
		if item.OverallTrust == "high" {
			profile.Strengths = append(profile.Strengths, signals.Finding{
				Label:      "Positive friend feedback",
				Evidence:   "A public-safe feedback export reported high overall trust.",
				Confidence: item.Confidence,
			})
		}
	}
	profile.ImprovementTrends = append(profile.ImprovementTrends, signals.Finding{
		Label:      "Friend feedback loop started",
		Evidence:   fmt.Sprintf("%d public-safe feedback export(s) were imported.", len(feedback)),
		Confidence: signals.ConfidenceMedium,
	})
	return profile
}

func feedbackUsefulness(feedback signals.FriendFeedbackExport) float64 {
	completeness := float64(answeredCount(feedback.Answers)) / float64(len(reviewRubric)) * 60
	if completeness > 60 {
		completeness = 60
	}
	var specific int
	for _, answer := range feedback.Answers {
		if wordCount(answer.Answer) >= 8 {
			specific++
		}
	}
	specificity := float64(specific) / float64(max(1, len(feedback.Answers))) * 40
	return completeness + specificity
}

func averageFeedbackUsefulness(feedback []signals.FriendFeedbackExport) float64 {
	if len(feedback) == 0 {
		return 0
	}
	var total float64
	for _, item := range feedback {
		total += feedbackUsefulness(item)
	}
	return total / float64(len(feedback))
}

func answeredCount(answers []signals.FriendFeedbackAnswer) int {
	var total int
	for _, answer := range answers {
		if strings.TrimSpace(answer.Answer) != "" {
			total++
		}
	}
	return total
}

func wordCount(value string) int {
	return len(strings.Fields(value))
}

func trustValue(value string) float64 {
	switch value {
	case "high":
		return 1
	case "medium":
		return 0.5
	default:
		return 0
	}
}

func firstStrings(values []string, limit int) []string {
	if len(values) < limit {
		limit = len(values)
	}
	return append([]string{}, values[:limit]...)
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
