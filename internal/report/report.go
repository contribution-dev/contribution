// Package report renders analysis artifacts.
package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/contribution-dev/contribution/internal/privacy"
	"github.com/contribution-dev/contribution/internal/signals"
)

type pathReplacement struct {
	private string
	public  string
}

var (
	commitSHAPattern     = regexp.MustCompile(`\b[0-9a-fA-F]{7,40}\b`)
	pathCandidatePattern = regexp.MustCompile(`(?:[A-Za-z]:)?(?:[./~]?[\w.-]+[/\\])+[\w.@+-]+`)
)

const publicRepoID = "private-repository"

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
	return writeJSON(filepath.Join(outputDir, "share-card.json"), ShareCard(analysis))
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

// PublicSafeAnalysis returns an analysis report suitable for public-safe output.
func PublicSafeAnalysis(analysis signals.AnalysisReport) signals.AnalysisReport {
	sourceRepoID := analysis.Repo.ID
	pathReplacements := publicSafePathReplacements(analysis)
	analysis.Repo = PublicSafeRepo(analysis.Repo)
	analysis.Config.PublicSafe = true
	analysis.Config.OutputDirectory = ""
	analysis.Config.GitHubMetadataConfigured = false
	analysis.Config.SelfReportedAITools = nil
	analysis.Config.SelfReportedAIModes = nil
	analysis.Privacy.PublicSafe = true
	analysis.Privacy.RawCodeIncluded = false
	analysis.Privacy.RawDiffsIncluded = false
	analysis.Privacy.PrivatePathsIncludedInPublicExport = false
	analysis.Privacy.AuthorEmailsIncluded = false
	analysis.PRCards = publicCards(analysis.PRCards, len(analysis.PRCards), pathReplacements)
	analysis.WeaknessMap = publicSafeWeaknessMap(analysis.WeaknessMap, pathReplacements)
	analysis.Coverage = publicSafeCoverage(analysis.Coverage, pathReplacements)
	analysis.DeepDives = publicSafeDeepDives(analysis.DeepDives, pathReplacements)
	analysis.Profile.Strengths = publicFindings(analysis.Profile.Strengths, len(analysis.Profile.Strengths), pathReplacements)
	analysis.Profile.ImprovementTrends = publicFindings(analysis.Profile.ImprovementTrends, len(analysis.Profile.ImprovementTrends), pathReplacements)
	analysis.Profile.Headline = redactCommitLikeText(redactText(analysis.Profile.Headline, pathReplacements))
	analysis.Profile.DisplayName = redactCommitLikeText(redactText(analysis.Profile.DisplayName, pathReplacements))
	analysis.SetupActions = publicSafeSetupActions(analysis.SetupActions, pathReplacements)
	analysis.Limitations = redactStrings(analysis.Limitations, pathReplacements)
	for i := range analysis.Signals {
		analysis.Signals[i] = publicSafeSignal(analysis.Signals[i], sourceRepoID, pathReplacements)
	}
	for i := range analysis.Tooling.Tools {
		analysis.Tooling.Tools[i].Version = redactText(analysis.Tooling.Tools[i].Version, pathReplacements)
		analysis.Tooling.Tools[i].Reason = redactText(analysis.Tooling.Tools[i].Reason, pathReplacements)
	}
	analysis.Tooling.Limitations = redactStrings(analysis.Tooling.Limitations, pathReplacements)
	return analysis
}

// PublicSafeRepo removes private repository identity from metadata.
func PublicSafeRepo(repo signals.RepoMetadata) signals.RepoMetadata {
	repo.ID = publicRepoID
	repo.Name = "private repository"
	repo.Root = ""
	repo.RemoteURL = ""
	repo.HeadSHA = ""
	repo.GitHubOwner = ""
	repo.GitHubRepo = ""
	return repo
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
	fmt.Fprintln(&buf, "# Contribution.dev Report")
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "## Summary")
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, summary(analysis))
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
		fmt.Fprintf(&buf, "AI workflow confidence is low because this report only uses self-reported tools and modes. Tools: %s. Modes: %s. The CLI does not detect AI-authored code or calculate token efficiency.\n", joinOrNone(analysis.Config.SelfReportedAITools), joinOrNone(analysis.Config.SelfReportedAIModes))
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
	pathReplacements := publicSafePathReplacements(analysis)
	var export signals.ProfileExport
	export.Version = 1
	export.GeneratedAt = analysis.GeneratedAt
	export.Profile.DisplayName = redactCommitLikeText(redactText(analysis.Profile.DisplayName, pathReplacements))
	export.Profile.Headline = "AI-native contribution profile"
	export.Profile.Visibility = "private_by_default"
	export.Summary.AnalyzedPRs = analysis.Profile.AnalyzedPRs
	export.Summary.AnalysisWindowDays = analysis.Profile.AnalysisWindowDays
	export.Summary.Confidence = analysis.Profile.Confidence
	export.Strengths = publicFindings(analysis.Profile.Strengths, 3, pathReplacements)
	export.ImprovementTrends = publicFindings(analysis.Profile.ImprovementTrends, 2, pathReplacements)
	export.BadgeCandidates = analysis.Profile.BadgeCandidates
	export.SelectedArtifacts = publicCards(analysis.PRCards, 3, pathReplacements)
	export.Redaction.PublicSafe = true
	export.Redaction.RawCodeIncluded = false
	export.Redaction.RawDiffsIncluded = false
	export.Redaction.PrivatePathsIncluded = false
	return export
}

// ShareCard builds the compact positive sharing export.
func ShareCard(analysis signals.AnalysisReport) signals.ShareCard {
	pathReplacements := publicSafePathReplacements(analysis)
	highlights := []string{
		fmt.Sprintf("%d artifacts analyzed", analysis.Profile.AnalyzedPRs),
	}
	for _, strength := range analysis.Profile.Strengths {
		highlights = append(highlights, redactCommitLikeText(redactText(strength.Label, pathReplacements)))
		if len(highlights) == 3 {
			break
		}
	}
	for len(highlights) < 3 {
		highlights = append(highlights, "Private local analysis")
	}
	subtitle := "Improving contribution quality across recent work"
	if len(analysis.Profile.ImprovementTrends) > 0 {
		subtitle = redactCommitLikeText(redactText(analysis.Profile.ImprovementTrends[0].Label, pathReplacements))
	}
	return signals.ShareCard{
		Version:    1,
		Title:      "AI-native contribution profile",
		Subtitle:   subtitle,
		Highlights: highlights,
		Confidence: analysis.Profile.Confidence,
		PublicSafe: true,
	}
}

// PublicSafeCard returns a single neutral, public-safe card.
func PublicSafeCard(card signals.PRQualityCard, ordinal int) signals.PRQualityCard {
	return publicCard(card, ordinal)
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
	if len(preflight.Rubric) > 0 {
		fmt.Fprintln(&buf)
		fmt.Fprintln(&buf, "## Rubric")
		fmt.Fprintln(&buf)
		for _, item := range preflight.Rubric {
			fmt.Fprintf(&buf, "- %s: %s (%s). %s\n", item.Label, item.Status, item.Severity, item.Evidence)
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
	if len(analysis.WeaknessMap.Weaknesses) > 0 && len(analysis.WeaknessMap.Strengths) > 0 {
		return fmt.Sprintf("%s The main risk pattern is %s. Confidence is %s because this report is based on the available local and optional metadata signals.",
			analysis.WeaknessMap.Strengths[0].Evidence,
			strings.ToLower(analysis.WeaknessMap.Weaknesses[0].Label),
			analysis.WeaknessMap.Confidence,
		)
	}
	return fmt.Sprintf("%d artifacts were analyzed locally with %s confidence. Missing optional metadata lowers certainty instead of creating fake precision.", analysis.Profile.AnalyzedPRs, analysis.Profile.Confidence)
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

func publicFindings(findings []signals.Finding, limit int, replacements ...[]pathReplacement) []signals.Finding {
	if len(findings) < limit {
		limit = len(findings)
	}
	out := make([]signals.Finding, 0, limit)
	for i := 0; i < limit; i++ {
		f := findings[i]
		f.Label = redactCommitLikeText(redactText(f.Label, replacements...))
		f.Evidence = redactCommitLikeText(redactText(f.Evidence, replacements...))
		f.NextAction = ""
		f.WhyItMatters = ""
		out = append(out, f)
	}
	return out
}

func publicCards(cards []signals.PRQualityCard, limit int, replacements ...[]pathReplacement) []signals.PRQualityCard {
	if len(cards) < limit {
		limit = len(cards)
	}
	out := make([]signals.PRQualityCard, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, publicCard(cards[i], i+1, replacements...))
	}
	return out
}

func publicCard(card signals.PRQualityCard, ordinal int, replacements ...[]pathReplacement) signals.PRQualityCard {
	card.Title = publicArtifactTitle(card, ordinal)
	card.URL = ""
	card.Summary = redactCommitLikeText(redactText(card.Summary, replacements...))
	card.Scope = redactText(card.Scope, replacements...)
	card.TestEvidence = redactText(card.TestEvidence, replacements...)
	card.ReviewBurden = redactText(card.ReviewBurden, replacements...)
	card.Durability = redactText(card.Durability, replacements...)
	card.MainRisk = ""
	card.Strengths = nil
	card.Risks = nil
	card.Evidence = nil
	card.NextAction = ""
	return card
}

func publicArtifactTitle(card signals.PRQualityCard, ordinal int) string {
	if card.PRNumber > 0 {
		return fmt.Sprintf("PR #%d", card.PRNumber)
	}
	if ordinal <= 0 {
		ordinal = 1
	}
	return fmt.Sprintf("Artifact %d", ordinal)
}

func publicSafeWeaknessMap(value signals.WeaknessMap, replacements ...[]pathReplacement) signals.WeaknessMap {
	value.Strengths = redactFindings(value.Strengths, replacements...)
	value.Weaknesses = redactFindings(value.Weaknesses, replacements...)
	value.WatchItems = redactFindings(value.WatchItems, replacements...)
	value.NextActions = redactStrings(value.NextActions, replacements...)
	return value
}

func publicSafeCoverage(value signals.CoverageSummary, replacements ...[]pathReplacement) signals.CoverageSummary {
	for i := range value.Files {
		value.Files[i].Path = privacy.RedactPath(redactText(value.Files[i].Path, replacements...), false)
	}
	value.Sources = redactStrings(value.Sources, replacements...)
	value.Reason = redactText(value.Reason, replacements...)
	return value
}

func publicSafeDeepDives(value signals.AnalysisDeepDives, replacements ...[]pathReplacement) signals.AnalysisDeepDives {
	ordinal := 1
	for i := range value.HighChurn {
		value.HighChurn[i].Path = privacy.RedactPath(redactText(value.HighChurn[i].Path, replacements...), false)
		value.HighChurn[i].NextAction = redactCommitLikeText(redactText(value.HighChurn[i].NextAction, replacements...))
		for j := range value.HighChurn[i].Artifacts {
			value.HighChurn[i].Artifacts[j] = publicSafeDeepDiveArtifact(value.HighChurn[i].Artifacts[j], &ordinal, replacements...)
		}
	}
	for i := range value.NoTestArtifacts {
		value.NoTestArtifacts[i].Artifact = publicSafeDeepDiveArtifact(value.NoTestArtifacts[i].Artifact, &ordinal, replacements...)
		value.NoTestArtifacts[i].Risk = redactCommitLikeText(redactText(value.NoTestArtifacts[i].Risk, replacements...))
		value.NoTestArtifacts[i].NextAction = redactCommitLikeText(redactText(value.NoTestArtifacts[i].NextAction, replacements...))
		for j := range value.NoTestArtifacts[i].ChangedSourceFiles {
			value.NoTestArtifacts[i].ChangedSourceFiles[j] = privacy.RedactPath(redactText(value.NoTestArtifacts[i].ChangedSourceFiles[j], replacements...), false)
		}
	}
	return value
}

func publicSafeDeepDiveArtifact(value signals.DeepDiveArtifact, ordinal *int, replacements ...[]pathReplacement) signals.DeepDiveArtifact {
	label := value.Label
	if !strings.HasPrefix(label, "PR #") {
		label = fmt.Sprintf("Artifact %d", *ordinal)
		*ordinal++
	}
	value.ID = ""
	value.Label = label
	value.Title = ""
	value.Scope = redactText(value.Scope, replacements...)
	value.TestEvidence = redactText(value.TestEvidence, replacements...)
	value.MainRisk = ""
	value.NextAction = redactCommitLikeText(redactText(value.NextAction, replacements...))
	return value
}

func publicSafeSetupActions(actions []signals.SetupAction, replacements ...[]pathReplacement) []signals.SetupAction {
	out := make([]signals.SetupAction, 0, len(actions))
	for _, action := range actions {
		action.Label = redactCommitLikeText(redactText(action.Label, replacements...))
		action.Command = redactCommitLikeText(redactText(action.Command, replacements...))
		action.Why = redactCommitLikeText(redactText(action.Why, replacements...))
		out = append(out, action)
	}
	return out
}

func publicSafeSignal(sig signals.Signal, sourceRepoID string, replacements ...[]pathReplacement) signals.Signal {
	privateSubjectID := sig.SubjectID
	if sig.RepoID == sourceRepoID {
		sig.RepoID = publicRepoID
	}
	if sig.SubjectID == sourceRepoID {
		sig.SubjectID = publicRepoID
	}
	if sig.SubjectType == "commit" {
		sig.SubjectID = ""
	}
	if sig.SubjectType == "file" && sig.SubjectID != "" {
		sig.SubjectID = privacy.RedactPath(sig.SubjectID, false)
	}
	if sig.FilePath != "" {
		sig.FilePath = privacy.RedactPath(sig.FilePath, false)
	}
	sig.Message = redactCommitLikeText(redactText(sig.Message, replacements...), privateSubjectID)
	sig.Evidence.ToolVersion = redactText(sig.Evidence.ToolVersion, replacements...)
	return sig
}

func redactFindings(findings []signals.Finding, replacements ...[]pathReplacement) []signals.Finding {
	out := make([]signals.Finding, 0, len(findings))
	for _, finding := range findings {
		finding.Label = redactCommitLikeText(redactText(finding.Label, replacements...))
		finding.Evidence = redactCommitLikeText(redactText(finding.Evidence, replacements...))
		finding.WhyItMatters = redactCommitLikeText(redactText(finding.WhyItMatters, replacements...))
		finding.NextAction = redactCommitLikeText(redactText(finding.NextAction, replacements...))
		out = append(out, finding)
	}
	return out
}

func redactStrings(values []string, replacements ...[]pathReplacement) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, redactCommitLikeText(redactText(value, replacements...)))
	}
	return out
}

func redactText(value string, replacements ...[]pathReplacement) string {
	if len(replacements) > 0 {
		for _, replacement := range replacements[0] {
			value = strings.ReplaceAll(value, replacement.private, replacement.public)
		}
	}
	return privacy.RedactSecretLikeText(value)
}

func redactCommitLikeText(value string, shas ...string) string {
	for _, sha := range shas {
		sha = strings.TrimSpace(sha)
		if sha == "" {
			continue
		}
		value = strings.ReplaceAll(value, sha, "commit")
		if len(sha) > 8 {
			value = strings.ReplaceAll(value, sha[:8], "commit")
		}
	}
	return commitSHAPattern.ReplaceAllString(value, "commit")
}

func publicSafePathReplacements(analysis signals.AnalysisReport) []pathReplacement {
	var replacements []pathReplacement
	seen := map[string]bool{}
	add := func(values ...string) {
		for _, value := range values {
			addPathReplacementsFromText(&replacements, seen, value)
		}
	}
	add(analysis.Repo.ID, analysis.Repo.Name, analysis.Repo.Root, analysis.Repo.RemoteURL, analysis.Repo.HeadSHA, analysis.Repo.GitHubOwner, analysis.Repo.GitHubRepo)
	add(analysis.Config.OutputDirectory)
	for _, tool := range analysis.Tooling.Tools {
		add(tool.Name, tool.Version, tool.Reason)
	}
	add(analysis.Tooling.Limitations...)
	for _, sig := range analysis.Signals {
		add(sig.RepoID, sig.SubjectID, sig.FilePath, sig.Message, sig.Evidence.ToolVersion)
	}
	for _, file := range analysis.Coverage.Files {
		add(file.Path)
	}
	add(analysis.Coverage.Sources...)
	add(analysis.Coverage.Reason)
	for _, card := range analysis.PRCards {
		add(card.Title, card.URL, card.Summary, card.Scope, card.TestEvidence, card.ReviewBurden, card.Durability, card.MainRisk, card.NextAction)
		for _, finding := range append(append([]signals.Finding{}, card.Strengths...), card.Risks...) {
			addFindingText(add, finding)
		}
		for _, evidence := range card.Evidence {
			add(evidence.ID, evidence.Message)
		}
	}
	for _, finding := range analysis.WeaknessMap.Strengths {
		addFindingText(add, finding)
	}
	for _, finding := range analysis.WeaknessMap.Weaknesses {
		addFindingText(add, finding)
	}
	for _, finding := range analysis.WeaknessMap.WatchItems {
		addFindingText(add, finding)
	}
	add(analysis.WeaknessMap.NextActions...)
	add(analysis.Profile.DisplayName, analysis.Profile.Headline)
	for _, finding := range analysis.Profile.Strengths {
		addFindingText(add, finding)
	}
	for _, finding := range analysis.Profile.ImprovementTrends {
		addFindingText(add, finding)
	}
	for _, badge := range analysis.Profile.BadgeCandidates {
		add(badge.ID, badge.Label)
	}
	for _, dive := range analysis.DeepDives.HighChurn {
		add(dive.Path, dive.NextAction)
		for _, artifact := range dive.Artifacts {
			addDeepDiveArtifactText(add, artifact)
		}
	}
	for _, dive := range analysis.DeepDives.NoTestArtifacts {
		addDeepDiveArtifactText(add, dive.Artifact)
		add(dive.ChangedSourceFiles...)
		add(dive.Risk, dive.NextAction)
	}
	for _, action := range analysis.SetupActions {
		add(action.ID, action.Label, action.Command, action.Why, action.ConfidenceImpact)
	}
	add(analysis.Limitations...)
	sort.Slice(replacements, func(i, j int) bool {
		return len(replacements[i].private) > len(replacements[j].private)
	})
	return replacements
}

func addFindingText(add func(...string), finding signals.Finding) {
	add(finding.Label, finding.Evidence, finding.WhyItMatters, finding.NextAction)
}

func addDeepDiveArtifactText(add func(...string), artifact signals.DeepDiveArtifact) {
	add(artifact.ID, artifact.Label, artifact.Title, artifact.Scope, artifact.TestEvidence, artifact.MainRisk, artifact.NextAction)
}

func addPathReplacementsFromText(replacements *[]pathReplacement, seen map[string]bool, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if isPathCandidate(value) {
		addPathReplacement(replacements, seen, value)
	}
	for _, candidate := range pathCandidatePattern.FindAllString(value, -1) {
		candidate = strings.Trim(candidate, ".,;:()[]{}<>\"'`")
		if isPathCandidate(candidate) {
			addPathReplacement(replacements, seen, candidate)
		}
	}
}

func isPathCandidate(value string) bool {
	return strings.Contains(value, "/") || strings.Contains(value, "\\")
}

func addPathReplacement(replacements *[]pathReplacement, seen map[string]bool, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	public := privacy.RedactPath(value, false)
	add := func(private string) {
		if private == "" || private == public || seen[private] {
			return
		}
		*replacements = append(*replacements, pathReplacement{private: private, public: public})
		seen[private] = true
	}
	add(value)
	add(filepath.ToSlash(value))
}

func firstStrings(values []string, limit int) []string {
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

func titleRisk(value string) string {
	if value == "" {
		return "Unknown"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
