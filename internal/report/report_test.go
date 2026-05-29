package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/contribution-dev/contribution/internal/signals"
)

func TestProfileExportIsPublicSafe(t *testing.T) {
	privateRelativePath := "internal/customer/acme/session.go"
	commitSHA := "abcdef1234567890abcdef1234567890abcdef12"
	analysis := signals.AnalysisReport{
		GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Profile: signals.ProfileSummary{
			Headline:           "AI-native contribution profile",
			AnalyzedPRs:        1,
			AnalysisWindowDays: 90,
			Confidence:         signals.ConfidenceMedium,
			Strengths: []signals.Finding{{
				Label:      "Focused local changes",
				Evidence:   "High-churn files include " + privateRelativePath + ".",
				Confidence: signals.ConfidenceMedium,
			}},
		},
		Signals: []signals.Signal{{
			SubjectType: "file",
			SubjectID:   privateRelativePath,
			FilePath:    privateRelativePath,
		}, {
			SubjectType: "commit",
			SubjectID:   commitSHA,
			Message:     "Commit " + commitSHA[:8] + " touched tests",
		}},
		PRCards: []signals.PRQualityCard{{
			PRNumber:   123,
			Title:      "Sensitive PR for " + privateRelativePath,
			URL:        "https://example.test/private",
			Label:      "mixed",
			Confidence: signals.ConfidenceLow,
			Summary:    "Changed " + privateRelativePath,
			Risks: []signals.Finding{{
				Label: "Private risk",
			}},
			MainRisk:   "private path risk",
			NextAction: "private action",
		}},
	}
	export := ProfileExport(analysis)
	if !export.Redaction.PublicSafe {
		t.Fatal("PublicSafe = false, want true")
	}
	if export.Redaction.RawCodeIncluded || export.Redaction.RawDiffsIncluded || export.Redaction.PrivatePathsIncluded {
		t.Fatalf("redaction flags are unsafe: %+v", export.Redaction)
	}
	if got := export.SelectedArtifacts[0]; got.URL != "" || len(got.Risks) != 0 || got.MainRisk != "" || got.NextAction != "" {
		t.Fatalf("selected artifact was not redacted: %+v", got)
	}
	if export.SelectedArtifacts[0].Title != "PR #123" {
		t.Fatalf("selected artifact title = %q, want neutral PR label", export.SelectedArtifacts[0].Title)
	}
	if containsInJSON(export, privateRelativePath) {
		t.Fatalf("profile export retained private path: %+v", export)
	}
	if containsInJSON(export, commitSHA) || containsInJSON(export, commitSHA[:8]) {
		t.Fatalf("profile export retained commit SHA: %+v", export)
	}
}

func TestPublicSafeAnalysisRedactsPrivateMetadata(t *testing.T) {
	secret := "token=dogfood-secret-value"
	privateRoot := "/private/tmp/contribution-secret-repo"
	privateRelativePath := "internal/customer/acme/session.go"
	commitSHA := "abcdef1234567890abcdef1234567890abcdef12"
	commitTitle := "Fix customer " + privateRelativePath
	analysis := signals.AnalysisReport{
		Repo: signals.RepoMetadata{
			ID:          "owner/private-repo",
			Name:        "private-repo",
			Root:        privateRoot,
			RemoteURL:   "https://" + secret + "@github.com/owner/private-repo.git",
			HeadSHA:     commitSHA,
			GitHubOwner: "owner",
			GitHubRepo:  "private-repo",
		},
		Config: signals.AnalysisConfigSnapshot{
			OutputDirectory:          privateRoot + "/.contribution/reports/run",
			GitHubMetadataConfigured: true,
			SelfReportedAITools:      []string{"tool " + secret},
			SelfReportedAIModes:      []string{"Authorization: Bearer dogfood-secret-value"},
		},
		Tooling: signals.ToolingReport{
			Tools:       []signals.ToolAvailability{{Name: "example", Reason: "failed with " + secret}},
			Limitations: []string{"tool failed with " + secret},
		},
		Signals: []signals.Signal{{
			RepoID:     "owner/private-repo",
			SubjectID:  "owner/private-repo",
			FilePath:   privateRoot + "/internal/auth/session.go",
			Message:    "failed with " + secret,
			PublicSafe: true,
			CreatedAt:  time.Now(),
		}, {
			RepoID:      "owner/private-repo",
			SubjectType: "file",
			SubjectID:   privateRelativePath,
			FilePath:    privateRelativePath,
			Message:     privateRelativePath + " changed 4 times",
			PublicSafe:  false,
			CreatedAt:   time.Now(),
		}, {
			RepoID:      "owner/private-repo",
			SubjectType: "commit",
			SubjectID:   commitSHA,
			Message:     "Commit " + commitSHA[:8] + " changed 10 lines",
			PublicSafe:  true,
			CreatedAt:   time.Now(),
		}},
		PRCards: []signals.PRQualityCard{{
			PRNumber: 123,
			Title:    commitTitle,
			URL:      "https://github.com/owner/private-repo/pull/123",
			Risks:    []signals.Finding{{Label: "private"}},
			Evidence: []signals.SignalRef{{ID: "private"}},
			MainRisk: "private risk",
		}},
		WeaknessMap: signals.WeaknessMap{
			Weaknesses:  []signals.Finding{{Label: "Secret", Evidence: "High-churn files include " + privateRelativePath + " with " + secret, WhyItMatters: secret, NextAction: secret}},
			NextActions: []string{"rotate " + secret},
		},
		Profile: signals.ProfileSummary{
			Headline:           "AI-native contribution profile",
			AnalyzedPRs:        1,
			AnalysisWindowDays: 90,
			Confidence:         signals.ConfidenceMedium,
			Strengths:          []signals.Finding{{Label: "Secret", Evidence: secret}},
		},
		Limitations: []string{"limitation " + secret},
		Privacy: signals.PrivacySummary{
			RawCodeIncluded:                    true,
			RawDiffsIncluded:                   true,
			PrivatePathsIncludedInPublicExport: true,
			AuthorEmailsIncluded:               true,
			UploadEnabled:                      true,
		},
	}

	got := PublicSafeAnalysis(analysis)
	if got.Repo.Root != "" || got.Repo.RemoteURL != "" || got.Repo.HeadSHA != "" || got.Repo.GitHubOwner != "" || got.Repo.GitHubRepo != "" {
		t.Fatalf("repo metadata was not redacted: %+v", got.Repo)
	}
	if got.Config.OutputDirectory != "" || !got.Config.PublicSafe || got.Config.GitHubMetadataConfigured {
		t.Fatalf("config was not public-safe: %+v", got.Config)
	}
	if len(got.Config.SelfReportedAITools) != 0 || len(got.Config.SelfReportedAIModes) != 0 {
		t.Fatalf("self-reported AI config was not cleared: %+v", got.Config)
	}
	if !got.Privacy.PublicSafe || got.Privacy.RawCodeIncluded || got.Privacy.RawDiffsIncluded || got.Privacy.UploadEnabled {
		t.Fatalf("privacy flags were not public-safe: %+v", got.Privacy)
	}
	if got.Signals[0].RepoID != "private-repository" || got.Signals[0].SubjectID != "private-repository" || got.Signals[0].FilePath != "session.go" {
		t.Fatalf("signal metadata was not redacted: %+v", got.Signals[0])
	}
	if got.Signals[1].SubjectID != "session.go" || got.Signals[1].FilePath != "session.go" {
		t.Fatalf("repo-relative signal path was not redacted: %+v", got.Signals[1])
	}
	if got.Signals[2].SubjectID != "" {
		t.Fatalf("commit signal metadata was not redacted: %+v", got.Signals[2])
	}
	if got.PRCards[0].Title != "PR #123" || got.PRCards[0].URL != "" || len(got.PRCards[0].Risks) != 0 || got.PRCards[0].MainRisk != "" || len(got.PRCards[0].Evidence) != 0 {
		t.Fatalf("PR card was not redacted: %+v", got.PRCards[0])
	}
	if containsText(got, "dogfood-secret-value") || containsText(got, privateRoot) || containsText(got, privateRelativePath) || containsText(got, commitSHA) || containsText(got, commitSHA[:8]) || containsText(got, commitTitle) {
		t.Fatalf("public-safe analysis retained private text: %+v", got)
	}
}

func TestPublicSafeAnalysisRedactsPathsAndEmailsOutsideSignals(t *testing.T) {
	privatePath := "internal/customer/acme/session.go"
	email := "builder@example.com"
	analysis := signals.AnalysisReport{
		Repo: signals.RepoMetadata{ID: "owner/private", Name: "private"},
		PRCards: []signals.PRQualityCard{{
			Title:        "Fix " + privatePath,
			Label:        "mixed",
			Confidence:   signals.ConfidenceMedium,
			Summary:      "Changed " + privatePath + " after review from " + email,
			MainRisk:     "Risk remains in " + privatePath,
			NextAction:   "Ask " + email + " to review " + privatePath,
			TestEvidence: "No tests for " + privatePath,
		}},
		WeaknessMap: signals.WeaknessMap{
			Weaknesses: []signals.Finding{{
				Label:      "Path-only finding",
				Evidence:   privatePath + " was changed by " + email,
				Confidence: signals.ConfidenceMedium,
			}},
			Confidence: signals.ConfidenceMedium,
		},
		Profile: signals.ProfileSummary{
			Headline:           "Work in " + privatePath,
			AnalyzedPRs:        1,
			AnalysisWindowDays: 90,
			Confidence:         signals.ConfidenceMedium,
			Strengths:          []signals.Finding{{Label: "Reviewed", Evidence: email + " reviewed " + privatePath, Confidence: signals.ConfidenceMedium}},
		},
		Limitations: []string{"Contact " + email + " about " + privatePath},
	}

	got := PublicSafeAnalysis(analysis)
	for _, forbidden := range []string{privatePath, email} {
		if containsText(got, forbidden) {
			t.Fatalf("public-safe analysis retained %q: %+v", forbidden, got)
		}
	}
	if !containsText(got, "session.go") {
		t.Fatalf("public-safe analysis did not preserve neutral basename: %+v", got)
	}
}

func TestPublicSafeReportArtifactsDoNotRetainPrivateIdentifiers(t *testing.T) {
	privateRoot := "/private/tmp/customer-repo"
	privateRelativePath := "internal/customer/acme/session.go"
	commitSHA := "abcdef1234567890abcdef1234567890abcdef12"
	analysis := PublicSafeAnalysis(signals.AnalysisReport{
		GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Repo: signals.RepoMetadata{
			ID:        "owner/private",
			Name:      "private",
			Root:      privateRoot,
			RemoteURL: "https://token=dogfood-secret-value@example.test/private.git",
			HeadSHA:   commitSHA,
		},
		Config: signals.AnalysisConfigSnapshot{OutputDirectory: privateRoot + "/reports"},
		Signals: []signals.Signal{{
			RepoID:      "owner/private",
			SubjectType: "commit",
			SubjectID:   commitSHA,
			Message:     "Commit " + commitSHA[:8] + " changed " + privateRelativePath,
			FilePath:    privateRelativePath,
		}},
		PRCards: []signals.PRQualityCard{{
			Title:      "Private commit " + commitSHA[:8],
			Label:      "mixed",
			Confidence: signals.ConfidenceMedium,
			Summary:    "Commit " + commitSHA[:8] + " changed " + privateRelativePath,
			Scope:      "1 file and 10 lines",
			MainRisk:   "private risk",
			Evidence:   []signals.SignalRef{{ID: commitSHA, Message: privateRelativePath}},
		}},
		WeaknessMap: signals.WeaknessMap{
			Strengths:  []signals.Finding{{Label: "Private", Evidence: "Commit " + commitSHA[:8] + " touched " + privateRelativePath, Confidence: signals.ConfidenceMedium}},
			Confidence: signals.ConfidenceMedium,
		},
		Profile: signals.ProfileSummary{
			Headline:           "Private " + privateRelativePath,
			AnalyzedPRs:        1,
			AnalysisWindowDays: 90,
			Confidence:         signals.ConfidenceMedium,
			Strengths:          []signals.Finding{{Label: "Private", Evidence: "Commit " + commitSHA[:8] + " touched " + privateRelativePath, Confidence: signals.ConfidenceMedium}},
		},
		Privacy: signals.PrivacySummary{PublicSafe: true},
	})
	output := t.TempDir()
	if err := WriteReportOnly(output, analysis, "all"); err != nil {
		t.Fatalf("WriteReportOnly() error = %v", err)
	}
	for _, name := range []string{"analysis.json", "report.md", "profile.export.json", "share-card.json"} {
		// #nosec G304 -- test reads a fixed allow-list of files from t.TempDir output.
		data, err := os.ReadFile(filepath.Join(output, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(data)
		if name == "report.md" {
			assertLedgerHasNoEmptyRiskActions(t, text)
			if strings.Contains(text, "V1") {
				t.Fatalf("%s retained phase-stale V1 wording:\n%s", name, text)
			}
		}
		for _, forbidden := range []string{privateRoot, privateRelativePath, commitSHA, commitSHA[:8], "dogfood-secret-value", "Private commit"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s retained %q:\n%s", name, forbidden, text)
			}
		}
	}
}

func assertLedgerHasNoEmptyRiskActions(t *testing.T, text string) {
	t.Helper()
	checked := 0
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| PR ") || strings.HasPrefix(line, "| ---") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 10 {
			continue
		}
		checked++
		if strings.TrimSpace(cells[8]) == "" {
			t.Fatalf("ledger row has empty main risk cell: %s", line)
		}
		if strings.TrimSpace(cells[9]) == "" {
			t.Fatalf("ledger row has empty next action cell: %s", line)
		}
	}
	if checked == 0 {
		t.Fatalf("no ledger rows found in report:\n%s", text)
	}
}

func containsText(analysis signals.AnalysisReport, needle string) bool {
	export := ProfileExport(analysis)
	return containsInJSON(analysis, needle) || containsInJSON(export, needle)
}

func containsInJSON(value any, needle string) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), needle)
}
