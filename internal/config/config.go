// Package config loads and validates .contribution.yml.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
	Privacy   PrivacyConfig   `yaml:"privacy" json:"privacy"`
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
	SinceDays               int  `yaml:"since_days" json:"since_days"`
	MaxPRs                  int  `yaml:"max_prs" json:"max_prs"`
	IncludeUnmergedBranches bool `yaml:"include_unmerged_branches" json:"include_unmerged_branches"`
}

// PreflightConfig controls current-diff readiness policy.
type PreflightConfig struct {
	MaxFiles               int      `yaml:"max_files" json:"max_files"`
	MaxLines               int      `yaml:"max_lines" json:"max_lines"`
	RequireTestsForSource  bool     `yaml:"require_tests_for_source" json:"require_tests_for_source"`
	ChangedLineCoverageMin float64  `yaml:"changed_line_coverage_min" json:"changed_line_coverage_min"`
	RiskyPaths             []string `yaml:"risky_paths" json:"risky_paths"`
}

// PrivacyConfig controls private and public export behavior.
type PrivacyConfig struct {
	IncludeRawDiffs                    bool `yaml:"include_raw_diffs" json:"include_raw_diffs"`
	IncludePrivatePathsInPublicExports bool `yaml:"include_private_paths_in_public_exports" json:"include_private_paths_in_public_exports"`
	IncludeAuthorEmails                bool `yaml:"include_author_emails" json:"include_author_emails"`
	UploadEnabled                      bool `yaml:"upload_enabled" json:"upload_enabled"`
}

// AIUsageConfig stores self-reported AI workflow context.
type AIUsageConfig struct {
	SelfReportedTools []string               `yaml:"self_reported_tools" json:"self_reported_tools"`
	SelfReportedModes []string               `yaml:"self_reported_modes" json:"self_reported_modes"`
	AllowManualPRTags bool                   `yaml:"allow_manual_ai_pr_tags" json:"allow_manual_ai_pr_tags"`
	ManualPRTags      map[string]ManualPRTag `yaml:"manual_pr_tags,omitempty" json:"manual_pr_tags,omitempty"`
}

// ManualPRTag stores explicit user-provided AI tags.
type ManualPRTag struct {
	AIAssisted bool     `yaml:"ai_assisted" json:"ai_assisted"`
	Tools      []string `yaml:"tools" json:"tools"`
	Confidence string   `yaml:"confidence" json:"confidence"`
}

// ReportsConfig controls local report output.
type ReportsConfig struct {
	OutputDir            string `yaml:"output_dir" json:"output_dir"`
	PublicProfileDefault bool   `yaml:"public_profile_default" json:"public_profile_default"`
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
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	applyDefaults(&cfg)
	return cfg, Validate(cfg), nil
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
			SinceDays:               90,
			MaxPRs:                  20,
			IncludeUnmergedBranches: false,
		},
		Preflight: PreflightConfig{
			MaxFiles:               20,
			MaxLines:               800,
			RequireTestsForSource:  false,
			ChangedLineCoverageMin: 0,
			RiskyPaths:             []string{},
		},
		Privacy: PrivacyConfig{
			IncludeRawDiffs:                    false,
			IncludePrivatePathsInPublicExports: false,
			IncludeAuthorEmails:                false,
			UploadEnabled:                      false,
		},
		AIUsage: AIUsageConfig{
			SelfReportedTools: []string{},
			SelfReportedModes: []string{},
			AllowManualPRTags: true,
			ManualPRTags:      map[string]ManualPRTag{},
		},
		Reports: ReportsConfig{
			OutputDir:            ".contribution/reports",
			PublicProfileDefault: false,
		},
	}
	return cfg
}

// Validate returns non-fatal warnings for risky or unusual settings.
func Validate(cfg Config) []string {
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
	if cfg.Privacy.UploadEnabled {
		warnings = append(warnings, "privacy.upload_enabled is true, but the CLI does not implement upload.")
	}
	if cfg.Privacy.IncludeRawDiffs {
		warnings = append(warnings, "privacy.include_raw_diffs is true; public exports will still omit raw diffs.")
	}
	return warnings
}

// WriteDefault writes a default config to path.
func WriteDefault(path string, defaultBranch string) error {
	cfg := Default()
	if defaultBranch != "" {
		cfg.Project.DefaultBranch = defaultBranch
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal default config: %w", err)
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
		cfg.Preflight.RiskyPaths = []string{}
	}
	if cfg.AIUsage.SelfReportedTools == nil {
		cfg.AIUsage.SelfReportedTools = []string{}
	}
	if cfg.AIUsage.SelfReportedModes == nil {
		cfg.AIUsage.SelfReportedModes = []string{}
	}
	if cfg.AIUsage.ManualPRTags == nil {
		cfg.AIUsage.ManualPRTags = map[string]ManualPRTag{}
	}
	if cfg.Reports.OutputDir == "" {
		cfg.Reports.OutputDir = defaults.Reports.OutputDir
	}
}
