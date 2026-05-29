package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkInventory(b *testing.B) {
	repoPath := b.TempDir()
	runBenchmarkGit(b, repoPath, "init", "-b", "main")
	runBenchmarkGit(b, repoPath, "config", "user.email", "bench@example.test")
	runBenchmarkGit(b, repoPath, "config", "user.name", "Bench User")
	for i := 0; i < 120; i++ {
		writeBenchmarkFile(b, repoPath, fmt.Sprintf("internal/pkg%d/service.go", i%20), "package pkg\n\nfunc Value() int { return 1 }\n")
		writeBenchmarkFile(b, repoPath, fmt.Sprintf("internal/pkg%d/service_test.go", i%20), "package pkg\n")
	}
	writeBenchmarkFile(b, repoPath, "README.md", "# Bench\n")
	writeBenchmarkFile(b, repoPath, ".gitignore", "bin/\n")
	runBenchmarkGit(b, repoPath, "add", ".")
	runBenchmarkGit(b, repoPath, "commit", "-m", "fixture")
	writeBenchmarkFile(b, repoPath, "internal/untracked.go", "package internal\n")
	writeBenchmarkFile(b, repoPath, ".contribution/reports/run/analysis.json", "{}\n")
	writeBenchmarkFile(b, repoPath, "bin/ignored", "ignored\n")

	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		summary, signals, err := Inventory(ctx, repoPath, "local:bench", now)
		if err != nil {
			b.Fatal(err)
		}
		if summary.TotalFiles == 0 || len(signals) == 0 {
			b.Fatalf("empty inventory: %+v", summary)
		}
	}
}

func runBenchmarkGit(tb testing.TB, dir string, args ...string) {
	tb.Helper()
	// #nosec G204 -- benchmark invokes the fixed git binary with deterministic fixture arguments.
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		tb.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeBenchmarkFile(tb testing.TB, repoPath string, relativePath string, content string) {
	tb.Helper()
	target := filepath.Join(repoPath, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
		tb.Fatal(err)
	}
}
