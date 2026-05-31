// Package report renders analysis artifacts.
package report

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	fmt.Fprintln(&buf, "## Summary")
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, summary(analysis))
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Agentic Readiness")
	fmt.Fprintln(&buf)
	writeAgenticReadiness(&buf, analysis.AgenticReadiness)
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Source Coverage")
	fmt.Fprintln(&buf)
	writeSourceCoverage(&buf, analysis.SourceCoverage, analysis.DataGaps)
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
	writeFindings(&buf, analysis.WeaknessMap.Strengths)
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
	fmt.Fprintln(&buf, "## PR Quality Ledger")
	fmt.Fprintln(&buf)
	writeLedger(&buf, analysis.PRCards)
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
	fmt.Fprintln(&buf, "## Next 3 Actions")
	fmt.Fprintln(&buf)
	for _, action := range firstStrings(analysis.WeaknessMap.NextActions, 3) {
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
	fmt.Fprintln(&buf, "## Limitations")
	fmt.Fprintln(&buf)
	if len(analysis.Limitations) == 0 {
		fmt.Fprintln(&buf, "- No major limitations were recorded.")
	} else {
		for _, limitation := range analysis.Limitations {
			fmt.Fprintf(&buf, "- %s\n", limitation)
		}
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
	highlights := []string{
		fmt.Sprintf("%d artifacts analyzed", analysis.Profile.AnalyzedPRs),
	}
	for _, strength := range analysis.Profile.Strengths {
		highlights = append(highlights, strength.Label)
		if len(highlights) == 3 {
			break
		}
	}
	for len(highlights) < 3 {
		highlights = append(highlights, "Private local analysis")
	}
	subtitle := "Improving repo readiness for AI-assisted development"
	if analysis.AgenticReadiness.Grade != "" {
		subtitle = fmt.Sprintf("Agentic readiness: %s (%d/100)", analysis.AgenticReadiness.Grade, analysis.AgenticReadiness.Score)
	} else if len(analysis.Profile.ImprovementTrends) > 0 {
		subtitle = analysis.Profile.ImprovementTrends[0].Label
	}
	return signals.ShareCard{
		Version:    1,
		Title:      "Agentic readiness profile",
		Subtitle:   subtitle,
		Highlights: highlights,
		Confidence: analysis.Profile.Confidence,
		PublicSafe: true,
	}
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

func summary(analysis signals.AnalysisReport) string {
	if analysis.AgenticReadiness.Grade != "" {
		return fmt.Sprintf("%s This report measures how prepared the repo is for agentic development, then shows which data gaps prevent stronger AI-spend-to-outcome analysis.",
			analysis.AgenticReadiness.Summary,
		)
	}
	if len(analysis.WeaknessMap.Weaknesses) > 0 && len(analysis.WeaknessMap.Strengths) > 0 {
		return fmt.Sprintf("%s The main risk pattern is %s. Confidence is %s because this report is based on the available local and optional metadata signals.",
			analysis.WeaknessMap.Strengths[0].Evidence,
			strings.ToLower(analysis.WeaknessMap.Weaknesses[0].Label),
			analysis.WeaknessMap.Confidence,
		)
	}
	return fmt.Sprintf("%d artifacts were analyzed locally with %s confidence. Missing optional metadata lowers certainty instead of creating fake precision.", analysis.Profile.AnalyzedPRs, analysis.Profile.Confidence)
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

func writeSourceCoverage(buf *bytes.Buffer, coverage signals.SourceCoverage, gaps []signals.DataGap) {
	if len(coverage.Sources) == 0 {
		fmt.Fprintln(buf, "No source coverage model was computed.")
		return
	}
	fmt.Fprintf(buf, "%s Confidence: %s.\n", coverage.Summary, coverage.Confidence)
	fmt.Fprintln(buf)
	fmt.Fprintln(buf, "| Source | Status | Unlocks | Next |")
	fmt.Fprintln(buf, "| --- | --- | --- | --- |")
	for _, item := range coverage.Sources {
		next := item.NextAction
		if next == "" {
			next = "No action needed."
		}
		fmt.Fprintf(buf, "| %s | %s | %s | %s |\n", escapeTable(item.Label), item.Status, escapeTable(item.Unlocks), escapeTable(next))
	}
	if len(gaps) > 0 {
		fmt.Fprintln(buf)
		fmt.Fprintln(buf, "Most important data gaps:")
		for _, gap := range firstDataGaps(gaps, 5) {
			fmt.Fprintf(buf, "- %s: %s Next: %s\n", gap.Label, gap.Unlocks, gap.NextAction)
		}
	}
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
	}
	if len(candidates) > 0 {
		fmt.Fprintln(buf)
		fmt.Fprintln(buf, "Candidate work units:")
		for _, candidate := range firstWorkUnitCandidates(candidates, 5) {
			fmt.Fprintf(buf, "- %s: %s (%s confidence)\n", candidate.Pattern, candidate.Title, candidate.Confidence)
		}
	}
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
