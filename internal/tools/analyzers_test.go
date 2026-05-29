package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/contribution-dev/contribution/internal/signals"
)

func TestParseAnalyzerFindings(t *testing.T) {
	semgrep := parseSemgrepFindings([]byte(`{"results":[{"check_id":"go.rule","path":"internal/app.go","extra":{"message":"avoid this","severity":"WARNING"}}]}`))
	if len(semgrep) != 1 || semgrep[0].Tool != "semgrep" || semgrep[0].Severity != signals.SeverityMedium || semgrep[0].FilePath != "internal/app.go" || semgrep[0].Scope != "repo_existing_or_unknown" {
		t.Fatalf("semgrep findings = %+v", semgrep)
	}

	gitleaks := parseGitleaksFindings([]byte(`[{"RuleID":"generic-api-key","Description":"redacted secret","File":"internal/config.go"}]`))
	if len(gitleaks) != 1 || gitleaks[0].Tool != "gitleaks" || gitleaks[0].Severity != signals.SeverityHigh {
		t.Fatalf("gitleaks findings = %+v", gitleaks)
	}

	osv := parseOSVFindings([]byte(`{"results":[{"source":{"path":"go.mod"},"packages":[{"package":{"name":"example"},"vulnerabilities":[{"id":"OSV-1","summary":"bad dep","severity":[{"type":"CVSS_V3","score":"HIGH"}]}]}]}]}`))
	if len(osv) != 1 || osv[0].Tool != "osv-scanner" || osv[0].RuleID != "OSV-1" || osv[0].Severity != signals.SeverityHigh {
		t.Fatalf("osv findings = %+v", osv)
	}

	trivy := parseTrivyFindings([]byte(`{"Results":[{"Target":"package-lock.json","Vulnerabilities":[{"VulnerabilityID":"CVE-1","Severity":"CRITICAL","Title":"bad vuln"}],"Misconfigurations":[{"ID":"MIS-1","Severity":"MEDIUM","Message":"bad config"}],"Secrets":[{"RuleID":"secret-1","Severity":"HIGH","Title":"secret"}]}]}`))
	if len(trivy) != 3 || trivy[0].Severity != signals.SeverityCritical || trivy[2].Tool != "trivy" {
		t.Fatalf("trivy findings = %+v", trivy)
	}
}

func TestRunAnalyzersUsesAvailableToolsAndSignals(t *testing.T) {
	bin := t.TempDir()
	writeFakeTool(t, bin, "semgrep", `{"results":[{"check_id":"go.rule","path":"internal/app.go","extra":{"message":"avoid this","severity":"ERROR"}}]}`, 0)
	t.Setenv("PATH", bin)

	tooling := signals.ToolingReport{Tools: []signals.ToolAvailability{{Name: "semgrep", Available: true}}}
	findings, signalsOut, limitations := RunAnalyzers(context.Background(), t.TempDir(), "repo", tooling, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	if len(limitations) != 0 {
		t.Fatalf("limitations = %+v", limitations)
	}
	if len(findings) != 1 || findings[0].RuleID != "go.rule" {
		t.Fatalf("findings = %+v", findings)
	}
	if len(signalsOut) != 2 || signalsOut[0].Type != "analyzer_findings_count" || signalsOut[1].Type != "analyzer_finding" {
		t.Fatalf("signals = %+v", signalsOut)
	}
	if !strings.Contains(signalsOut[1].Message, "internal/app.go") {
		t.Fatalf("signal message = %q", signalsOut[1].Message)
	}
}
