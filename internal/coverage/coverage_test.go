package coverage

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/contribution-dev/contribution/internal/signals"
)

func TestParseGoCoverprofileAndChangedLineCoverage(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module github.com/example/repo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cover := filepath.Join(repo, "coverage.out")
	if err := os.WriteFile(cover, []byte(`mode: set
internal/app/app.go:10.1,12.2 2 1
internal/app/app.go:13.1,14.2 1 0
github.com/example/repo/internal/app/extra.go:5.1,5.8 1 1
../outside.go:1.1,1.2 1 1
`), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := ParseFiles([]string{cover}, FormatGo, repo)
	if err != nil {
		t.Fatalf("ParseFiles() error = %v", err)
	}
	got := ComputeChangedLineCoverage(report, []ChangedFileInput{{
		Path: "internal/app/app.go",
		LineRanges: []signals.LineRange{
			{Start: 10, End: 10},
			{Start: 13, End: 13},
			{Start: 20, End: 20},
		},
	}})

	if got.Status != "available" {
		t.Fatalf("Status = %q, want available", got.Status)
	}
	if got.CoveredLines != 1 || got.TotalLines != 2 {
		t.Fatalf("coverage = %d/%d, want 1/2", got.CoveredLines, got.TotalLines)
	}
	if _, ok := report.Files["internal/app/extra.go"]; !ok {
		t.Fatalf("module-path coverage was not normalized: %+v", report.Files)
	}
}

func TestResolveInputsUsesExistingConfiguredCoverage(t *testing.T) {
	repo := t.TempDir()
	cover := filepath.Join(repo, "coverage.out")
	if err := os.WriteFile(cover, []byte("mode: set\n"), 0o600); err != nil {
		t.Fatalf("write coverage: %v", err)
	}

	paths, format, warnings := ResolveInputs(nil, "auto", repo, "coverage.out", "go")
	if len(paths) != 1 || paths[0] != cover {
		t.Fatalf("paths = %#v, want configured coverage path", paths)
	}
	if format != "go" {
		t.Fatalf("format = %q, want configured format", format)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
}

func TestResolveInputsWarnsWhenConfiguredCoverageMissing(t *testing.T) {
	paths, format, warnings := ResolveInputs(nil, "auto", t.TempDir(), "coverage.out", "go")
	if len(paths) != 0 {
		t.Fatalf("paths = %#v, want none", paths)
	}
	if format != "auto" {
		t.Fatalf("format = %q, want original auto without imported path", format)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "Configured coverage path coverage.out was not found") {
		t.Fatalf("warnings = %v, want missing configured coverage warning", warnings)
	}
}

func TestSplitCommandRejectsShellSyntax(t *testing.T) {
	if _, err := SplitCommand("go test ./... -coverprofile=coverage.out"); err != nil {
		t.Fatalf("SplitCommand() error = %v", err)
	}
	if got, err := SplitCommand(`go test "./..." -coverprofile=coverage.out`); err != nil || got[2] != "./..." {
		t.Fatalf("SplitCommand() = %#v/%v, want quoted argument", got, err)
	}
	for _, command := range []string{
		"go test ./... && rm -rf /",
		"go test ./... > coverage.out",
		"go test ./...\nrm -rf /",
	} {
		if _, err := SplitCommand(command); err == nil {
			t.Fatalf("SplitCommand(%q) error = nil, want rejection", command)
		}
	}
}

func TestRunCommandExecutesRepoLocalCoverageScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses POSIX sh")
	}
	repo := t.TempDir()
	script := filepath.Join(repo, "write-coverage.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'mode: set\\n' > coverage.out\n"), 0o600); err != nil {
		t.Fatalf("write coverage script: %v", err)
	}
	// #nosec G302 -- test fixture script must be executable inside a private temp dir.
	if err := os.Chmod(script, 0o700); err != nil {
		t.Fatalf("chmod coverage script: %v", err)
	}

	if err := RunCommand(context.Background(), repo, "./write-coverage.sh"); err != nil {
		t.Fatalf("RunCommand() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "coverage.out")); err != nil {
		t.Fatalf("coverage artifact was not written: %v", err)
	}
}

func TestSummarizeCoverageReport(t *testing.T) {
	report := Report{
		Sources: []string{"coverage.out"},
		Files: map[string]File{
			"internal/app.go": {Lines: map[int]bool{1: true, 2: false, 3: true}},
			"internal/api.go": {Lines: map[int]bool{10: false}},
		},
	}

	got := Summarize(report)
	if got.Status != "available" {
		t.Fatalf("Status = %q, want available", got.Status)
	}
	if got.CoveredLines != 2 || got.TotalLines != 4 || got.Percent != 50 {
		t.Fatalf("coverage = %d/%d %.1f, want 2/4 50", got.CoveredLines, got.TotalLines, got.Percent)
	}
	if len(got.Files) != 2 || got.Files[0].Path != "internal/api.go" || got.Files[1].Path != "internal/app.go" {
		t.Fatalf("files not sorted/stable: %+v", got.Files)
	}
	if len(got.LowCoverageFiles) != 2 || got.LowCoverageFiles[0].Path != "internal/api.go" {
		t.Fatalf("low coverage files = %+v", got.LowCoverageFiles)
	}
}

func TestParseLCOVNormalizesAbsolutePaths(t *testing.T) {
	repo := t.TempDir()
	source := filepath.Join(repo, "src", "index.ts")
	cover := filepath.Join(repo, "lcov.info")
	if err := os.MkdirAll(filepath.Dir(source), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cover, []byte("SF:"+source+`
DA:4,1
DA:5,0
end_of_record
`), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := ParseFiles([]string{cover}, FormatLCOV, repo)
	if err != nil {
		t.Fatalf("ParseFiles() error = %v", err)
	}
	got := ComputeChangedLineCoverage(report, []ChangedFileInput{{
		Path:       "src/index.ts",
		LineRanges: []signals.LineRange{{Start: 4, End: 5}},
	}})

	if got.CoveredLines != 1 || got.TotalLines != 2 {
		t.Fatalf("coverage = %d/%d, want 1/2", got.CoveredLines, got.TotalLines)
	}
}

func TestParseFilesReportsScannerErrors(t *testing.T) {
	repo := t.TempDir()
	cover := filepath.Join(repo, "coverage.out")
	longLine := "mode: set\n" + strings.Repeat("x", maxCoverageLineBytes+1)
	if err := os.WriteFile(cover, []byte(longLine), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := ParseFiles([]string{cover}, FormatGo, repo)

	if err == nil || !strings.Contains(err.Error(), "parse go coverage") {
		t.Fatalf("ParseFiles() error = %v, want scanner parse error", err)
	}
}

func TestParseFilesRejectsOversizedCoverageFile(t *testing.T) {
	repo := t.TempDir()
	cover := filepath.Join(repo, "coverage.out")
	// #nosec G304 -- test creates a coverage artifact under a private temp dir.
	file, err := os.Create(cover)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxCoverageFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = ParseFiles([]string{cover}, FormatGo, repo)

	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("ParseFiles() error = %v, want oversized coverage error", err)
	}
}

func TestParseFilesRejectsAbsurdCoverageRange(t *testing.T) {
	repo := t.TempDir()
	cover := filepath.Join(repo, "coverage.out")
	data := "mode: set\ninternal/app.go:1.1,200000.1 1 1\n"
	if err := os.WriteFile(cover, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := ParseFiles([]string{cover}, FormatGo, repo)

	if err == nil || !strings.Contains(err.Error(), "coverage range") {
		t.Fatalf("ParseFiles() error = %v, want range bound error", err)
	}
}

func TestChangedLineCoverageUnknownWhenNoRecordsMatch(t *testing.T) {
	report := Report{Files: map[string]File{
		"src/app.ts": {Lines: map[int]bool{1: true}},
	}}
	got := ComputeChangedLineCoverage(report, []ChangedFileInput{{
		Path:       "src/app.ts",
		LineRanges: []signals.LineRange{{Start: 10, End: 10}},
	}})

	if got.Status != "unknown" {
		t.Fatalf("Status = %q, want unknown", got.Status)
	}
	if got.TotalLines != 0 {
		t.Fatalf("TotalLines = %d, want 0", got.TotalLines)
	}
}

func TestChangedLineCoverageIsUnknownForAmbiguousSuffixMatch(t *testing.T) {
	report := Report{Files: map[string]File{
		"pkg/one/app.go": {Lines: map[int]bool{1: true}},
		"pkg/two/app.go": {Lines: map[int]bool{1: false}},
	}}

	got := ComputeChangedLineCoverage(report, []ChangedFileInput{{
		Path:       "app.go",
		LineRanges: []signals.LineRange{{Start: 1, End: 1}},
	}})

	if got.Status != "unknown" {
		t.Fatalf("Status = %q, want unknown", got.Status)
	}
	if got.TotalLines != 0 {
		t.Fatalf("TotalLines = %d, want ambiguous match to contribute no lines", got.TotalLines)
	}
}
