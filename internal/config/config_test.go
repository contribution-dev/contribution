package config

import (
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
	if cfg.Privacy.UploadEnabled {
		t.Fatal("UploadEnabled = true, want false")
	}
	if cfg.AIUsage.AllowManualPRTags != true {
		t.Fatal("AllowManualPRTags = false, want true")
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
}
