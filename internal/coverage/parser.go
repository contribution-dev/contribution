// Package coverage imports line coverage artifacts for changed-line evidence.
package coverage

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/contribution-dev/contribution/internal/signals"
)

// Format names a supported coverage artifact format.
type Format string

const (
	// FormatAuto detects coverage format from file content and extension.
	FormatAuto Format = "auto"
	// FormatGo is the Go coverprofile format.
	FormatGo Format = "go"
	// FormatLCOV is the LCOV tracefile format.
	FormatLCOV Format = "lcov"
)

// Report stores executable line coverage keyed by repository-relative path.
type Report struct {
	Files   map[string]File
	Sources []string
}

// File stores executable lines and whether each line was covered.
type File struct {
	Lines map[int]bool
}

// ValidateFormat checks supported coverage format names.
func ValidateFormat(format string) error {
	switch format {
	case "", string(FormatAuto), string(FormatGo), string(FormatLCOV):
		return nil
	default:
		return fmt.Errorf("unsupported coverage format %q", format)
	}
}

// ParseFiles imports coverage files. Unsupported or outside-repo entries are
// ignored; unreadable explicit coverage files return an error.
func ParseFiles(paths []string, format Format, repoRoot string) (Report, error) {
	report := Report{Files: map[string]File{}}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		parsed, detected, err := parseFile(path, format, repoRoot)
		if err != nil {
			return report, err
		}
		report.Sources = append(report.Sources, filepath.Base(path))
		_ = detected
		for filePath, file := range parsed.Files {
			existing := report.Files[filePath]
			if existing.Lines == nil {
				existing.Lines = map[int]bool{}
			}
			for line, covered := range file.Lines {
				existing.Lines[line] = existing.Lines[line] || covered
			}
			report.Files[filePath] = existing
		}
	}
	return report, nil
}

// Summarize turns imported coverage into whole-report coverage context.
func Summarize(report Report) signals.CoverageSummary {
	out := signals.CoverageSummary{
		Status:  "unknown",
		Sources: append([]string{}, report.Sources...),
	}
	if len(report.Files) == 0 {
		out.Reason = "No coverage records were imported."
		return out
	}
	paths := make([]string, 0, len(report.Files))
	for path := range report.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		file := report.Files[path]
		var covered int
		var total int
		for _, lineCovered := range file.Lines {
			total++
			if lineCovered {
				covered++
			}
		}
		if total == 0 {
			continue
		}
		out.Files = append(out.Files, signals.PreflightFileCoverage{
			Path:         path,
			CoveredLines: covered,
			TotalLines:   total,
			Percent:      percent(covered, total),
		})
		out.CoveredLines += covered
		out.TotalLines += total
	}
	if out.TotalLines == 0 {
		out.Reason = "Coverage was imported, but no executable coverage records were found."
		return out
	}
	out.Status = "available"
	out.Percent = percent(out.CoveredLines, out.TotalLines)
	out.Reason = ""
	return out
}

// ComputeChangedLineCoverage intersects imported coverage with changed new-side lines.
func ComputeChangedLineCoverage(report Report, changed []ChangedFileInput) signals.PreflightCoverage {
	if len(report.Files) == 0 {
		return signals.PreflightCoverage{Status: "unknown", Reason: "No coverage report was imported."}
	}
	out := signals.PreflightCoverage{Status: "unknown", Sources: append([]string{}, report.Sources...)}
	for _, file := range changed {
		coverageFile, ok := findCoverageFile(report.Files, file.Path)
		if !ok {
			continue
		}
		var covered int
		var total int
		for _, line := range changedLines(file.LineRanges) {
			lineCovered, executable := coverageFile.Lines[line]
			if !executable {
				continue
			}
			total++
			if lineCovered {
				covered++
			}
		}
		if total == 0 {
			continue
		}
		out.Files = append(out.Files, signals.PreflightFileCoverage{
			Path:         file.Path,
			CoveredLines: covered,
			TotalLines:   total,
			Percent:      percent(covered, total),
		})
		out.CoveredLines += covered
		out.TotalLines += total
	}
	if out.TotalLines == 0 {
		out.Reason = "Coverage was imported, but no coverage records matched changed executable lines."
		return out
	}
	out.Status = "available"
	out.Percent = percent(out.CoveredLines, out.TotalLines)
	out.Reason = ""
	return out
}

// ChangedFileInput is the minimal changed-file shape coverage needs.
type ChangedFileInput struct {
	Path       string
	LineRanges []signals.LineRange
}

func parseFile(path string, format Format, repoRoot string) (Report, Format, error) {
	// #nosec G304 -- coverage path is user-provided CLI input.
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, "", fmt.Errorf("read coverage %s: %w", path, err)
	}
	detected := format
	if detected == "" || detected == FormatAuto {
		detected = detectFormat(path, string(data))
	}
	switch detected {
	case FormatGo:
		return parseGoCoverprofile(string(data), repoRoot), detected, nil
	case FormatLCOV:
		return parseLCOV(string(data), repoRoot), detected, nil
	default:
		return Report{}, detected, fmt.Errorf("unsupported coverage format %q for %s", detected, path)
	}
}

func detectFormat(path string, data string) Format {
	if strings.HasPrefix(data, "mode: ") {
		return FormatGo
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".lcov" || strings.Contains(data, "\nSF:") || strings.HasPrefix(data, "SF:") {
		return FormatLCOV
	}
	return FormatGo
}

func parseGoCoverprofile(data string, repoRoot string) Report {
	report := Report{Files: map[string]File{}}
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pathRange := fields[0]
		colon := strings.LastIndex(pathRange, ":")
		if colon <= 0 {
			continue
		}
		path := normalizePath(pathRange[:colon], repoRoot)
		if path == "" {
			continue
		}
		rng := pathRange[colon+1:]
		comma := strings.Index(rng, ",")
		if comma <= 0 {
			continue
		}
		startLine := parseLeadingInt(rng[:comma])
		endLine := parseLeadingInt(rng[comma+1:])
		if startLine <= 0 || endLine <= 0 {
			continue
		}
		count, err := strconv.Atoi(fields[2])
		if err != nil {
			count = 0
		}
		addRange(&report, path, startLine, endLine, count > 0)
	}
	return report
}

func parseLCOV(data string, repoRoot string) Report {
	report := Report{Files: map[string]File{}}
	var current string
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "SF:"):
			current = normalizePath(strings.TrimPrefix(line, "SF:"), repoRoot)
		case strings.HasPrefix(line, "DA:") && current != "":
			parts := strings.Split(strings.TrimPrefix(line, "DA:"), ",")
			if len(parts) < 2 {
				continue
			}
			lineNumber, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil || lineNumber <= 0 {
				continue
			}
			count, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				count = 0
			}
			addLine(&report, current, lineNumber, count > 0)
		case line == "end_of_record":
			current = ""
		}
	}
	return report
}

func normalizePath(path string, repoRoot string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return ""
		}
		path = rel
	}
	path = filepath.ToSlash(filepath.Clean(path))
	if path == "." || strings.HasPrefix(path, "../") || strings.HasPrefix(path, "/") {
		return ""
	}
	return path
}

func addRange(report *Report, path string, startLine int, endLine int, covered bool) {
	if endLine < startLine {
		endLine = startLine
	}
	for line := startLine; line <= endLine; line++ {
		addLine(report, path, line, covered)
	}
}

func addLine(report *Report, path string, line int, covered bool) {
	file := report.Files[path]
	if file.Lines == nil {
		file.Lines = map[int]bool{}
	}
	file.Lines[line] = file.Lines[line] || covered
	report.Files[path] = file
}

func parseLeadingInt(value string) int {
	value = strings.TrimSpace(value)
	var digits strings.Builder
	for _, r := range value {
		if r < '0' || r > '9' {
			break
		}
		digits.WriteRune(r)
	}
	if digits.Len() == 0 {
		return 0
	}
	parsed, err := strconv.Atoi(digits.String())
	if err != nil {
		return 0
	}
	return parsed
}

func findCoverageFile(files map[string]File, changedPath string) (File, bool) {
	changedPath = filepath.ToSlash(filepath.Clean(changedPath))
	if file, ok := files[changedPath]; ok {
		return file, true
	}
	for path, file := range files {
		if strings.HasSuffix(path, "/"+changedPath) {
			return file, true
		}
	}
	return File{}, false
}

func changedLines(ranges []signals.LineRange) []int {
	var out []int
	for _, rng := range ranges {
		start := rng.Start
		end := rng.End
		if start <= 0 {
			continue
		}
		if end < start {
			end = start
		}
		for line := start; line <= end; line++ {
			out = append(out, line)
		}
	}
	return out
}

func percent(covered int, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(covered) * 100 / float64(total)
}
