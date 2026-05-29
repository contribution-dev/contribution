// Package signals defines the normalized analysis data model.
package signals

import "time"

// Confidence describes how directly the available evidence supports a claim.
type Confidence string

const (
	// ConfidenceLow means evidence is indirect or incomplete.
	ConfidenceLow Confidence = "low"
	// ConfidenceMedium means evidence is useful but has meaningful uncertainty.
	ConfidenceMedium Confidence = "medium"
	// ConfidenceHigh means evidence directly supports the claim.
	ConfidenceHigh Confidence = "high"
)

// Severity describes the importance of a signal.
type Severity string

const (
	// SeverityInfo is contextual and not a concern by itself.
	SeverityInfo Severity = "info"
	// SeverityLow is a minor concern.
	SeverityLow Severity = "low"
	// SeverityMedium is a moderate concern.
	SeverityMedium Severity = "medium"
	// SeverityHigh is an important concern.
	SeverityHigh Severity = "high"
	// SeverityCritical is a blocking or urgent concern.
	SeverityCritical Severity = "critical"
)

// Direction describes whether a signal is favorable, unfavorable, or context.
type Direction string

const (
	// DirectionPositive indicates favorable evidence.
	DirectionPositive Direction = "positive"
	// DirectionNegative indicates unfavorable evidence.
	DirectionNegative Direction = "negative"
	// DirectionNeutral indicates context without positive or negative direction.
	DirectionNeutral Direction = "neutral"
)

// LineRange points at a span in a file when line evidence is safe to expose.
type LineRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Evidence stores provenance for a signal without embedding source code.
type Evidence struct {
	URL         string `json:"url,omitempty"`
	CommitSHA   string `json:"commit_sha,omitempty"`
	PRNumber    int    `json:"pr_number,omitempty"`
	ToolVersion string `json:"tool_version,omitempty"`
}

// Signal is the normalized fact emitted by all V1 analyzers.
type Signal struct {
	ID          string     `json:"id"`
	RepoID      string     `json:"repo_id"`
	Source      string     `json:"source"`
	Type        string     `json:"type"`
	SubjectType string     `json:"subject_type"`
	SubjectID   string     `json:"subject_id,omitempty"`
	FilePath    string     `json:"file_path,omitempty"`
	LineRange   *LineRange `json:"line_range,omitempty"`
	Severity    Severity   `json:"severity"`
	Direction   Direction  `json:"direction"`
	Confidence  Confidence `json:"confidence"`
	Value       float64    `json:"value,omitempty"`
	Unit        string     `json:"unit,omitempty"`
	Message     string     `json:"message"`
	Evidence    Evidence   `json:"evidence"`
	PublicSafe  bool       `json:"is_public_safe"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ToolAvailability records an external tool status.
type ToolAvailability struct {
	Name      string `json:"name"`
	Required  bool   `json:"required"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// ToolingReport summarizes required and optional runtime tooling.
type ToolingReport struct {
	GeneratedAt time.Time          `json:"generated_at"`
	Tools       []ToolAvailability `json:"tools"`
	Limitations []string           `json:"limitations,omitempty"`
}

// RepoMetadata describes the analyzed repository.
type RepoMetadata struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Root          string `json:"root,omitempty"`
	RemoteURL     string `json:"remote_url,omitempty"`
	DefaultBranch string `json:"default_branch,omitempty"`
	HeadSHA       string `json:"head_sha,omitempty"`
	IsRemoteClone bool   `json:"is_remote_clone"`
	GitHubOwner   string `json:"github_owner,omitempty"`
	GitHubRepo    string `json:"github_repo,omitempty"`
}

// AnalysisConfigSnapshot captures the effective settings for a run.
type AnalysisConfigSnapshot struct {
	SinceDays                int      `json:"since_days"`
	MaxPRs                   int      `json:"max_prs"`
	IncludeUnmergedBranches  bool     `json:"include_unmerged_branches"`
	PublicSafe               bool     `json:"public_safe"`
	NoExternalTools          bool     `json:"no_external_tools"`
	SelfReportedAITools      []string `json:"self_reported_ai_tools,omitempty"`
	SelfReportedAIModes      []string `json:"self_reported_ai_modes,omitempty"`
	AllowManualAIPRTags      bool     `json:"allow_manual_ai_pr_tags"`
	OutputDirectory          string   `json:"output_directory"`
	GitHubMetadataConfigured bool     `json:"github_metadata_configured"`
}

// PrivacySummary records what the CLI did and did not expose.
type PrivacySummary struct {
	PublicSafe                         bool `json:"public_safe"`
	RawCodeIncluded                    bool `json:"raw_code_included"`
	RawDiffsIncluded                   bool `json:"raw_diffs_included"`
	PrivatePathsIncludedInPublicExport bool `json:"private_paths_included_in_public_export"`
	AuthorEmailsIncluded               bool `json:"author_emails_included"`
	UploadEnabled                      bool `json:"upload_enabled"`
}

// Finding is a human-readable conclusion with evidence and confidence.
type Finding struct {
	Label        string     `json:"label"`
	Evidence     string     `json:"evidence"`
	Confidence   Confidence `json:"confidence"`
	WhyItMatters string     `json:"why_it_matters,omitempty"`
	NextAction   string     `json:"next_action,omitempty"`
}

// SignalRef points from higher-level reports back to source evidence.
type SignalRef struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

// PRQualityCard labels a PR or commit group as an analyzed artifact.
type PRQualityCard struct {
	PRNumber     int         `json:"pr_number,omitempty"`
	Title        string      `json:"title"`
	URL          string      `json:"url,omitempty"`
	Label        string      `json:"quality_label"`
	Confidence   Confidence  `json:"confidence"`
	Summary      string      `json:"summary"`
	Scope        string      `json:"scope"`
	TestEvidence string      `json:"test_evidence"`
	ReviewBurden string      `json:"review_burden"`
	Durability   string      `json:"durability"`
	MainRisk     string      `json:"main_risk"`
	Strengths    []Finding   `json:"strengths"`
	Risks        []Finding   `json:"risks"`
	Evidence     []SignalRef `json:"evidence"`
	NextAction   string      `json:"next_action"`
}

// WeaknessMap summarizes repeated strengths, weaknesses, and watch items.
type WeaknessMap struct {
	Strengths   []Finding  `json:"strengths"`
	Weaknesses  []Finding  `json:"weaknesses"`
	WatchItems  []Finding  `json:"watch_items"`
	NextActions []string   `json:"next_actions"`
	Confidence  Confidence `json:"confidence"`
}

// BadgeCandidate is a non-authoritative public profile badge suggestion.
type BadgeCandidate struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	Confidence Confidence `json:"confidence"`
}

// ProfileSummary is the internal profile summary before export redaction.
type ProfileSummary struct {
	DisplayName        string           `json:"display_name,omitempty"`
	Headline           string           `json:"headline"`
	AnalyzedPRs        int              `json:"analyzed_prs"`
	AnalysisWindowDays int              `json:"analysis_window_days"`
	Confidence         Confidence       `json:"confidence"`
	Strengths          []Finding        `json:"strengths"`
	ImprovementTrends  []Finding        `json:"improvement_trends"`
	BadgeCandidates    []BadgeCandidate `json:"badge_candidates"`
}

// FileSummary is a compact repo or diff inventory.
type FileSummary struct {
	TotalFiles      int            `json:"total_files"`
	ByClass         map[string]int `json:"by_class"`
	ByLanguage      map[string]int `json:"by_language"`
	TestFiles       int            `json:"test_files"`
	SourceFiles     int            `json:"source_files"`
	DocsFiles       int            `json:"docs_files"`
	DependencyFiles int            `json:"dependency_files"`
	ConfigFiles     int            `json:"config_files"`
	GeneratedFiles  int            `json:"generated_files"`
	VendorFiles     int            `json:"vendor_files"`
	RiskyFiles      int            `json:"risky_files"`
}

// AnalysisReport is the canonical machine-readable V1 output.
type AnalysisReport struct {
	Version     int                    `json:"version"`
	GeneratedAt time.Time              `json:"generated_at"`
	Repo        RepoMetadata           `json:"repo"`
	Config      AnalysisConfigSnapshot `json:"config"`
	Tooling     ToolingReport          `json:"tooling"`
	Inventory   FileSummary            `json:"inventory"`
	Signals     []Signal               `json:"signals"`
	PRCards     []PRQualityCard        `json:"pr_quality_cards"`
	WeaknessMap WeaknessMap            `json:"weakness_map"`
	Profile     ProfileSummary         `json:"profile"`
	Limitations []string               `json:"limitations"`
	Privacy     PrivacySummary         `json:"privacy"`
}

// ProfileExport is the public-safe profile artifact consumed by a future web app.
type ProfileExport struct {
	Version     int       `json:"version"`
	GeneratedAt time.Time `json:"generated_at"`
	Profile     struct {
		DisplayName string `json:"display_name"`
		Headline    string `json:"headline"`
		Visibility  string `json:"visibility"`
	} `json:"profile"`
	Summary struct {
		AnalyzedPRs        int        `json:"analyzed_prs"`
		AnalysisWindowDays int        `json:"analysis_window_days"`
		Confidence         Confidence `json:"confidence"`
	} `json:"summary"`
	Strengths         []Finding        `json:"strengths"`
	ImprovementTrends []Finding        `json:"improvement_trends"`
	BadgeCandidates   []BadgeCandidate `json:"badge_candidates"`
	SelectedArtifacts []PRQualityCard  `json:"selected_artifacts"`
	Redaction         struct {
		PublicSafe           bool `json:"public_safe"`
		RawCodeIncluded      bool `json:"raw_code_included"`
		RawDiffsIncluded     bool `json:"raw_diffs_included"`
		PrivatePathsIncluded bool `json:"private_paths_included"`
	} `json:"redaction"`
}

// ShareCard is the compact, positive, public-safe sharing artifact.
type ShareCard struct {
	Version    int        `json:"version"`
	Title      string     `json:"title"`
	Subtitle   string     `json:"subtitle"`
	Highlights []string   `json:"highlights"`
	Confidence Confidence `json:"confidence"`
	PublicSafe bool       `json:"public_safe"`
}

// PreflightReport summarizes current-diff review readiness.
type PreflightReport struct {
	Version       int            `json:"version"`
	GeneratedAt   time.Time      `json:"generated_at"`
	Repo          RepoMetadata   `json:"repo"`
	Base          string         `json:"base"`
	Head          string         `json:"head"`
	RiskLevel     string         `json:"risk_level"`
	Why           []string       `json:"why"`
	ChangedFiles  []string       `json:"changed_files,omitempty"`
	FileSummary   FileSummary    `json:"file_summary"`
	TestEvidence  string         `json:"test_evidence"`
	Tooling       ToolingReport  `json:"tooling"`
	ReviewerFocus []string       `json:"reviewer_focus"`
	Limitations   []string       `json:"limitations"`
	Privacy       PrivacySummary `json:"privacy"`
}

// FriendReviewPacket is the bridge artifact for friend feedback.
type FriendReviewPacket struct {
	Version     int           `json:"version"`
	GeneratedAt time.Time     `json:"generated_at"`
	Repo        RepoMetadata  `json:"repo"`
	PRNumber    int           `json:"pr_number"`
	Context     string        `json:"context"`
	Card        PRQualityCard `json:"card"`
	Evidence    []string      `json:"evidence"`
	Questions   []string      `json:"questions"`
	Confidence  Confidence    `json:"confidence"`
	PublicSafe  bool          `json:"public_safe"`
}
