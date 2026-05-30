package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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

func TestGitleaksWorktreeScanUsesGitVisibleFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repoPath := t.TempDir()
	runGitForAnalyzerTest(t, repoPath, "init", "-b", "main")
	writeAnalyzerTestFile(t, repoPath, ".gitignore", "ignored.txt\n")
	writeAnalyzerTestFile(t, repoPath, "internal/secret.txt", "api_key = \"synthetic-secret\"\n")
	writeAnalyzerTestFile(t, repoPath, "ignored.txt", "ignored-secret\n")
	writeAnalyzerTestFile(t, repoPath, "dist/generated.txt", "generated-secret\n")
	writeAnalyzerTestFile(t, repoPath, ".tools/cache.txt", "tool-secret\n")

	bin := t.TempDir()
	writeFakeAnalyzerScript(t, bin, "gitleaks", `#!/bin/sh
if [ "$1" = "dir" ]; then
  if [ -e "ignored.txt" ] || [ -e "dist/generated.txt" ] || [ -e ".tools/cache.txt" ]; then
    printf '%s\n' '[{"RuleID":"unexpected-path","Description":"unexpected copied path","File":"ignored.txt"}]'
    exit 0
  fi
  if [ -f "internal/secret.txt" ]; then
    printf '%s\n' '[{"RuleID":"generic-api-key","Description":"redacted worktree secret","File":"internal/secret.txt"}]'
    exit 0
  fi
fi
printf '%s\n' '[]'
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	tooling := signals.ToolingReport{Tools: []signals.ToolAvailability{{Name: "gitleaks", Available: true}}}
	findings, _, limitations := RunAnalyzers(context.Background(), repoPath, "repo", tooling, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want one worktree finding; limitations=%+v", findings, limitations)
	}
	if findings[0].RuleID != "generic-api-key" || findings[0].FilePath != "internal/secret.txt" {
		t.Fatalf("finding = %+v, want untracked Git-visible secret", findings[0])
	}
}

func runGitForAnalyzerTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	// #nosec G204 -- test helper invokes git with fixed test-controlled arguments.
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
}

func writeAnalyzerTestFile(t *testing.T, root string, rel string, content string) {
	t.Helper()
	target := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func writeFakeAnalyzerScript(t *testing.T, bin string, name string, body string) {
	t.Helper()
	path := filepath.Join(bin, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
	// #nosec G302 -- test fake tools must be executable inside a private temp dir.
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatalf("chmod fake %s: %v", name, err)
	}
}
