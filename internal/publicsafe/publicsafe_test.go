package publicsafe

import (
	"strings"
	"testing"

	"github.com/contribution-dev/contribution/internal/signals"
)

func TestAnalysisPreservesSlashSeparatedProductTerms(t *testing.T) {
	analysis := signals.AnalysisReport{
		Repo: signals.RepoMetadata{ID: "local:test", Name: "test"},
		SourceCoverage: signals.SourceCoverage{
			Sources: []signals.SourceCoverageItem{{
				Label:      "CI/test configuration",
				Evidence:   "No artifact token/cost metadata is available.",
				NextAction: "Run analyze/probe with --github-token gh.",
			}},
		},
		Privacy: signals.PrivacySummary{PublicSafe: true},
	}

	got := Analysis(analysis)
	if len(got.SourceCoverage.Sources) != 1 {
		t.Fatalf("sources = %+v, want one source", got.SourceCoverage.Sources)
	}
	source := got.SourceCoverage.Sources[0]
	for _, want := range []string{"CI/test", "token/cost", "analyze/probe"} {
		if !strings.Contains(source.Label+" "+source.Evidence+" "+source.NextAction, want) {
			t.Fatalf("public-safe source lost %q: %+v", want, source)
		}
	}
}

func TestAnalysisRedactsPathCandidatesInText(t *testing.T) {
	privateDir := "internal/customer"
	privatePath := "internal/customer/acme/session.go"
	analysis := signals.AnalysisReport{
		Repo: signals.RepoMetadata{ID: "local:test", Name: "test"},
		SourceCoverage: signals.SourceCoverage{
			Summary: "High churn includes " + privatePath + " and " + privateDir + ".",
		},
		Privacy: signals.PrivacySummary{PublicSafe: true},
	}

	got := Analysis(analysis)
	if strings.Contains(got.SourceCoverage.Summary, privatePath) || strings.Contains(got.SourceCoverage.Summary, privateDir) {
		t.Fatalf("summary retained private path: %q", got.SourceCoverage.Summary)
	}
	if !strings.Contains(got.SourceCoverage.Summary, "session.go") {
		t.Fatalf("summary did not keep redacted basename: %q", got.SourceCoverage.Summary)
	}
}
