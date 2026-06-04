package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/contribution-dev/contribution/internal/privacy"
	"github.com/contribution-dev/contribution/internal/signals"
)

const (
	analyzerTimeout               = 20 * time.Second
	gitleaksWorktreeMaxFiles      = 2000
	gitleaksWorktreeMaxFileBytes  = 1024 * 1024
	gitleaksWorktreeMaxTotalBytes = 20 * 1024 * 1024
)

type analyzerDefinition struct {
	name    string
	label   string
	args    []string
	parse   func([]byte) ([]signals.AnalyzerFinding, error)
	prepare func(context.Context, string, []string) (analyzerRun, []string)
}

type analyzerRun struct {
	dir     string
	args    []string
	cleanup func()
	skip    bool
}

// RunAnalyzers executes available optional analyzers and normalizes findings.
func RunAnalyzers(parent context.Context, repoPath string, repoID string, tooling signals.ToolingReport, createdAt time.Time) ([]signals.AnalyzerFinding, []signals.Signal, []string) {
	available := map[string]bool{}
	toolPaths := map[string]string{}
	envRepoPath := ""
	if tooling.TrustedRepoLocalTools {
		envRepoPath = repoPath
	}
	for _, tool := range tooling.Tools {
		available[tool.Name] = tool.Available
		toolPaths[tool.Name] = tool.Path
	}
	defs := []analyzerDefinition{
		{name: "semgrep", label: "semgrep", args: []string{"--config", "auto", "--json", "--quiet", "."}, parse: parseSemgrepFindings},
		{name: "gitleaks", label: "gitleaks history", args: []string{"git", "--redact", "--report-format", "json", "--exit-code", "0", "--no-banner", "--log-level", "error", "."}, parse: parseGitleaksFindings},
		{name: "gitleaks", label: "gitleaks worktree", args: []string{"dir", "--redact", "--report-format", "json", "--exit-code", "0", "--no-banner", "--log-level", "error", "."}, parse: parseGitleaksFindings, prepare: prepareGitleaksWorktreeRun},
		{name: "osv-scanner", label: "osv-scanner", args: []string{"--format", "json", "."}, parse: parseOSVFindings},
		{name: "trivy", label: "trivy", args: []string{"fs", "--format", "json", "--quiet", "--scanners", "vuln,secret,misconfig", "--skip-dirs", ".git", "--skip-dirs", ".tools", "--skip-dirs", "node_modules", "--skip-dirs", "dist", "."}, parse: parseTrivyFindings},
	}

	var findings []signals.AnalyzerFinding
	var limitations []string
	for _, def := range defs {
		if !available[def.name] {
			continue
		}
		run := analyzerRun{dir: repoPath, args: def.args, cleanup: func() {}}
		if def.prepare != nil {
			preparedRun, prepareLimitations := def.prepare(parent, repoPath, def.args)
			limitations = append(limitations, prepareLimitations...)
			if preparedRun.cleanup == nil {
				preparedRun.cleanup = func() {}
			}
			run = preparedRun
		}
		if run.skip {
			run.cleanup()
			continue
		}
		out, err := runAnalyzer(parent, run.dir, envRepoPath, def.name, toolPaths[def.name], run.args)
		run.cleanup()
		parsed, parseErr := def.parse(out)
		if parseErr != nil {
			limitations = append(limitations, fmt.Sprintf("%s output could not be parsed: %s", def.label, truncateAnalyzerText(privacy.RedactSecretLikeText(parseErr.Error()))))
		}
		parsed = limitAnalyzerFindings(parsed, 20)
		findings = append(findings, parsed...)
		if err != nil && len(parsed) == 0 {
			limitations = append(limitations, fmt.Sprintf("%s scan unavailable: %s", def.label, truncateAnalyzerText(privacy.RedactSecretLikeText(err.Error()))))
		}
	}

	signalsOut := make([]signals.Signal, 0, len(findings)+1)
	if len(findings) > 0 {
		signalsOut = append(signalsOut, signals.New(repoID, "tools", "analyzer_findings_count", "repo", repoID, signals.SeverityMedium, signals.DirectionNegative, signals.ConfidenceMedium, float64(len(findings)), "count", fmt.Sprintf("%d optional analyzer finding(s) were imported.", len(findings)), false, createdAt))
	}
	for _, finding := range findings {
		subject := finding.FilePath
		if subject == "" {
			subject = finding.RuleID
		}
		sig := signals.New(repoID, finding.Tool, "analyzer_finding", "file", subject, finding.Severity, signals.DirectionNegative, finding.Confidence, 1, "finding", analyzerFindingMessage(finding), finding.PublicSafe, createdAt)
		sig.FilePath = finding.FilePath
		signalsOut = append(signalsOut, sig)
	}
	return findings, signalsOut, limitations
}

func prepareGitleaksWorktreeRun(parent context.Context, repoPath string, args []string) (analyzerRun, []string) {
	run := analyzerRun{dir: repoPath, args: args, cleanup: func() {}}
	files, limitations, err := gitVisibleFiles(parent, repoPath)
	if err != nil {
		limitations = append(limitations, "gitleaks worktree scan skipped: "+truncateAnalyzerText(privacy.RedactSecretLikeText(err.Error())))
		run.skip = true
		return run, limitations
	}
	tempDir, copyLimitations, err := copyVisibleFilesForGitleaks(repoPath, files)
	limitations = append(limitations, copyLimitations...)
	if err != nil {
		limitations = append(limitations, "gitleaks worktree scan skipped: "+truncateAnalyzerText(privacy.RedactSecretLikeText(err.Error())))
		run.skip = true
		return run, limitations
	}
	if tempDir == "" {
		limitations = append(limitations, "gitleaks worktree scan skipped: no Git-visible files were eligible for bounded scanning.")
		run.skip = true
		return run, limitations
	}
	run.dir = tempDir
	run.cleanup = func() {
		_ = os.RemoveAll(tempDir)
	}
	return run, limitations
}

func gitVisibleFiles(parent context.Context, repoPath string) ([]string, []string, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	// #nosec G204 -- executable is fixed and arguments are static.
	cmd := exec.CommandContext(ctx, "git", "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, nil, fmt.Errorf("git ls-files timed out")
	}
	if err != nil {
		return nil, nil, err
	}
	raw := strings.Split(string(out), "\x00")
	files := make([]string, 0, len(raw))
	var skippedGenerated int
	var bounded bool
	for _, item := range raw {
		path := normalizeAnalyzerPath(item)
		if path == "" {
			continue
		}
		if shouldSkipWorktreeSecretPath(path) {
			skippedGenerated++
			continue
		}
		if len(files) >= gitleaksWorktreeMaxFiles {
			bounded = true
			continue
		}
		files = append(files, path)
	}
	var limitations []string
	if skippedGenerated > 0 {
		limitations = append(limitations, fmt.Sprintf("gitleaks worktree scan skipped %d generated, tool, dependency, or report path(s).", skippedGenerated))
	}
	if bounded {
		limitations = append(limitations, fmt.Sprintf("gitleaks worktree scan was bounded to %d Git-visible file(s).", len(files)))
	}
	return files, limitations, nil
}

func copyVisibleFilesForGitleaks(repoPath string, files []string) (string, []string, error) {
	tempDir, err := os.MkdirTemp("", "contribution-gitleaks-worktree-*")
	if err != nil {
		return "", nil, err
	}
	var copied int
	var totalBytes int64
	var skippedLarge int
	var skippedSpecial int
	for _, rel := range files {
		source := filepath.Join(repoPath, filepath.FromSlash(rel))
		info, err := os.Lstat(source)
		if err != nil {
			skippedSpecial++
			continue
		}
		if !info.Mode().IsRegular() {
			skippedSpecial++
			continue
		}
		if info.Size() > gitleaksWorktreeMaxFileBytes || totalBytes+info.Size() > gitleaksWorktreeMaxTotalBytes {
			skippedLarge++
			continue
		}
		// #nosec G304 -- source comes from normalized Git-visible repo-relative paths.
		data, err := os.ReadFile(source)
		if err != nil {
			skippedSpecial++
			continue
		}
		target := filepath.Join(tempDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			_ = os.RemoveAll(tempDir)
			return "", nil, err
		}
		// #nosec G703 -- target stays under a private temp dir using normalized repo-relative paths.
		if err := os.WriteFile(target, data, 0o600); err != nil {
			_ = os.RemoveAll(tempDir)
			return "", nil, err
		}
		copied++
		totalBytes += info.Size()
	}
	if copied == 0 {
		_ = os.RemoveAll(tempDir)
		tempDir = ""
	}
	var limitations []string
	if skippedLarge > 0 {
		limitations = append(limitations, fmt.Sprintf("gitleaks worktree scan skipped %d large file(s) to stay within scan bounds.", skippedLarge))
	}
	if skippedSpecial > 0 {
		limitations = append(limitations, fmt.Sprintf("gitleaks worktree scan skipped %d non-regular or unreadable file(s).", skippedSpecial))
	}
	return tempDir, limitations, nil
}

func shouldSkipWorktreeSecretPath(path string) bool {
	path = strings.Trim(filepath.ToSlash(path), "/")
	if path == "" {
		return true
	}
	for _, prefix := range []string{".git/", ".tools/", "node_modules/", "dist/", "bin/", ".contribution/reports/"} {
		if path == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func runAnalyzer(parent context.Context, repoPath string, envRepoPath string, name string, path string, args []string) ([]byte, error) {
	if path == "" {
		resolved, err := exec.LookPath(name)
		if err != nil {
			return nil, err
		}
		path = resolved
	}
	ctx, cancel := context.WithTimeout(parent, analyzerTimeout)
	defer cancel()
	// #nosec G204 -- path comes from discovery or exec.LookPath for fixed optional analyzer names.
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- analyzer names come from the fixed definitions above, not user input.
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = repoPath
	cmd.Env = toolCommandEnv(os.Environ(), envRepoPath)
	out, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return out, fmt.Errorf("timed out after %s", analyzerTimeout)
	}
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = err.Error()
		}
		return out, fmt.Errorf("%s", privacy.RedactSecretLikeText(text))
	}
	return out, nil
}

func parseSemgrepFindings(data []byte) ([]signals.AnalyzerFinding, error) {
	var raw struct {
		Results []struct {
			CheckID string `json:"check_id"`
			Path    string `json:"path"`
			Extra   struct {
				Message  string `json:"message"`
				Severity string `json:"severity"`
			} `json:"extra"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make([]signals.AnalyzerFinding, 0, len(raw.Results))
	for _, item := range raw.Results {
		message := strings.TrimSpace(item.Extra.Message)
		if message == "" {
			message = "Semgrep finding."
		}
		out = append(out, analyzerFinding("semgrep", item.CheckID, item.Extra.Severity, item.Path, message, false))
	}
	return out, nil
}

func parseGitleaksFindings(data []byte) ([]signals.AnalyzerFinding, error) {
	var raw []struct {
		RuleID      string `json:"RuleID"`
		Description string `json:"Description"`
		File        string `json:"File"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make([]signals.AnalyzerFinding, 0, len(raw))
	for _, item := range raw {
		message := strings.TrimSpace(item.Description)
		if message == "" {
			message = "Secret-like value detected by gitleaks."
		}
		out = append(out, analyzerFinding("gitleaks", item.RuleID, "high", item.File, message, false))
	}
	return out, nil
}

func parseOSVFindings(data []byte) ([]signals.AnalyzerFinding, error) {
	var raw struct {
		Results []struct {
			Source struct {
				Path string `json:"path"`
			} `json:"source"`
			Packages []struct {
				Package struct {
					Name string `json:"name"`
				} `json:"package"`
				Vulnerabilities []struct {
					ID       string `json:"id"`
					Summary  string `json:"summary"`
					Severity []struct {
						Type  string `json:"type"`
						Score string `json:"score"`
					} `json:"severity"`
				} `json:"vulnerabilities"`
			} `json:"packages"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	var out []signals.AnalyzerFinding
	for _, result := range raw.Results {
		for _, pkg := range result.Packages {
			for _, vuln := range pkg.Vulnerabilities {
				message := strings.TrimSpace(vuln.Summary)
				if message == "" {
					message = "Vulnerability detected in " + pkg.Package.Name + "."
				}
				out = append(out, analyzerFinding("osv-scanner", vuln.ID, osvSeverity(vuln.Severity), result.Source.Path, message, false))
			}
		}
	}
	return out, nil
}

func parseTrivyFindings(data []byte) ([]signals.AnalyzerFinding, error) {
	var raw struct {
		Results []struct {
			Target          string `json:"Target"`
			Vulnerabilities []struct {
				VulnerabilityID string `json:"VulnerabilityID"`
				Severity        string `json:"Severity"`
				Title           string `json:"Title"`
				PkgName         string `json:"PkgName"`
			} `json:"Vulnerabilities"`
			Misconfigurations []struct {
				ID       string `json:"ID"`
				Severity string `json:"Severity"`
				Message  string `json:"Message"`
				Title    string `json:"Title"`
			} `json:"Misconfigurations"`
			Secrets []struct {
				RuleID   string `json:"RuleID"`
				Severity string `json:"Severity"`
				Title    string `json:"Title"`
			} `json:"Secrets"`
		} `json:"Results"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	var out []signals.AnalyzerFinding
	for _, result := range raw.Results {
		for _, vuln := range result.Vulnerabilities {
			message := strings.TrimSpace(vuln.Title)
			if message == "" {
				message = "Vulnerability detected in " + vuln.PkgName + "."
			}
			out = append(out, analyzerFinding("trivy", vuln.VulnerabilityID, vuln.Severity, result.Target, message, false))
		}
		for _, misconfig := range result.Misconfigurations {
			message := strings.TrimSpace(misconfig.Message)
			if message == "" {
				message = strings.TrimSpace(misconfig.Title)
			}
			if message == "" {
				message = "Misconfiguration detected by trivy."
			}
			out = append(out, analyzerFinding("trivy", misconfig.ID, misconfig.Severity, result.Target, message, false))
		}
		for _, secret := range result.Secrets {
			message := strings.TrimSpace(secret.Title)
			if message == "" {
				message = "Secret-like value detected by trivy."
			}
			out = append(out, analyzerFinding("trivy", secret.RuleID, secret.Severity, result.Target, message, false))
		}
	}
	return out, nil
}

func analyzerFinding(tool string, ruleID string, severity string, filePath string, message string, publicSafe bool) signals.AnalyzerFinding {
	return signals.AnalyzerFinding{
		Tool:       tool,
		RuleID:     strings.TrimSpace(ruleID),
		Severity:   severityFromString(severity),
		FilePath:   normalizeAnalyzerPath(filePath),
		Scope:      "repo_existing_or_unknown",
		Message:    oneLine(message),
		Confidence: signals.ConfidenceMedium,
		PublicSafe: publicSafe,
	}
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func truncateAnalyzerText(value string) string {
	value = oneLine(value)
	if len(value) <= 300 {
		return value
	}
	return value[:300] + "..."
}

func severityFromString(value string) signals.Severity {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical":
		return signals.SeverityCritical
	case "high", "error":
		return signals.SeverityHigh
	case "medium", "warning", "warn":
		return signals.SeverityMedium
	case "low", "info", "note", "negligible":
		return signals.SeverityLow
	default:
		return signals.SeverityLow
	}
}

func osvSeverity(values []struct {
	Type  string `json:"type"`
	Score string `json:"score"`
},
) string {
	if len(values) == 0 {
		return "medium"
	}
	for _, value := range values {
		score := strings.ToUpper(value.Score)
		switch {
		case strings.Contains(score, "CRITICAL"):
			return "critical"
		case strings.Contains(score, "HIGH"):
			return "high"
		case strings.Contains(score, "MEDIUM"):
			return "medium"
		case strings.Contains(score, "LOW"):
			return "low"
		}
	}
	return "medium"
}

func normalizeAnalyzerPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.ToSlash(filepath.Clean(path))
	if path == "." || strings.HasPrefix(path, "../") || strings.HasPrefix(path, "/") {
		return ""
	}
	return path
}

func limitAnalyzerFindings(findings []signals.AnalyzerFinding, limit int) []signals.AnalyzerFinding {
	if len(findings) < limit {
		limit = len(findings)
	}
	return append([]signals.AnalyzerFinding{}, findings[:limit]...)
}

func analyzerFindingMessage(finding signals.AnalyzerFinding) string {
	if finding.FilePath == "" {
		return fmt.Sprintf("%s reported %s.", finding.Tool, finding.Message)
	}
	return fmt.Sprintf("%s reported %s in %s.", finding.Tool, finding.Message, finding.FilePath)
}
