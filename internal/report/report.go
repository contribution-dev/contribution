// Package report renders analysis artifacts.
package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/contribution-dev/contribution/internal/publicsafe"
	"github.com/contribution-dev/contribution/internal/signals"
)

// ValidateFormat checks supported report output formats.
func ValidateFormat(format string, allowAll bool) error {
	switch format {
	case "json", "markdown":
		return nil
	case "all", "":
		if allowAll {
			return nil
		}
	}
	return fmt.Errorf("unsupported format %q", format)
}

// WriteAnalysisBundle writes analysis artifacts.
func WriteAnalysisBundle(outputDir string, analysis signals.AnalysisReport, format string) error {
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if format == "" {
		format = "all"
	}
	if err := ValidateFormat(format, true); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outputDir, "analysis.json"), analysis); err != nil {
		return err
	}
	if format == "all" || format == "markdown" {
		if err := os.WriteFile(filepath.Join(outputDir, "report.md"), []byte(Markdown(analysis)), 0o600); err != nil {
			return fmt.Errorf("write report.md: %w", err)
		}
	}
	if err := writeJSON(filepath.Join(outputDir, "profile.export.json"), ProfileExport(analysis)); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outputDir, "share-card.json"), ShareCard(analysis)); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outputDir, "tooling.json"), analysis.Tooling); err != nil {
		return err
	}
	publicAnalysis := publicsafe.Analysis(analysis)
	if err := writeJSON(filepath.Join(outputDir, "source-coverage.json"), publicAnalysis.SourceCoverage); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outputDir, "attribution-readiness.json"), publicAnalysis.AttributionReadiness); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outputDir, "collector.bundle.json"), CollectorBundle(analysis)); err != nil {
		return err
	}
	return nil
}

// WriteReportOnly regenerates report artifacts from analysis.
func WriteReportOnly(outputDir string, analysis signals.AnalysisReport, format string) error {
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if format == "" {
		format = "all"
	}
	if err := ValidateFormat(format, true); err != nil {
		return err
	}
	switch format {
	case "all":
		if err := writeJSON(filepath.Join(outputDir, "analysis.json"), analysis); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(outputDir, "report.md"), []byte(Markdown(analysis)), 0o600); err != nil {
			return fmt.Errorf("write report.md: %w", err)
		}
	case "json":
		if err := writeJSON(filepath.Join(outputDir, "analysis.json"), analysis); err != nil {
			return err
		}
	case "markdown":
		if err := os.WriteFile(filepath.Join(outputDir, "report.md"), []byte(Markdown(analysis)), 0o600); err != nil {
			return fmt.Errorf("write report.md: %w", err)
		}
	}
	if err := writeJSON(filepath.Join(outputDir, "profile.export.json"), ProfileExport(analysis)); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outputDir, "share-card.json"), ShareCard(analysis)); err != nil {
		return err
	}
	publicAnalysis := publicsafe.Analysis(analysis)
	if err := writeJSON(filepath.Join(outputDir, "source-coverage.json"), publicAnalysis.SourceCoverage); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outputDir, "attribution-readiness.json"), publicAnalysis.AttributionReadiness); err != nil {
		return err
	}
	return writeJSON(filepath.Join(outputDir, "collector.bundle.json"), CollectorBundle(analysis))
}

// WriteProfileArtifacts writes only the public profile export contract files.
func WriteProfileArtifacts(outputDir string, analysis signals.AnalysisReport) error {
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := writeJSON(filepath.Join(outputDir, "profile.export.json"), ProfileExport(analysis)); err != nil {
		return err
	}
	return writeJSON(filepath.Join(outputDir, "share-card.json"), ShareCard(analysis))
}

// ReadAnalysis reads analysis.json.
func ReadAnalysis(path string) (signals.AnalysisReport, error) {
	var analysis signals.AnalysisReport
	// #nosec G304 -- the CLI intentionally reads a user-provided analysis.json path.
	data, err := os.ReadFile(path)
	if err != nil {
		return analysis, fmt.Errorf("read analysis: %w", err)
	}
	if err := json.Unmarshal(data, &analysis); err != nil {
		return analysis, fmt.Errorf("parse analysis: %w", err)
	}
	return analysis, nil
}

// Markdown renders the private human report.
func Markdown(analysis signals.AnalysisReport) string {
	var buf bytes.Buffer
	fmt.Fprintln(&buf, "# Agentic Readiness Report")
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Top Read")
	fmt.Fprintln(&buf)
	writeTopRead(&buf, analysis.TopRead)
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Summary")
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, summary())
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Agentic Readiness")
	fmt.Fprintln(&buf)
	writeAgenticReadiness(&buf, analysis.AgenticReadiness)
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Source Coverage")
	fmt.Fprintln(&buf)
	writeSourceCoverage(&buf, analysis.SourceCoverage, analysis.DataGaps, analysis.TopRead)
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Attribution Readiness")
	fmt.Fprintln(&buf)
	writeAttributionReadiness(&buf, analysis.AttributionReadiness, analysis.WorkUnitCandidates)
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Since Last Report")
	fmt.Fprintln(&buf)
	writeFollowUpComparison(&buf, analysis.FollowUp)
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Strengths")
	fmt.Fprintln(&buf)
	writeStrengthFindings(&buf, analysis.WeaknessMap.Strengths)
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Weakness Map")
	fmt.Fprintln(&buf)
	writeNumberedFindings(&buf, analysis.WeaknessMap.Weaknesses)
	if len(analysis.WeaknessMap.WatchItems) > 0 {
		fmt.Fprintln(&buf)
		fmt.Fprintln(&buf, "### Watch items")
		fmt.Fprintln(&buf)
		writeFindings(&buf, analysis.WeaknessMap.WatchItems)
	}
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Trend Comparison")
	fmt.Fprintln(&buf)
	writeTrendComparison(&buf, analysis.Trends)
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## PR Inspection Priorities")
	fmt.Fprintln(&buf)
	writeInspectionPriorities(&buf, analysis.PRCards)
	if len(analysis.PRCards) > 0 {
		fmt.Fprintln(&buf)
		fmt.Fprintln(&buf, "## Full PR Quality Ledger")
		fmt.Fprintln(&buf)
		writeLedger(&buf, analysis.PRCards)
	}
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## High-Churn Deep Dive")
	fmt.Fprintln(&buf)
	writeHighChurnDeepDive(&buf, analysis.DeepDives.HighChurn)
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## No-Test Deep Dive")
	fmt.Fprintln(&buf)
	writeNoTestDeepDive(&buf, analysis.DeepDives.NoTestArtifacts)
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Test Evidence")
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, testEvidence(analysis))
	if len(analysis.AnalyzerFindings) > 0 {
		fmt.Fprintln(&buf)
		fmt.Fprintln(&buf, "## Safety Analyzer Findings")
		fmt.Fprintln(&buf)
		writeAnalyzerFindings(&buf, analysis.AnalyzerFindings)
	}
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Durability and Churn")
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, durability(analysis))
	if analysis.Config.GitHubMetadataConfigured {
		fmt.Fprintln(&buf)
		fmt.Fprintln(&buf, "## Review Burden")
		fmt.Fprintln(&buf)
		fmt.Fprintln(&buf, "GitHub metadata was requested. Detailed review comments are enrichment data and may be unavailable if API access failed or this report did not import them for this repo.")
	}
	if len(analysis.Config.SelfReportedAITools) > 0 || len(analysis.Config.SelfReportedAIModes) > 0 {
		fmt.Fprintln(&buf)
		fmt.Fprintln(&buf, "## AI Workflow Notes")
		fmt.Fprintln(&buf)
		fmt.Fprintf(&buf, "AI workflow confidence is low because this report only uses self-reported tools and modes. Tools: %s. Modes: %s. The CLI does not detect AI-authored code or calculate token efficiency unless telemetry or metadata artifacts are imported.\n", joinOrNone(analysis.Config.SelfReportedAITools), joinOrNone(analysis.Config.SelfReportedAIModes))
	}
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Next PR Plan")
	fmt.Fprintln(&buf)
	for _, action := range firstStrings(nextPlanActions(analysis), 5) {
		fmt.Fprintf(&buf, "- %s\n", action)
	}
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Confidence Setup")
	fmt.Fprintln(&buf)
	writeSetupActions(&buf, analysis.SetupActions)
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Public Profile Preview")
	fmt.Fprintln(&buf)
	profile := ProfileExport(analysis)
	fmt.Fprintf(&buf, "- Headline: %s\n", profile.Profile.Headline)
	fmt.Fprintf(&buf, "- Analyzed artifacts: %d\n", profile.Summary.AnalyzedPRs)
	fmt.Fprintf(&buf, "- Confidence: %s\n", profile.Summary.Confidence)
	for _, strength := range profile.Strengths {
		fmt.Fprintf(&buf, "- %s: %s (%s confidence)\n", strength.Label, strength.Evidence, strength.Confidence)
	}
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Web-Connected Next Step")
	fmt.Fprintln(&buf)
	writeWebConnectedNextStep(&buf, analysis)
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Limitations")
	fmt.Fprintln(&buf)
	if len(analysis.Limitations) == 0 {
		fmt.Fprintln(&buf, "- No major limitations were recorded.")
	} else {
		writeLimitations(&buf, analysis.Limitations)
	}
	return buf.String()
}

// ProfileExport builds the public-safe profile export.
func ProfileExport(analysis signals.AnalysisReport) signals.ProfileExport {
	analysis = publicsafe.Analysis(analysis)
	var export signals.ProfileExport
	export.Version = 1
	export.GeneratedAt = analysis.GeneratedAt
	export.Profile.DisplayName = analysis.Profile.DisplayName
	export.Profile.Headline = "Agentic readiness profile"
	export.Profile.Visibility = "private_by_default"
	export.Summary.AnalyzedPRs = analysis.Profile.AnalyzedPRs
	export.Summary.AnalysisWindowDays = analysis.Profile.AnalysisWindowDays
	export.Summary.Confidence = analysis.Profile.Confidence
	export.Strengths = firstFindings(analysis.Profile.Strengths, 3)
	export.ImprovementTrends = firstFindings(analysis.Profile.ImprovementTrends, 2)
	export.BadgeCandidates = analysis.Profile.BadgeCandidates
	export.SelectedArtifacts = profileCards(analysis.PRCards, 3)
	export.Redaction.PublicSafe = true
	export.Redaction.RawCodeIncluded = false
	export.Redaction.RawDiffsIncluded = false
	export.Redaction.PrivatePathsIncluded = false
	return export
}

// ShareCard builds the compact positive sharing export.
func ShareCard(analysis signals.AnalysisReport) signals.ShareCard {
	analysis = publicsafe.Analysis(analysis)
	var highlights []string
	highlights = appendShareHighlight(highlights, artifactHighlight(analysis.Profile.AnalyzedPRs))
	for _, strength := range analysis.Profile.Strengths {
		highlights = appendShareHighlight(highlights, strength.Label)
		if len(highlights) == 3 {
			break
		}
	}
	if len(highlights) < 3 && analysis.AgenticReadiness.Grade != "" {
		highlights = appendShareHighlight(
			highlights,
			fmt.Sprintf("Readiness %s (%d/100)", analysis.AgenticReadiness.Grade, analysis.AgenticReadiness.Score),
		)
	}
	for _, finding := range analysis.TopRead.Findings {
		if len(highlights) == 3 {
			break
		}
		if highlight, ok := shareHighlightFromTopFinding(finding); ok {
			highlights = appendShareHighlight(highlights, highlight)
		}
	}
	for _, trend := range analysis.Profile.ImprovementTrends {
		if len(highlights) == 3 {
			break
		}
		highlights = appendShareHighlight(highlights, trend.Label)
	}
	for _, fallback := range []string{"Public-safe local analysis", "Deterministic repo signals", "Ready for web import"} {
		if len(highlights) == 3 {
			break
		}
		highlights = appendShareHighlight(highlights, fallback)
	}
	for len(highlights) < 3 {
		highlights = append(highlights, fmt.Sprintf("Local readiness insight %d", len(highlights)+1))
	}
	subtitle := "Improving repo readiness for AI-assisted development"
	confidence := analysis.Profile.Confidence
	if analysis.AgenticReadiness.Grade != "" {
		subtitle = fmt.Sprintf("Agentic readiness: %s (%d/100)", analysis.AgenticReadiness.Grade, analysis.AgenticReadiness.Score)
		if analysis.AgenticReadiness.Confidence != "" {
			confidence = analysis.AgenticReadiness.Confidence
		}
	} else if len(analysis.Profile.ImprovementTrends) > 0 {
		subtitle = analysis.Profile.ImprovementTrends[0].Label
	}
	return signals.ShareCard{
		Version:    1,
		Title:      "Agentic readiness profile",
		Subtitle:   subtitle,
		Highlights: highlights,
		Confidence: confidence,
		PublicSafe: true,
	}
}

func artifactHighlight(analyzed int) string {
	if analyzed <= 0 {
		return "Local readiness baseline"
	}
	if analyzed == 1 {
		return "1 artifact analyzed"
	}
	return fmt.Sprintf("%d artifacts analyzed", analyzed)
}

func appendShareHighlight(highlights []string, candidate string) []string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" || shareHighlightsContain(highlights, candidate) {
		return highlights
	}
	return append(highlights, candidate)
}

func shareHighlightsContain(highlights []string, candidate string) bool {
	for _, highlight := range highlights {
		if strings.EqualFold(strings.TrimSpace(highlight), candidate) {
			return true
		}
	}
	return false
}

func shareHighlightFromTopFinding(finding signals.TopFinding) (string, bool) {
	switch finding.ID {
	case "missing_validation_command":
		return "Validation setup identified", true
	case "failed_checks":
		return "Check reliability focus", true
	case "no_test_evidence", "risky_no_test_work":
		return "Test evidence focus", true
	case "fix_like_repair_loop", "pr_follow_up_churn":
		return "Follow-up churn focus", true
	case "high_churn_files":
		return "High-churn files surfaced", true
	case "large_work_units":
		return "Review scope focus", true
	case "context_bloat":
		return "Context efficiency focus", true
	case "attribution_gap":
		return "Attribution setup next", true
	case "setup_gap_github_metadata":
		return "GitHub connection next", true
	case "setup_gap_issue_tracker":
		return "Issue intent connection next", true
	case "setup_gap_coverage_report":
		return "Coverage import next", true
	case "setup_gap_ci_configuration":
		return "CI validation setup next", true
	case "setup_gap_repo_instructions":
		return "Agent instructions setup next", true
	case "setup_gap_validation_commands":
		return "Validation command setup next", true
	case "setup_gap_optional_static_tools":
		return "Static analysis setup next", true
	}
	return "", false
}

// WriteShareHandoff prints the public-safe share card and web handoff.
func WriteShareHandoff(out io.Writer, analysis signals.AnalysisReport, outputDir string) error {
	card := ShareCard(analysis)
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "Shareable card (public-safe)"); err != nil {
		return err
	}
	if card.Title != "" {
		if _, err := fmt.Fprintln(out, card.Title); err != nil {
			return err
		}
	}
	if card.Subtitle != "" {
		if _, err := fmt.Fprintln(out, card.Subtitle); err != nil {
			return err
		}
	}
	if card.Confidence != "" {
		if _, err := fmt.Fprintf(out, "Confidence: %s\n", card.Confidence); err != nil {
			return err
		}
	}
	for _, highlight := range card.Highlights {
		if _, err := fmt.Fprintf(out, "- %s\n", highlight); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "Create image: https://contribution.dev/share"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Upload: %s\n", filepath.Join(outputDir, "profile.export.json")); err != nil {
		return err
	}
	_, err := fmt.Fprintf(out, "        %s\n", filepath.Join(outputDir, "share-card.json"))
	return err
}

// CollectorBundle builds the public-safe web-importable local probe artifact.
func CollectorBundle(analysis signals.AnalysisReport) signals.CollectorBundle {
	headSHAAvailable := analysis.Repo.HeadSHA != "" || analysis.Trends.CurrentWindow.Commits > 0
	analysis = publicsafe.Analysis(analysis)
	highChurn := make([]string, 0, len(analysis.DeepDives.HighChurn))
	for _, item := range analysis.DeepDives.HighChurn {
		highChurn = append(highChurn, item.Path)
	}
	return signals.CollectorBundle{
		Version:     1,
		GeneratedAt: analysis.GeneratedAt,
		Repo:        analysis.Repo,
		Git: signals.CollectorGitSummary{
			CommitCount:      analysis.Trends.CurrentWindow.Commits,
			UniqueFiles:      uniqueFilesChanged(analysis.Signals),
			HighChurnFiles:   highChurn,
			HeadSHAAvailable: headSHAAvailable,
		},
		TopRead:              analysis.TopRead,
		Tooling:              analysis.Tooling,
		AgenticReadiness:     analysis.AgenticReadiness,
		SourceCoverage:       analysis.SourceCoverage,
		DataGaps:             analysis.DataGaps,
		Recommended:          analysis.RecommendedConnections,
		AttributionReadiness: analysis.AttributionReadiness,
		WorkUnitCandidates:   analysis.WorkUnitCandidates,
		AgentArtifacts:       analysis.AgentArtifacts,
		SetupActions:         analysis.SetupActions,
		Limitations:          analysis.Limitations,
		Privacy:              analysis.Privacy,
	}
}

func uniqueFilesChanged(signalsIn []signals.Signal) int {
	for _, sig := range signalsIn {
		if sig.Type == "files_changed_count" {
			return int(sig.Value)
		}
	}
	return 0
}

// WritePreflight writes current-diff preflight artifacts.
func WritePreflight(outputDir string, preflight signals.PreflightReport, format string) error {
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if format == "" {
		format = "all"
	}
	switch format {
	case "all":
		if err := writeJSON(filepath.Join(outputDir, "preflight.json"), preflight); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(outputDir, "preflight.md"), []byte(PreflightMarkdown(preflight)), 0o600)
	case "json":
		return writeJSON(filepath.Join(outputDir, "preflight.json"), preflight)
	case "markdown":
		return os.WriteFile(filepath.Join(outputDir, "preflight.md"), []byte(PreflightMarkdown(preflight)), 0o600)
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
}

// PreflightMarkdown renders a maintainer-friendly preflight.
func PreflightMarkdown(preflight signals.PreflightReport) string {
	var buf bytes.Buffer
	fmt.Fprintln(&buf, "# Contribution.dev Preflight")
	fmt.Fprintln(&buf)
	fmt.Fprintf(&buf, "Preflight risk: %s\n", titleRisk(preflight.RiskLevel))
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Why")
	fmt.Fprintln(&buf)
	for _, item := range preflight.Why {
		fmt.Fprintf(&buf, "- %s\n", item)
	}
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Changed File Summary")
	fmt.Fprintln(&buf)
	fmt.Fprintf(&buf, "- Files changed: %d\n", preflight.FileSummary.TotalFiles)
	fmt.Fprintf(&buf, "- Source: %d\n", preflight.FileSummary.SourceFiles)
	fmt.Fprintf(&buf, "- Tests: %d\n", preflight.FileSummary.TestFiles)
	fmt.Fprintf(&buf, "- Dependencies: %d\n", preflight.FileSummary.DependencyFiles)
	fmt.Fprintf(&buf, "- Generated/vendor: %d\n", preflight.FileSummary.GeneratedFiles+preflight.FileSummary.VendorFiles)
	fmt.Fprintf(&buf, "- Risky paths: %d\n", preflight.FileSummary.RiskyFiles)
	fmt.Fprintf(&buf, "- Changed lines: %d\n", preflight.TotalChangedLines)
	if len(preflight.ChangedFiles) > 0 {
		fmt.Fprintln(&buf)
		fmt.Fprintln(&buf, "## Changed Files")
		fmt.Fprintln(&buf)
		for _, file := range preflight.ChangedFiles {
			fmt.Fprintf(&buf, "- %s: +%d/-%d", file.Path, file.Additions, file.Deletions)
			if len(file.LineRanges) > 0 {
				fmt.Fprintf(&buf, " (%s)", formatLineRanges(file.LineRanges))
			}
			fmt.Fprintln(&buf)
		}
	}
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Test Evidence")
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, preflight.TestEvidence)
	if preflight.Coverage.Status != "" {
		fmt.Fprintln(&buf)
		fmt.Fprintln(&buf, "## Changed-Line Coverage")
		fmt.Fprintln(&buf)
		switch preflight.Coverage.Status {
		case "available":
			fmt.Fprintf(&buf, "%.1f%% (%d/%d executable changed lines)\n", preflight.Coverage.Percent, preflight.Coverage.CoveredLines, preflight.Coverage.TotalLines)
		default:
			if preflight.Coverage.Reason != "" {
				fmt.Fprintln(&buf, preflight.Coverage.Reason)
			} else {
				fmt.Fprintln(&buf, "Changed-line coverage is unknown.")
			}
		}
	}
	if len(preflight.AnalyzerFindings) > 0 {
		fmt.Fprintln(&buf)
		fmt.Fprintln(&buf, "## Safety Analyzer Findings")
		fmt.Fprintln(&buf)
		writeAnalyzerFindings(&buf, preflight.AnalyzerFindings)
	}
	if len(preflight.Rubric) > 0 {
		fmt.Fprintln(&buf)
		fmt.Fprintln(&buf, "## Rubric")
		fmt.Fprintln(&buf)
		for _, item := range preflight.Rubric {
			fmt.Fprintf(&buf, "- %s: %s (%s). %s", item.Label, item.Status, item.Severity, item.Evidence)
			if item.Recommendation != "" {
				fmt.Fprintf(&buf, " Next: %s", item.Recommendation)
			}
			fmt.Fprintln(&buf)
		}
	}
	if preflight.PersonalContext != nil {
		fmt.Fprintln(&buf)
		fmt.Fprintln(&buf, "## Personal Pattern Checks")
		fmt.Fprintln(&buf)
		fmt.Fprintf(&buf, "- Recent artifacts analyzed: %d\n", preflight.PersonalContext.ArtifactsAnalyzed)
		if len(preflight.PersonalContext.HighChurnFiles) > 0 {
			fmt.Fprintf(&buf, "- Recent high-churn files: %s\n", strings.Join(preflight.PersonalContext.HighChurnFiles, ", "))
		}
		if preflight.PersonalContext.RecentSourceWithoutTests > 0 {
			fmt.Fprintf(&buf, "- Recent source-without-test artifacts: %d\n", preflight.PersonalContext.RecentSourceWithoutTests)
		}
		if preflight.PersonalContext.TypicalFiles > 0 || preflight.PersonalContext.TypicalLines > 0 {
			fmt.Fprintf(&buf, "- Typical recent scope: %d file(s), %d line(s)\n", preflight.PersonalContext.TypicalFiles, preflight.PersonalContext.TypicalLines)
		}
	}
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Reviewer Focus")
	fmt.Fprintln(&buf)
	for _, item := range preflight.ReviewerFocus {
		fmt.Fprintf(&buf, "- %s\n", item)
	}
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Limitations")
	fmt.Fprintln(&buf)
	for _, item := range preflight.Limitations {
		fmt.Fprintf(&buf, "- %s\n", item)
	}
	return buf.String()
}

func formatLineRanges(ranges []signals.LineRange) string {
	parts := make([]string, 0, len(ranges))
	for _, rng := range ranges {
		if rng.Start == rng.End {
			parts = append(parts, fmt.Sprintf("L%d", rng.Start))
		} else {
			parts = append(parts, fmt.Sprintf("L%d-L%d", rng.Start, rng.End))
		}
	}
	return strings.Join(parts, ", ")
}

// WritePacket writes friend-review packet artifacts.
func WritePacket(outputDir string, packet signals.FriendReviewPacket) error {
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := writeJSON(filepath.Join(outputDir, "friend-review-packet.json"), packet); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outputDir, "friend-review-packet.md"), []byte(PacketMarkdown(packet)), 0o600)
}

// PacketMarkdown renders a friend-review packet.
func PacketMarkdown(packet signals.FriendReviewPacket) string {
	var buf bytes.Buffer
	fmt.Fprintln(&buf, "# Friend Review Packet")
	fmt.Fprintln(&buf)
	fmt.Fprintf(&buf, "Packet ID: %s\n", packet.PacketID)
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Context")
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, packet.Context)
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Evidence")
	fmt.Fprintln(&buf)
	for _, item := range packet.Evidence {
		fmt.Fprintf(&buf, "- %s\n", item)
	}
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Rubric")
	fmt.Fprintln(&buf)
	for i, question := range packet.Rubric {
		if question.Focus == "" {
			fmt.Fprintf(&buf, "%d. %s\n", i+1, question.Prompt)
		} else {
			fmt.Fprintf(&buf, "%d. %s (%s)\n", i+1, question.Prompt, question.Focus)
		}
	}
	fmt.Fprintln(&buf)
	fmt.Fprintf(&buf, "Confidence: %s\n", packet.Confidence)
	return buf.String()
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", filepath.Base(path), err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}

func summary() string {
	return "This report uses deterministic local evidence: git history, repo shape, validation/config signals, and any artifacts you explicitly import. For PR review/check metadata, issue intent, AI/session telemetry, or product outcome context, import the CLI probe bundle (`collector.bundle.json`) at contribution.dev and connect those sources there."
}

func nextPlanActions(analysis signals.AnalysisReport) []string {
	if len(analysis.TopRead.NextPRPlan) > 0 {
		return analysis.TopRead.NextPRPlan
	}
	if len(analysis.WeaknessMap.NextActions) > 0 {
		return analysis.WeaknessMap.NextActions
	}
	return []string{"Run contribution preflight before the next behavior-changing PR."}
}

func writeTopRead(buf *bytes.Buffer, top signals.TopRead) {
	if top.Headline == "" {
		fmt.Fprintln(buf, "No deterministic top read was computed.")
		return
	}
	fmt.Fprintln(buf, top.Headline)
	if top.Summary != "" {
		fmt.Fprintf(buf, "\n%s\n", top.Summary)
	}
	if len(top.Findings) > 0 {
		fmt.Fprintln(buf)
		for _, finding := range top.Findings {
			fmt.Fprintf(buf, "- %s: %s (%s confidence)\n", finding.Label, finding.Evidence, finding.Confidence)
			if finding.WhyItMatters != "" {
				fmt.Fprintf(buf, "  Why it matters: %s\n", finding.WhyItMatters)
			}
			if finding.NextAction != "" {
				fmt.Fprintf(buf, "  Next: %s\n", finding.NextAction)
			}
		}
	}
	if len(top.NextPRPlan) > 0 {
		fmt.Fprintln(buf)
		fmt.Fprintln(buf, "Next PR plan:")
		for _, action := range firstStrings(top.NextPRPlan, 5) {
			fmt.Fprintf(buf, "- %s\n", action)
		}
	}
	if top.Confidence != "" {
		fmt.Fprintf(buf, "\nConfidence: %s\n", top.Confidence)
	}
}

func writeAgenticReadiness(buf *bytes.Buffer, readiness signals.AgenticReadiness) {
	if readiness.Grade == "" {
		fmt.Fprintln(buf, "No agentic readiness score was computed.")
		return
	}
	fmt.Fprintf(buf, "Your repo is a %s (%d/100). Confidence: %s.\n", readiness.Grade, readiness.Score, readiness.Confidence)
	if readiness.Summary != "" {
		fmt.Fprintf(buf, "\n%s\n", readiness.Summary)
	}
	if len(readiness.Components) > 0 {
		fmt.Fprintln(buf)
		fmt.Fprintln(buf, "| Component | Score | Confidence | Evidence |")
		fmt.Fprintln(buf, "| --- | ---: | --- | --- |")
		for _, component := range readiness.Components {
			fmt.Fprintf(buf, "| %s | %d | %s | %s |\n", escapeTable(component.Label), component.Score, component.Confidence, escapeTable(component.Evidence))
		}
	}
	if len(readiness.TopActions) > 0 {
		fmt.Fprintln(buf)
		fmt.Fprintln(buf, "Highest-ROI improvements:")
		for _, action := range firstStrings(readiness.TopActions, 5) {
			fmt.Fprintf(buf, "- %s\n", action)
		}
	}
	if len(readiness.Limitations) > 0 {
		fmt.Fprintln(buf)
		fmt.Fprintln(buf, "What would make this score more trustworthy:")
		for _, limitation := range readiness.Limitations {
			fmt.Fprintf(buf, "- %s\n", limitation)
		}
	}
}

func writeSourceCoverage(buf *bytes.Buffer, coverage signals.SourceCoverage, gaps []signals.DataGap, topRead signals.TopRead) {
	if len(coverage.Sources) == 0 {
		fmt.Fprintln(buf, "No source coverage model was computed.")
		return
	}
	fmt.Fprintf(buf, "%s Confidence: %s.\n", coverage.Summary, coverage.Confidence)
	readiness, future := splitSourceCoverage(coverage.Sources)
	writeSourceCoverageGroup(buf, "Readiness Essentials", readiness)
	writeSourceCoverageGroup(buf, "Future ROI Telemetry", future)
	readinessGaps := readinessDataGaps(gaps, topRead)
	if len(readinessGaps) > 0 {
		fmt.Fprintln(buf)
		fmt.Fprintln(buf, "Most important readiness gaps:")
		for _, gap := range firstDataGaps(readinessGaps, 5) {
			fmt.Fprintf(buf, "- %s: %s Next: %s\n", gap.Label, gap.Unlocks, gap.NextAction)
		}
	}
	if hasFutureDataGaps(gaps) {
		fmt.Fprintln(buf)
		fmt.Fprintln(buf, "Future ROI telemetry stays separate from readiness. Connect spend, session, deployment, or product data in the web report when you are ready to measure outcomes beyond local engineering evidence.")
	}
}

func splitSourceCoverage(items []signals.SourceCoverageItem) ([]signals.SourceCoverageItem, []signals.SourceCoverageItem) {
	var readiness []signals.SourceCoverageItem
	var future []signals.SourceCoverageItem
	for _, item := range items {
		switch item.Category {
		case "spend", "business":
			future = append(future, item)
		default:
			readiness = append(readiness, item)
		}
	}
	return readiness, future
}

func writeSourceCoverageGroup(buf *bytes.Buffer, title string, items []signals.SourceCoverageItem) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintln(buf)
	fmt.Fprintf(buf, "### %s\n\n", title)
	fmt.Fprintln(buf, "| Source | Status | Unlocks | Next |")
	fmt.Fprintln(buf, "| --- | --- | --- | --- |")
	for _, item := range items {
		next := item.NextAction
		if next == "" {
			next = "No action needed."
		}
		fmt.Fprintf(buf, "| %s | %s | %s | %s |\n", escapeTable(item.Label), item.Status, escapeTable(item.Unlocks), escapeTable(next))
	}
}

func readinessDataGaps(gaps []signals.DataGap, topRead signals.TopRead) []signals.DataGap {
	filtered := make([]signals.DataGap, 0, len(gaps))
	for _, gap := range gaps {
		if futureTelemetryGapID(gap.ID) {
			continue
		}
		filtered = append(filtered, gap)
	}
	if len(topRead.Findings) == 0 {
		return filtered
	}
	out := make([]signals.DataGap, 0, len(filtered))
	used := map[string]bool{}
	for _, id := range topReadDataGapIDs(topRead) {
		for _, gap := range filtered {
			if gap.ID != id || used[gap.ID] {
				continue
			}
			out = append(out, gap)
			used[gap.ID] = true
			break
		}
	}
	for _, gap := range filtered {
		if used[gap.ID] {
			continue
		}
		out = append(out, gap)
	}
	return out
}

func topReadDataGapIDs(topRead signals.TopRead) []string {
	ids := make([]string, 0, len(topRead.Findings))
	for _, finding := range topRead.Findings {
		switch finding.ID {
		case "missing_validation_command":
			ids = append(ids, "validation_commands")
		case "attribution_gap":
			ids = append(ids, "github_metadata", "issue_tracker")
		case "setup_gap_github_metadata":
			ids = append(ids, "github_metadata")
		case "setup_gap_issue_tracker":
			ids = append(ids, "issue_tracker")
		case "setup_gap_coverage_report":
			ids = append(ids, "coverage_report")
		case "setup_gap_optional_static_tools":
			ids = append(ids, "optional_static_tools")
		}
	}
	return ids
}

func hasFutureDataGaps(gaps []signals.DataGap) bool {
	for _, gap := range gaps {
		if futureTelemetryGapID(gap.ID) {
			return true
		}
	}
	return false
}

func futureTelemetryGapID(id string) bool {
	switch id {
	case "ai_spend_telemetry", "agent_session_telemetry", "deployment_product_telemetry":
		return true
	}
	return false
}

func writeAttributionReadiness(buf *bytes.Buffer, attribution signals.AttributionReadiness, candidates []signals.WorkUnitCandidate) {
	if attribution.Pattern == "" {
		fmt.Fprintln(buf, "No attribution readiness model was computed.")
		return
	}
	fmt.Fprintf(buf, "Pattern: %s. Confidence: %s.\n", attribution.Pattern, attribution.Confidence)
	if attribution.Summary != "" {
		fmt.Fprintf(buf, "\n%s\n", attribution.Summary)
	}
	if attribution.NextAction != "" {
		fmt.Fprintf(buf, "\nNext: %s\n", attribution.NextAction)
	}
	if len(attribution.MissingEvidence) > 0 {
		fmt.Fprintln(buf)
		fmt.Fprintf(buf, "Missing evidence: %s.\n", strings.Join(attribution.MissingEvidence, ", "))
		fmt.Fprintln(buf)
		fmt.Fprintln(buf, "Web-connected context helps here: import the CLI probe bundle (`collector.bundle.json`) at contribution.dev to connect GitHub PRs, issue tracker intent, and agent-session metadata instead of asking the local CLI to infer work units from commit batches.")
	}
	if len(candidates) > 0 {
		fmt.Fprintln(buf)
		fmt.Fprintln(buf, "Candidate work units:")
		for _, candidate := range firstWorkUnitCandidates(candidates, 5) {
			fmt.Fprintf(buf, "- %s: %s (%s confidence)\n", candidate.Pattern, candidate.Title, candidate.Confidence)
		}
	}
}

func writeWebConnectedNextStep(buf *bytes.Buffer, analysis signals.AnalysisReport) {
	fmt.Fprintln(buf, "The CLI report is complete for deterministic local evidence. The web report becomes useful when you want the missing context this process should not guess locally.")
	fmt.Fprintln(buf)
	fmt.Fprintln(buf, "Import the CLI probe bundle (`collector.bundle.json`) at contribution.dev when you want to connect GitHub reviews/checks, issue tracker intent, AI session or spend metadata, and deployment or product outcomes to the same findings.")
	if reasons := webConnectedReasons(analysis); len(reasons) > 0 {
		fmt.Fprintln(buf)
		fmt.Fprintln(buf, "Most useful connected checks for this run:")
		for _, reason := range firstStrings(reasons, 3) {
			fmt.Fprintf(buf, "- %s\n", reason)
		}
	}
	if len(analysis.SourceCoverage.NextActions) > 0 {
		fmt.Fprintln(buf)
		fmt.Fprintln(buf, "Most relevant connection from this run:")
		fmt.Fprintf(buf, "- %s\n", analysis.SourceCoverage.NextActions[0])
	}
}

func webConnectedReasons(analysis signals.AnalysisReport) []string {
	reasons := make([]string, 0, len(analysis.TopRead.Findings))
	seen := map[string]bool{}
	for _, finding := range analysis.TopRead.Findings {
		var reason string
		switch finding.ID {
		case "fix_like_repair_loop", "pr_follow_up_churn":
			reason = "Connect GitHub PR and changed-file metadata to verify whether the fix-like commits followed the same PRs or files."
		case "high_churn_files":
			reason = "Connect GitHub and issue intent to group the repeated file touches by feature or incident."
		case "no_test_evidence", "risky_no_test_work":
			reason = "Connect GitHub checks and reviews to see whether the untested or risky changes caused CI failures, requested changes, or follow-up fixes."
		case "large_work_units":
			reason = "Connect PR metadata to separate broad commits into actual review units and find which large changes created reviewer load."
		case "missing_validation_command":
			reason = "After adding one validation command, import the bundle so the web report can show which repos are ready for repeatable agent work."
		case "attribution_gap":
			reason = "Connect issues or work-unit markers to tie the local findings to feature intent instead of raw commit batches."
		}
		if reason == "" || seen[reason] {
			continue
		}
		reasons = append(reasons, reason)
		seen[reason] = true
	}
	return reasons
}

func writeLimitations(buf *bytes.Buffer, limitations []string) {
	for _, limitation := range dedupeLimitations(limitations) {
		fmt.Fprintf(buf, "- %s\n", limitation)
	}
}

func dedupeLimitations(limitations []string) []string {
	type limitationChoice struct {
		text string
		rank int
	}
	choices := map[string]limitationChoice{}
	var order []string
	for _, limitation := range limitations {
		limitation = strings.TrimSpace(limitation)
		if limitation == "" {
			continue
		}
		key := limitationKey(limitation)
		rank := limitationRank(limitation)
		if _, ok := choices[key]; !ok {
			order = append(order, key)
			choices[key] = limitationChoice{text: limitation, rank: rank}
			continue
		}
		if rank < choices[key].rank {
			choices[key] = limitationChoice{text: limitation, rank: rank}
		}
	}
	out := make([]string, 0, len(order))
	for _, key := range order {
		out = append(out, choices[key].text)
	}
	return out
}

func limitationKey(limitation string) string {
	normalized := strings.ToLower(limitation)
	switch {
	case strings.Contains(normalized, "no coverage report was imported"):
		return "coverage"
	case strings.Contains(normalized, "github metadata was not") ||
		strings.Contains(normalized, "review burden is unavailable"):
		return "github_metadata"
	}
	return normalized
}

func limitationRank(limitation string) int {
	normalized := strings.ToLower(limitation)
	switch {
	case strings.Contains(normalized, "testing confidence"):
		return 0
	case strings.Contains(normalized, "review and pr workflow evidence"):
		return 0
	case strings.Contains(normalized, "review burden is unavailable"):
		return 1
	case strings.Contains(normalized, "no coverage report was imported"):
		return 1
	case strings.Contains(normalized, "github metadata was not"):
		return 2
	}
	return 0
}

func writeFindings(buf *bytes.Buffer, findings []signals.Finding) {
	if len(findings) == 0 {
		fmt.Fprintln(buf, "- No finding available.")
		return
	}
	for _, finding := range findings {
		fmt.Fprintf(buf, "- %s: %s (%s confidence)\n", finding.Label, finding.Evidence, finding.Confidence)
	}
}

func writeStrengthFindings(buf *bytes.Buffer, findings []signals.Finding) {
	if len(findings) == 0 {
		fmt.Fprintln(buf, "No durable strength signal was visible in this local run. That is neutral evidence, not a failure; inspect the watch items and setup gaps before treating the repo as ready.")
		return
	}
	writeFindings(buf, findings)
}

func writeNumberedFindings(buf *bytes.Buffer, findings []signals.Finding) {
	if len(findings) == 0 {
		fmt.Fprintln(buf, "No weakness pattern was detected with the available evidence.")
		return
	}
	for i, finding := range findings {
		fmt.Fprintf(buf, "### %d. %s\n\n", i+1, finding.Label)
		fmt.Fprintf(buf, "Evidence: %s\n\n", finding.Evidence)
		if finding.WhyItMatters != "" {
			fmt.Fprintf(buf, "Why it matters: %s\n\n", finding.WhyItMatters)
		}
		if finding.NextAction != "" {
			fmt.Fprintf(buf, "Suggested next action: %s\n\n", finding.NextAction)
		}
		fmt.Fprintf(buf, "Confidence: %s\n\n", finding.Confidence)
	}
}

func writeTrendComparison(buf *bytes.Buffer, trends signals.TrendComparison) {
	if trends.Status == "" {
		fmt.Fprintln(buf, "No trend comparison was computed.")
		return
	}
	fmt.Fprintf(buf, "Recent window: %d commit artifact(s). Prior window: %d commit artifact(s). Status: %s. Confidence: %s.\n", trends.CurrentWindow.Commits, trends.PriorWindow.Commits, trends.Status, trends.Confidence)
	if trends.Reason != "" {
		fmt.Fprintf(buf, "\n%s\n", trends.Reason)
	}
	if len(trends.Findings) > 0 {
		fmt.Fprintln(buf)
		for _, finding := range trends.Findings {
			fmt.Fprintf(buf, "- %s: %s (%s confidence)", finding.Label, finding.Evidence, finding.Confidence)
			if finding.NextAction != "" {
				fmt.Fprintf(buf, " Next: %s", finding.NextAction)
			}
			fmt.Fprintln(buf)
		}
	}
	if len(trends.Metrics) == 0 {
		return
	}
	fmt.Fprintln(buf)
	fmt.Fprintln(buf, "| Metric | Direction | Recent | Prior | Delta | Next |")
	fmt.Fprintln(buf, "| --- | --- | ---: | ---: | ---: | --- |")
	for _, metric := range trends.Metrics {
		fmt.Fprintf(
			buf,
			"| %s | %s | %s | %s | %s | %s |\n",
			escapeTable(metric.Label),
			escapeTable(metric.Direction),
			escapeTable(formatTrendMetricValue(metric.CurrentValue, metric.Unit)),
			escapeTable(formatTrendMetricValue(metric.PriorValue, metric.Unit)),
			escapeTable(formatTrendMetricValue(metric.Delta, metric.Unit)),
			escapeTable(metric.NextAction),
		)
	}
}

func writeFollowUpComparison(buf *bytes.Buffer, followUp signals.FollowUpComparison) {
	if followUp.Status == "" {
		fmt.Fprintln(buf, "No previous-report comparison was computed.")
		return
	}
	if followUp.Summary != "" {
		fmt.Fprintln(buf, followUp.Summary)
	} else if followUp.Reason != "" {
		fmt.Fprintln(buf, followUp.Reason)
	}
	if followUp.Status != "available" {
		if followUp.NextAction != "" {
			fmt.Fprintf(buf, "\nBest next move: %s\n", followUp.NextAction)
		}
		return
	}
	writeFollowUpGroup(buf, "Improved", followUp.Improved)
	writeFollowUpGroup(buf, "Got worse", followUp.Regressed)
	writeFollowUpGroup(buf, "Resolved", followUp.Resolved)
	writeFollowUpGroup(buf, "Still true", followUp.Persistent)
	if len(followUp.Improved)+len(followUp.Regressed)+len(followUp.Resolved)+len(followUp.Persistent) == 0 {
		fmt.Fprintln(buf, "\nNo major tracked movement was detected since the last report.")
	}
	if followUp.NextAction != "" {
		fmt.Fprintf(buf, "\nBest next move: %s\n", followUp.NextAction)
	}
	fmt.Fprintf(buf, "\nConfidence: %s\n", followUp.Confidence)
}

func writeFollowUpGroup(buf *bytes.Buffer, title string, findings []signals.Finding) {
	if len(findings) == 0 {
		return
	}
	fmt.Fprintf(buf, "\n%s:\n", title)
	for _, finding := range findings {
		fmt.Fprintf(buf, "- %s: %s (%s confidence)", finding.Label, finding.Evidence, finding.Confidence)
		if finding.NextAction != "" && title == "Got worse" {
			fmt.Fprintf(buf, " Next: %s", finding.NextAction)
		}
		fmt.Fprintln(buf)
	}
}

func firstFindings(values []signals.Finding, limit int) []signals.Finding {
	if len(values) < limit {
		limit = len(values)
	}
	return append([]signals.Finding{}, values[:limit]...)
}

func profileCards(cards []signals.PRQualityCard, limit int) []signals.PRQualityCard {
	out := make([]signals.PRQualityCard, 0, min(limit, len(cards)))
	for _, label := range []string{"strong", "mixed"} {
		for _, card := range cards {
			if card.Label != label {
				continue
			}
			out = append(out, publicsafe.Card(card, len(out)+1))
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func firstStrings(values []string, limit int) []string {
	if len(values) < limit {
		limit = len(values)
	}
	return values[:limit]
}

func firstDataGaps(values []signals.DataGap, limit int) []signals.DataGap {
	if len(values) < limit {
		limit = len(values)
	}
	return values[:limit]
}

func firstWorkUnitCandidates(values []signals.WorkUnitCandidate, limit int) []signals.WorkUnitCandidate {
	if len(values) < limit {
		limit = len(values)
	}
	return values[:limit]
}

func joinOrNone(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

func escapeTable(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "|", "\\|")
	return value
}

func formatTrendMetricValue(value float64, unit string) string {
	if unit == "percent" {
		return fmt.Sprintf("%.1f%%", value)
	}
	return fmt.Sprintf("%.0f", value)
}

func titleRisk(value string) string {
	if value == "" {
		return "Unknown"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
