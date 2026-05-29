package report

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/contribution-dev/contribution/internal/signals"
)

func TestProfileExportIsPublicSafe(t *testing.T) {
	privateRelativePath := "internal/customer/acme/session.go"
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
	if containsInJSON(export, privateRelativePath) {
		t.Fatalf("profile export retained private path: %+v", export)
	}
}

func TestPublicSafeAnalysisRedactsPrivateMetadata(t *testing.T) {
	secret := "token=dogfood-secret-value"
	privateRoot := "/private/tmp/contribution-secret-repo"
	privateRelativePath := "internal/customer/acme/session.go"
	analysis := signals.AnalysisReport{
		Repo: signals.RepoMetadata{
			ID:          "owner/private-repo",
			Name:        "private-repo",
			Root:        privateRoot,
			RemoteURL:   "https://" + secret + "@github.com/owner/private-repo.git",
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
		}},
		PRCards: []signals.PRQualityCard{{
			PRNumber: 123,
			Title:    "Fix " + secret,
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
	if got.Repo.Root != "" || got.Repo.RemoteURL != "" || got.Repo.GitHubOwner != "" || got.Repo.GitHubRepo != "" {
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
	if got.PRCards[0].URL != "" || len(got.PRCards[0].Risks) != 0 || got.PRCards[0].MainRisk != "" || len(got.PRCards[0].Evidence) != 0 {
		t.Fatalf("PR card was not redacted: %+v", got.PRCards[0])
	}
	if containsText(got, "dogfood-secret-value") || containsText(got, privateRoot) || containsText(got, privateRelativePath) {
		t.Fatalf("public-safe analysis retained private text: %+v", got)
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
