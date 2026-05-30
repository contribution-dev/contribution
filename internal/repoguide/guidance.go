// Package repoguide builds repo-aware user guidance.
package repoguide

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/contribution-dev/contribution/internal/config"
)

// CoverageAnalyzeCommand returns the best available command for importing
// supported coverage evidence in an analyze report.
func CoverageAnalyzeCommand(repoRoot string, coverageConfig config.CoverageConfig) string {
	if coverageConfig.Command != "" && coverageConfig.Path != "" {
		format := coverageConfig.Format
		if format == "" {
			format = "auto"
		}
		return fmt.Sprintf("%s && contribution analyze --repo . --coverage %s --coverage-format %s --format all", coverageConfig.Command, coverageConfig.Path, format)
	}
	switch {
	case hasFile(repoRoot, "go.mod"):
		return "go test ./... -coverprofile=coverage.out && contribution analyze --repo . --coverage coverage.out --coverage-format go --format all"
	case hasFile(repoRoot, "package.json"):
		return "contribution analyze --repo . --coverage coverage/lcov.info --coverage-format lcov --format all"
	default:
		return "contribution analyze --repo . --coverage <path> --coverage-format go|lcov --format all"
	}
}

// CoverageDoctorStep returns repo-aware next-step guidance for coverage import.
func CoverageDoctorStep(repoRoot string) string {
	switch {
	case hasFile(repoRoot, "go.mod"):
		return "Run `go test ./... -coverprofile=coverage.out` and pass `--coverage coverage.out --coverage-format go`, or configure coverage.command and use `preflight --run-coverage`."
	case hasFile(repoRoot, "package.json"):
		return "Run your repo's coverage command, then pass `--coverage coverage/lcov.info --coverage-format lcov` to import LCOV coverage."
	default:
		return "Generate a Go coverprofile or LCOV report, then pass `--coverage <path> --coverage-format go|lcov` to import coverage."
	}
}

// CoverageWhy explains the coverage setup action without assuming a language.
func CoverageWhy(repoRoot string) string {
	prefix := "Coverage import lets the report distinguish test-file presence from executable-line coverage."
	switch {
	case hasFile(repoRoot, "go.mod"):
		return prefix + " Reuse coverage.out with preflight for changed-line coverage on behavior diffs."
	case hasFile(repoRoot, "package.json"):
		return prefix + " Generate LCOV with your repo's coverage command first, then reuse the LCOV artifact with preflight for changed-line coverage on behavior diffs."
	default:
		return prefix + " Generate a supported coverage artifact first, then reuse it with preflight for changed-line coverage on behavior diffs."
	}
}

func hasFile(root string, relativePath string) bool {
	if root == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(root, relativePath))
	return err == nil && !info.IsDir()
}
