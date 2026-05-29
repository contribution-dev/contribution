package coverage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/contribution-dev/contribution/internal/signals"
)

func TestParseGoCoverprofileAndChangedLineCoverage(t *testing.T) {
	repo := t.TempDir()
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
