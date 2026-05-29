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
	fmt.Fprintln(buf, "| PR | Label | Confidence | Scope | Test evidence | Review burden | Durability | Main risk | Next action |")
	fmt.Fprintln(buf, "| --- | --- | --- | --- | --- | --- | --- | --- | --- |")
	for _, card := range cards {
		pr := "commit"
		if card.PRNumber > 0 {
			pr = fmt.Sprintf("#%d", card.PRNumber)
		}
		fmt.Fprintf(buf, "| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			escapeTable(pr),
			escapeTable(card.Label),
			escapeTable(string(card.Confidence)),
			escapeTable(card.Scope),
			escapeTable(card.TestEvidence),
			escapeTable(card.ReviewBurden),
			escapeTable(card.Durability),
			escapeTable(ledgerCell(card.MainRisk, "No specific risk recorded.")),
			escapeTable(ledgerCell(card.NextAction, "No specific action recorded.")),
		)
	}
}

func ledgerCell(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func testEvidence(analysis signals.AnalysisReport) string {
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
