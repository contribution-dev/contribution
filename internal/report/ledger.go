package report

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/contribution-dev/contribution/internal/signals"
)

func writeLedger(buf *bytes.Buffer, cards []signals.PRQualityCard) {
	if len(cards) == 0 {
		fmt.Fprintln(buf, "No PR or commit-group cards were available.")
		return
	}
	fmt.Fprintln(buf, "| Artifact | Label | Confidence | Scope | Test evidence | Review burden | Durability | Main risk | Why | Next action |")
	fmt.Fprintln(buf, "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |")
	for _, card := range cards {
		fmt.Fprintf(buf, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			escapeTable(artifactLabel(card)),
			escapeTable(card.Label),
			escapeTable(string(card.Confidence)),
			escapeTable(card.Scope),
			escapeTable(card.TestEvidence),
			escapeTable(card.ReviewBurden),
			escapeTable(card.Durability),
			escapeTable(ledgerCell(card.MainRisk, "No specific risk recorded.")),
			escapeTable(ledgerCell(riskCause(card), "No specific cause recorded.")),
			escapeTable(ledgerCell(card.NextAction, "No specific action recorded.")),
		)
	}
}

func artifactLabel(card signals.PRQualityCard) string {
	title := strings.TrimSpace(card.Title)
	if card.PRNumber > 0 {
		if title == fmt.Sprintf("PR #%d", card.PRNumber) {
			return title
		}
		if title == "" {
			return fmt.Sprintf("#%d", card.PRNumber)
		}
		return fmt.Sprintf("#%d: %s", card.PRNumber, title)
	}
	if strings.HasPrefix(title, "Artifact ") {
		return title
	}
	if title == "" {
		return "commit"
	}
	return "commit: " + title
}

func ledgerCell(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func riskCause(card signals.PRQualityCard) string {
	if len(card.Risks) > 0 {
		risk := card.Risks[0]
		if risk.Evidence != "" {
			return risk.Evidence
		}
		return risk.Label
	}
	if len(card.Strengths) > 0 {
		strength := card.Strengths[0]
		if strength.Evidence != "" {
			return strength.Evidence
		}
		return strength.Label
	}
	if len(card.Evidence) > 0 {
		return card.Evidence[0].Message
	}
	return card.Summary
}

func writeHighChurnDeepDive(buf *bytes.Buffer, dives []signals.HighChurnDeepDive) {
	if len(dives) == 0 {
		fmt.Fprintln(buf, "No high-churn file crossed the current threshold.")
		return
	}
	for _, dive := range dives {
		fmt.Fprintf(buf, "### %s\n\n", dive.Path)
		fmt.Fprintf(buf, "Touched %d times in the analysis window (%s confidence).\n\n", dive.Touches, dive.Confidence)
		if len(dive.Artifacts) > 0 {
			fmt.Fprintln(buf, "Recent artifacts:")
			for _, artifact := range dive.Artifacts {
				fmt.Fprintf(buf, "- %s: %s", artifactLabelForDeepDive(artifact), ledgerCell(artifact.Scope, "scope unavailable"))
				if artifact.MainRisk != "" {
					fmt.Fprintf(buf, "; risk: %s", artifact.MainRisk)
				}
				fmt.Fprintln(buf)
			}
			fmt.Fprintln(buf)
		}
		fmt.Fprintf(buf, "Next action: %s\n\n", dive.NextAction)
	}
}

func writeNoTestDeepDive(buf *bytes.Buffer, dives []signals.NoTestArtifactDeepDive) {
	if len(dives) == 0 {
		fmt.Fprintln(buf, "No source-changing artifact without test-file evidence was found in the analysis window.")
		return
	}
	for _, dive := range dives {
		artifact := dive.Artifact
		fmt.Fprintf(buf, "### %s\n\n", artifactLabelForDeepDive(artifact))
		if len(dive.ChangedSourceFiles) > 0 {
			fmt.Fprintf(buf, "Changed source files: %s.\n\n", strings.Join(dive.ChangedSourceFiles, ", "))
		}
		if artifact.Scope != "" {
			fmt.Fprintf(buf, "Scope: %s.\n\n", artifact.Scope)
		}
		fmt.Fprintf(buf, "Risk: %s\n\n", dive.Risk)
		fmt.Fprintf(buf, "Next action: %s\n\n", dive.NextAction)
	}
}

func artifactLabelForDeepDive(artifact signals.DeepDiveArtifact) string {
	label := strings.TrimSpace(artifact.Label)
	title := strings.TrimSpace(artifact.Title)
	if title == "" {
		if label == "" {
			return "Artifact"
		}
		return label
	}
	if label == "" {
		return title
	}
	return label + ": " + title
}

func writeSetupActions(buf *bytes.Buffer, actions []signals.SetupAction) {
	if len(actions) == 0 {
		fmt.Fprintln(buf, "- No confidence-raising setup actions were identified.")
		return
	}
	for _, action := range actions {
		if action.Command != "" {
			fmt.Fprintf(buf, "- %s: `%s`. %s Confidence impact: %s.\n", action.Label, action.Command, action.Why, action.ConfidenceImpact)
		} else {
			fmt.Fprintf(buf, "- %s: %s Confidence impact: %s.\n", action.Label, action.Why, action.ConfidenceImpact)
		}
	}
}

func testEvidence(analysis signals.AnalysisReport) string {
	if analysis.Coverage.Status == "available" {
		return fmt.Sprintf("Repository inventory found %d test files and %d source files. Imported coverage covers %.1f%% of executable lines in %d file(s).", analysis.Inventory.TestFiles, analysis.Inventory.SourceFiles, analysis.Coverage.Percent, len(analysis.Coverage.Files))
	}
	if analysis.Inventory.TestFiles == 0 {
		return "No test files were found in the repository inventory. This does not prove behavior is untested, but this report has no coverage import or test-file evidence to rely on."
	}
	return fmt.Sprintf("Repository inventory found %d test files and %d source files. No coverage report was imported, so this is based on test-file evidence only.", analysis.Inventory.TestFiles, analysis.Inventory.SourceFiles)
}

func durability(analysis signals.AnalysisReport) string {
	highChurn := 0
	fixLike := 0
	for _, sig := range analysis.Signals {
		switch sig.Type {
		case "high_churn_file":
			highChurn++
		case "follow_up_fix_commit", "revert_commit":
			fixLike++
		}
	}
	if highChurn == 0 && fixLike == 0 {
		return "No strong churn or revert pattern was detected in the local analysis window. PR-level post-merge churn needs GitHub file metadata and is unavailable when PR metadata is missing."
	}
	return fmt.Sprintf("The analysis found %d high-churn file signals and %d low-confidence fix or revert signals. Treat message-based fix signals as directional, not proof.", highChurn, fixLike)
}
