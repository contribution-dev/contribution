package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingConfigUsesSafeDefaults(t *testing.T) {
	cfg, warnings, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Analysis.SinceDays != 90 {
		t.Fatalf("SinceDays = %d, want 90", cfg.Analysis.SinceDays)
	}
	if cfg.Preflight.MaxFiles != 20 || cfg.Preflight.MaxLines != 800 {
		t.Fatalf("Preflight limits = %d files/%d lines, want 20/800", cfg.Preflight.MaxFiles, cfg.Preflight.MaxLines)
	}
	if len(warnings) == 0 {
		t.Fatal("expected missing config warning")
	}
}

func TestWriteDefaultAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := WriteDefault(path, "trunk"); err != nil {
		t.Fatalf("WriteDefault() error = %v", err)
	}
	cfg, _, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Project.DefaultBranch != "trunk" {
		t.Fatalf("DefaultBranch = %q, want trunk", cfg.Project.DefaultBranch)
	}
	if cfg.Reports.OutputDir != ".contribution/reports" {
		t.Fatalf("OutputDir = %q", cfg.Reports.OutputDir)
	}
	if cfg.Preflight.RiskyPaths == nil {
		t.Fatal("Preflight.RiskyPaths = nil, want empty slice")
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("version: 1\nprivacy:\n  upload_enabled: true\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, _, err := Load(dir); err == nil {
		t.Fatal("Load() error = nil, want unknown field error")
	}
}

func TestLoadWarnsBeforeApplyingDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("version: 1\nanalysis:\n  since_days: -1\n  max_prs: 0\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, warnings, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Analysis.SinceDays != 90 || cfg.Analysis.MaxPRs != 20 {
		t.Fatalf("defaults = since_days %d max_prs %d, want 90/20", cfg.Analysis.SinceDays, cfg.Analysis.MaxPRs)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %#v, want invalid since_days and max_prs warnings", warnings)
	}
}
