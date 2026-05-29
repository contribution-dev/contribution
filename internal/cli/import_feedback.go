package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/contribution-dev/contribution/internal/report"
	"github.com/contribution-dev/contribution/internal/signals"
	"github.com/spf13/cobra"
)

func newImportFeedbackCommand(out io.Writer) *cobra.Command {
	var analysisPath string
	var feedbackPath string
	var output string
	var format string
	var publicSafe bool
	cmd := &cobra.Command{
		Use:   "import-feedback",
		Short: "Import public-safe friend feedback into an analysis bundle.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if analysisPath == "" {
				return fmt.Errorf("--analysis is required")
			}
			if feedbackPath == "" {
				return fmt.Errorf("--feedback is required")
			}
			if output == "" {
				return fmt.Errorf("--output is required")
			}
			if err := validateFormat(format, true); err != nil {
				return err
			}
			analysis, err := report.ReadAnalysis(analysisPath)
			if err != nil {
				return err
			}
			feedback, err := readFeedbackExports(feedbackPath)
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			analysis = applyFeedback(analysis, feedback, now)
			if publicSafe {
				analysis = report.PublicSafeAnalysis(analysis)
			}
			if err := report.WriteAnalysisBundle(output, analysis, format); err != nil {
				return err
			}
			return writef(out, "Feedback import written to %s\n", output)
		},
	}
	cmd.Flags().StringVar(&analysisPath, "analysis", "", "Input analysis.json.")
	cmd.Flags().StringVar(&feedbackPath, "feedback", "", "Feedback JSON file or directory.")
	cmd.Flags().StringVar(&output, "output", "", "Output directory.")
	cmd.Flags().StringVar(&format, "format", "all", "Output format: json, markdown, or all.")
	cmd.Flags().BoolVar(&publicSafe, "public-safe", false, "Redact local repo metadata from imported analysis output.")
	return cmd
}

func readFeedbackExports(path string) ([]signals.FriendFeedbackExport, error) {
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
	if err := validateFeedback(feedback); err != nil {
		return feedback, fmt.Errorf("invalid feedback %s: %w", filepath.Base(path), err)
	}
	return feedback, nil
}

func validateFeedback(feedback signals.FriendFeedbackExport) error {
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
	return nil
}

func applyFeedback(analysis signals.AnalysisReport, feedback []signals.FriendFeedbackExport, now time.Time) signals.AnalysisReport {
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
	value.NextActions = firstFeedbackStrings(uniqueStrings(value.NextActions), 3)
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
	completeness := float64(answeredCount(feedback.Answers)) / 7 * 60
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

func firstFeedbackStrings(values []string, limit int) []string {
	if len(values) < limit {
		limit = len(values)
	}
	return append([]string{}, values[:limit]...)
}
