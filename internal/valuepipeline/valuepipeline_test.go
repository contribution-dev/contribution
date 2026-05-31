package valuepipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/contribution-dev/contribution/internal/config"
	gitrepo "github.com/contribution-dev/contribution/internal/git"
	"github.com/contribution-dev/contribution/internal/github"
	"github.com/contribution-dev/contribution/internal/signals"
)

func TestBuildScoresReadyRepoAndSourceCoverage(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "# agent guide\n")
	writeFile(t, filepath.Join(root, "README.md"), "# app\n")
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test/app\n")
	input := Input{
		GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Repo:        gitrepo.Repo{Path: root, Name: "app", ID: "local:app"},
		Inventory: signals.FileSummary{
			TotalFiles:  4,
			SourceFiles: 1,
			TestFiles:   1,
			DocsFiles:   1,
			ConfigFiles: 1,
		},
		History: gitrepo.History{
			Commits:        []gitrepo.Commit{{SHA: "abcdef1234567890", Subject: "ENG-123 add feature", SourceTouched: true, TestsTouched: true}},
			FileTouchCount: map[string]int{"app.go": 1},
		},
		GitHub: github.Metadata{
			Available: true,
			PRs: []github.PullRequest{{
				Number:         12,
				Title:          "ENG-123 add feature",
				ChangedFiles:   2,
				ReviewCount:    1,
				CheckRuns:      1,
				MergeCommitSHA: "abcdef1234567890",
			}},
		},
		Coverage:             signals.CoverageSummary{Status: "available", Percent: 85},
		Tooling:              signals.ToolingReport{Tools: []signals.ToolAvailability{{Name: "semgrep", Available: true}}},
		GitHubTokenAvailable: true,
		ExternalToolsAllowed: true,
	}

	out := Build(input)
	if out.AgenticReadiness.Score < 75 || out.AgenticReadiness.Grade == "" {
		t.Fatalf("readiness = %+v, want strong score", out.AgenticReadiness)
	}
	if out.AttributionReadiness.Pattern != "issue-per-pr" || out.AttributionReadiness.Confidence != signals.ConfidenceHigh {
		t.Fatalf("attribution = %+v, want high-confidence issue-per-pr", out.AttributionReadiness)
	}
	if len(out.SourceCoverage.Sources) == 0 || len(out.DataGaps) == 0 || len(out.RecommendedConnections) == 0 {
		t.Fatalf("coverage/gaps/recommendations missing: %+v gaps=%+v recs=%+v", out.SourceCoverage, out.DataGaps, out.RecommendedConnections)
	}
}

func TestInspectAgentArtifactsRequiresExplicitOptInAndStoresMetadataOnly(t *testing.T) {
	root := t.TempDir()
	artifact := filepath.Join(root, "artifact.json")
	writeFile(t, artifact, `{
  "provider": "openai",
  "session_id": "secret-session-id",
  "total_tokens": 1234,
  "cost_usd": 0.42,
  "prompt": "do not store this",
  "branch": "feature/roi"
}`)

	if _, _, err := InspectAgentArtifacts([]string{artifact}, false, root); err == nil {
		t.Fatal("InspectAgentArtifacts() error = nil, want explicit opt-in error")
	}
	got, limitations, err := InspectAgentArtifacts([]string{artifact}, true, root)
	if err != nil {
		t.Fatalf("InspectAgentArtifacts() error = %v", err)
	}
	if len(limitations) != 0 {
		t.Fatalf("limitations = %+v, want none", limitations)
	}
	if len(got) != 1 || got[0].Status != "available" || got[0].TokenCount != 1234 || got[0].CostUSD != 0.42 {
		t.Fatalf("artifact metadata = %+v", got)
	}
	if got[0].SessionFingerprint == "" || got[0].SessionFingerprint == "secret-session-id" {
		t.Fatalf("session fingerprint not hashed: %+v", got[0])
	}
}

func writeFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestBuildUsesDefaultConfigWhenMissing(t *testing.T) {
	root := t.TempDir()
	out := Build(Input{
		GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Repo:        gitrepo.Repo{Path: root, Name: "app", ID: "local:app"},
		Config:      config.Default(),
		Inventory:   signals.FileSummary{TotalFiles: 1},
		History:     gitrepo.History{FileTouchCount: map[string]int{}},
		Coverage:    signals.CoverageSummary{Status: "unknown", Reason: "No coverage report was imported."},
	})
	if out.AgenticReadiness.Grade == "" {
		t.Fatalf("readiness missing with default config: %+v", out.AgenticReadiness)
	}
	if out.AttributionReadiness.Pattern != "unknown" {
		t.Fatalf("pattern = %q, want unknown", out.AttributionReadiness.Pattern)
	}
}

func TestBuildSkipsEmptyWorkUnitAnchors(t *testing.T) {
	root := t.TempDir()
	out := Build(Input{
		GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Repo:        gitrepo.Repo{Path: root, Name: "app", ID: "local:app"},
		Config:      config.Default(),
		Inventory:   signals.FileSummary{TotalFiles: 1},
		History:     gitrepo.History{FileTouchCount: map[string]int{}},
		Coverage:    signals.CoverageSummary{Status: "unknown", Reason: "No coverage report was imported."},
		WorkUnitMarkers: []signals.WorkUnitMarker{{
			ID:   "awu-1",
			Goal: "Build onboarding",
		}},
	})
	if out.AttributionReadiness.Pattern != "manual_marker" {
		t.Fatalf("pattern = %q, want manual_marker", out.AttributionReadiness.Pattern)
	}
	if len(out.WorkUnitCandidates) != 1 {
		t.Fatalf("candidates = %+v, want one candidate", out.WorkUnitCandidates)
	}
	candidate := out.WorkUnitCandidates[0]
	if len(candidate.Anchors) != 1 || candidate.Anchors[0].Type != "manual_marker" {
		t.Fatalf("anchors = %+v, want only manual marker anchor", candidate.Anchors)
	}
}

func TestBuildDoesNotInventInstructionCommand(t *testing.T) {
	root := t.TempDir()
	out := Build(Input{
		GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Repo:        gitrepo.Repo{Path: root, Name: "app", ID: "local:app"},
		Config:      config.Default(),
		Inventory:   signals.FileSummary{TotalFiles: 1},
		History:     gitrepo.History{FileTouchCount: map[string]int{}},
		Coverage:    signals.CoverageSummary{Status: "unknown", Reason: "No coverage report was imported."},
	})
	for _, connection := range out.RecommendedConnections {
		if connection.ID != "repo_instructions" {
			continue
		}
		if connection.Command != "" {
			t.Fatalf("repo instruction command = %q, want empty manual action", connection.Command)
		}
		if !strings.Contains(connection.Label, "AGENTS.md") {
			t.Fatalf("repo instruction label = %q, want AGENTS.md action", connection.Label)
		}
		return
	}
	t.Fatalf("repo instruction recommendation missing: %+v", out.RecommendedConnections)
}
