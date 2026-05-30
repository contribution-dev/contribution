// Package config loads and validates .contribution.yml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	coveragepkg "github.com/contribution-dev/contribution/internal/coverage"
	"gopkg.in/yaml.v3"
)

const (
	// FileName is the repo-local config file.
	FileName = ".contribution.yml"
)

// Config is the editable V1 repository configuration.
type Config struct {
	Version   int             `yaml:"version" json:"version"`
	Project   ProjectConfig   `yaml:"project" json:"project"`
	Analysis  AnalysisConfig  `yaml:"analysis" json:"analysis"`
	Preflight PreflightConfig `yaml:"preflight" json:"preflight"`
	Coverage  CoverageConfig  `yaml:"coverage" json:"coverage"`
	AIUsage   AIUsageConfig   `yaml:"ai_usage" json:"ai_usage"`
	Reports   ReportsConfig   `yaml:"reports" json:"reports"`
}

// ProjectConfig contains project identity settings.
type ProjectConfig struct {
	Name          string `yaml:"name" json:"name"`
	DefaultBranch string `yaml:"default_branch" json:"default_branch"`
}

// AnalysisConfig controls local analysis scope.
type AnalysisConfig struct {
	SinceDays int `yaml:"since_days" json:"since_days"`
	MaxPRs    int `yaml:"max_prs" json:"max_prs"`
}

// PreflightConfig controls current-diff readiness policy.
type PreflightConfig struct {
	MaxFiles               int      `yaml:"max_files" json:"max_files"`
	MaxLines               int      `yaml:"max_lines" json:"max_lines"`
	RequireTestsForSource  bool     `yaml:"require_tests_for_source" json:"require_tests_for_source"`
	ChangedLineCoverageMin float64  `yaml:"changed_line_coverage_min" json:"changed_line_coverage_min"`
	RiskyPaths             []string `yaml:"risky_paths" json:"risky_paths"`
}

// CoverageConfig stores repo-specific coverage import guidance.
type CoverageConfig struct {
	Command string `yaml:"command" json:"command"`
	Path    string `yaml:"path" json:"path"`
	Format  string `yaml:"format" json:"format"`
}

// AIUsageConfig stores self-reported AI workflow context.
type AIUsageConfig struct {
	SelfReportedTools []string `yaml:"self_reported_tools" json:"self_reported_tools"`
	SelfReportedModes []string `yaml:"self_reported_modes" json:"self_reported_modes"`
}

// ReportsConfig controls local report output.
type ReportsConfig struct {
	OutputDir string `yaml:"output_dir" json:"output_dir"`
}

// Load reads a config file from repoRoot, returning defaults when it is absent.
func Load(repoRoot string) (Config, []string, error) {
	path := filepath.Join(repoRoot, FileName)
	cfg := Default()
	// #nosec G304 -- path is constrained to the repository-local config file.
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, []string{"No .contribution.yml found; using safe defaults."}, nil
	}
	if err != nil {
		return cfg, nil, fmt.Errorf("read %s: %w", path, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	warnings := validate(cfg)
	applyDefaults(&cfg)
	return cfg, warnings, nil
}

// Default returns safe private-by-default settings.
func Default() Config {
	cfg := Config{
		Version: 1,
		Project: ProjectConfig{
			Name:          "",
			DefaultBranch: "main",
		},
		Analysis: AnalysisConfig{
			SinceDays: 90,
			MaxPRs:    20,
		},
		Preflight: PreflightConfig{
			MaxFiles:               20,
			MaxLines:               800,
			RequireTestsForSource:  false,
			ChangedLineCoverageMin: 0,
			RiskyPaths: []string{
				"internal/auth/",
				"internal/security/",
				"internal/billing/",
				"internal/payments/",
				"app/api/auth/",
				"apps/app/app/api/auth/",
			},
		},
		Coverage: CoverageConfig{
			Command: "",
			Path:    "",
			Format:  "auto",
		},
		AIUsage: AIUsageConfig{
			SelfReportedTools: []string{},
			SelfReportedModes: []string{},
		},
		Reports: ReportsConfig{
			OutputDir: ".contribution/reports",
		},
	}
	return cfg
}

func validate(cfg Config) []string {
	var warnings []string
	if cfg.Version != 1 {
		warnings = append(warnings, fmt.Sprintf("Config version %d is not supported; continuing with best effort.", cfg.Version))
	}
	if cfg.Analysis.SinceDays <= 0 {
		warnings = append(warnings, "analysis.since_days must be positive; defaulting to 90.")
	}
	if cfg.Analysis.MaxPRs <= 0 {
		warnings = append(warnings, "analysis.max_prs must be positive; defaulting to 20.")
	}
	if cfg.Preflight.MaxFiles < 0 {
		warnings = append(warnings, "preflight.max_files must be zero or positive; defaulting to 20.")
	}
	if cfg.Preflight.MaxLines < 0 {
		warnings = append(warnings, "preflight.max_lines must be zero or positive; defaulting to 800.")
	}
	if cfg.Preflight.ChangedLineCoverageMin < 0 || cfg.Preflight.ChangedLineCoverageMin > 100 {
		warnings = append(warnings, "preflight.changed_line_coverage_min must be between 0 and 100; ignoring the configured threshold.")
	}
	if !coveragepkg.IsSupportedFormat(cfg.Coverage.Format) {
		warnings = append(warnings, "coverage.format must be auto, go, or lcov; defaulting to auto.")
	}
	return warnings
}

// Suggested returns safe defaults with repo-specific local guidance.
func Suggested(repoRoot string, defaultBranch string) Config {
	cfg := Default()
	if defaultBranch != "" {
		cfg.Project.DefaultBranch = defaultBranch
	}
	if hasFile(repoRoot, "go.mod") {
		cfg.Coverage.Command = "go test ./... -coverprofile=coverage.out"
		cfg.Coverage.Path = "coverage.out"
		cfg.Coverage.Format = "go"
	}
	return cfg
}

// WriteDefault writes a default config to path.
func WriteDefault(path string, defaultBranch string) error {
	cfg := Suggested(filepath.Dir(path), defaultBranch)
	return Write(path, cfg)
}

// Write writes a config file.
func Write(path string, cfg Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func applyDefaults(cfg *Config) {
	defaults := Default()
	if cfg.Version == 0 {
		cfg.Version = defaults.Version
	}
	if cfg.Project.DefaultBranch == "" {
		cfg.Project.DefaultBranch = defaults.Project.DefaultBranch
	}
	if cfg.Analysis.SinceDays <= 0 {
		cfg.Analysis.SinceDays = defaults.Analysis.SinceDays
	}
	if cfg.Analysis.MaxPRs <= 0 {
		cfg.Analysis.MaxPRs = defaults.Analysis.MaxPRs
	}
	if cfg.Preflight.MaxFiles < 0 {
		cfg.Preflight.MaxFiles = defaults.Preflight.MaxFiles
	}
	if cfg.Preflight.MaxLines < 0 {
		cfg.Preflight.MaxLines = defaults.Preflight.MaxLines
	}
	if cfg.Preflight.RiskyPaths == nil {
		cfg.Preflight.RiskyPaths = defaults.Preflight.RiskyPaths
	}
	if !coveragepkg.IsSupportedFormat(cfg.Coverage.Format) {
		cfg.Coverage.Format = defaults.Coverage.Format
	}
	if cfg.AIUsage.SelfReportedTools == nil {
		cfg.AIUsage.SelfReportedTools = []string{}
	}
	if cfg.AIUsage.SelfReportedModes == nil {
		cfg.AIUsage.SelfReportedModes = []string{}
	}
	if cfg.Reports.OutputDir == "" {
		cfg.Reports.OutputDir = defaults.Reports.OutputDir
	}
}

func hasFile(root string, relativePath string) bool {
	if root == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(root, relativePath))
	return err == nil && !info.IsDir()
}
