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
	Path      string `json:"-"`
}

// ToolingReport summarizes required and optional runtime tooling.
type ToolingReport struct {
	GeneratedAt time.Time          `json:"generated_at"`
	Tools       []ToolAvailability `json:"tools"`
	Limitations []string           `json:"limitations,omitempty"`
}

// AnalyzerFinding is a normalized optional-tool finding without raw code.
type AnalyzerFinding struct {
	Tool       string     `json:"tool"`
	RuleID     string     `json:"rule_id,omitempty"`
	Severity   Severity   `json:"severity"`
	FilePath   string     `json:"file_path,omitempty"`
	Scope      string     `json:"scope"`
	Message    string     `json:"message"`
	Confidence Confidence `json:"confidence"`
	PublicSafe bool       `json:"is_public_safe"`
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
	PublicSafe               bool     `json:"public_safe"`
	NoExternalTools          bool     `json:"no_external_tools"`
	SelfReportedAITools      []string `json:"self_reported_ai_tools,omitempty"`
	SelfReportedAIModes      []string `json:"self_reported_ai_modes,omitempty"`
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
}

// SourceCoverageStatus describes whether a source is usable for this run.
type SourceCoverageStatus string

const (
	// SourceCoverageAvailable means direct evidence was available.
	SourceCoverageAvailable SourceCoverageStatus = "available"
	// SourceCoveragePartial means evidence exists but does not cover the full question.
	SourceCoveragePartial SourceCoverageStatus = "partial"
	// SourceCoverageMissing means the source is absent.
	SourceCoverageMissing SourceCoverageStatus = "missing"
	// SourceCoverageNotRequested means the user did not ask the CLI to inspect the source.
	SourceCoverageNotRequested SourceCoverageStatus = "not_requested"
	// SourceCoverageRequiresWebConnection means the web app must authenticate the source.
	SourceCoverageRequiresWebConnection SourceCoverageStatus = "requires_web_connection"
	// SourceCoverageRequiresAdmin means admin or manager permissions are needed.
	SourceCoverageRequiresAdmin SourceCoverageStatus = "requires_admin"
	// SourceCoverageFutureInstrumentation means future telemetry must be emitted before this source can be trusted.
	SourceCoverageFutureInstrumentation SourceCoverageStatus = "future_instrumentation"
)

// SourceCoverageItem records one evidence source and what it unlocks.
type SourceCoverageItem struct {
	ID         string               `json:"id"`
	Label      string               `json:"label"`
	Category   string               `json:"category"`
	Status     SourceCoverageStatus `json:"status"`
	Evidence   string               `json:"evidence,omitempty"`
	Why        string               `json:"why,omitempty"`
	Unlocks    string               `json:"unlocks,omitempty"`
	NextAction string               `json:"next_action,omitempty"`
	Confidence Confidence           `json:"confidence"`
}

// SourceCoverage summarizes the observable and missing data sources.
type SourceCoverage struct {
	GeneratedAt time.Time            `json:"generated_at"`
	Summary     string               `json:"summary"`
	Confidence  Confidence           `json:"confidence"`
	Sources     []SourceCoverageItem `json:"sources"`
	NextActions []string             `json:"next_actions"`
}

// DataGap is a missing source that materially limits insight.
type DataGap struct {
	ID               string               `json:"id"`
	Label            string               `json:"label"`
	Status           SourceCoverageStatus `json:"status"`
	Why              string               `json:"why"`
	Unlocks          string               `json:"unlocks"`
	NextAction       string               `json:"next_action"`
	ConfidenceImpact string               `json:"confidence_impact"`
}

// RecommendedConnection is a concrete setup step that increases future coverage.
type RecommendedConnection struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	Category      string `json:"category"`
	Command       string `json:"command,omitempty"`
	Why           string `json:"why"`
	Unlocks       string `json:"unlocks"`
	RequiresAdmin bool   `json:"requires_admin,omitempty"`
}

// ReadinessComponent is one deterministic part of the agentic readiness score.
type ReadinessComponent struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	Score      int        `json:"score"`
	Weight     int        `json:"weight"`
	Confidence Confidence `json:"confidence"`
	Evidence   string     `json:"evidence"`
	NextAction string     `json:"next_action,omitempty"`
}

// AgenticReadiness answers how prepared the repo is for agentic development.
type AgenticReadiness struct {
	Score       int                  `json:"score"`
	Grade       string               `json:"grade"`
	Confidence  Confidence           `json:"confidence"`
	Summary     string               `json:"summary"`
	Components  []ReadinessComponent `json:"components"`
	TopActions  []string             `json:"top_actions"`
	Evidence    []string             `json:"evidence"`
	Limitations []string             `json:"limitations"`
}

// AnchorPattern describes an inferred workflow pattern.
type AnchorPattern struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	Count      int        `json:"count"`
	Confidence Confidence `json:"confidence"`
	Evidence   string     `json:"evidence,omitempty"`
}

// WorkUnitAnchor is one observable anchor for a candidate unit of intent.
type WorkUnitAnchor struct {
	Type       string     `json:"type"`
	ID         string     `json:"id,omitempty"`
	Label      string     `json:"label,omitempty"`
	Confidence Confidence `json:"confidence"`
}

// WorkUnitCandidate is a confidence-scored hypothesis about a coherent intent.
type WorkUnitCandidate struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Pattern     string           `json:"pattern"`
	Confidence  Confidence       `json:"confidence"`
	Summary     string           `json:"summary"`
	Anchors     []WorkUnitAnchor `json:"anchors"`
	Evidence    []string         `json:"evidence,omitempty"`
	Limitations []string         `json:"limitations,omitempty"`
}

// AttributionReadiness summarizes whether visible evidence can group work units.
type AttributionReadiness struct {
	Pattern         string          `json:"pattern"`
	Confidence      Confidence      `json:"confidence"`
	Summary         string          `json:"summary"`
	Evidence        []string        `json:"evidence"`
	MissingEvidence []string        `json:"missing_evidence"`
	NextAction      string          `json:"next_action"`
	AnchorPatterns  []AnchorPattern `json:"anchor_patterns"`
}

// AgentArtifactMetadata is metadata-only evidence from explicitly supplied agent artifacts.
type AgentArtifactMetadata struct {
	Path               string     `json:"path,omitempty"`
	Source             string     `json:"source,omitempty"`
	Status             string     `json:"status"`
	Reason             string     `json:"reason,omitempty"`
	SessionFingerprint string     `json:"session_fingerprint,omitempty"`
	RepoMatched        bool       `json:"repo_matched,omitempty"`
	Branch             string     `json:"branch,omitempty"`
	Commit             string     `json:"commit,omitempty"`
	TokenCount         int        `json:"token_count,omitempty"`
	CostUSD            float64    `json:"cost_usd,omitempty"`
	Confidence         Confidence `json:"confidence"`
}

// WorkUnitMarker is a local, user-created intent marker.
type WorkUnitMarker struct {
	Version               int       `json:"version"`
	ID                    string    `json:"id"`
	CreatedAt             time.Time `json:"created_at"`
	RepoRootFingerprint   string    `json:"repo_root_fingerprint"`
	RepoName              string    `json:"repo_name,omitempty"`
	Branch                string    `json:"branch,omitempty"`
	Commit                string    `json:"commit,omitempty"`
	Goal                  string    `json:"goal"`
	Issue                 string    `json:"issue,omitempty"`
	PrivacyClassification string    `json:"privacy_classification"`
}

// WorkUnitMarkerExport is the export artifact for local work-unit markers.
type WorkUnitMarkerExport struct {
	Version     int              `json:"version"`
	GeneratedAt time.Time        `json:"generated_at"`
	Repo        RepoMetadata     `json:"repo"`
	Markers     []WorkUnitMarker `json:"markers"`
	Privacy     PrivacySummary   `json:"privacy"`
}

// CollectorGitSummary gives the web app local git evidence without raw diffs.
type CollectorGitSummary struct {
	CommitCount      int      `json:"commit_count"`
	UniqueFiles      int      `json:"unique_files"`
	HighChurnFiles   []string `json:"high_churn_files,omitempty"`
	HeadSHAAvailable bool     `json:"head_sha_available"`
}

// CollectorBundle is the public-safe local probe artifact consumed by the web app.
type CollectorBundle struct {
	Version              int                     `json:"version"`
	GeneratedAt          time.Time               `json:"generated_at"`
	Repo                 RepoMetadata            `json:"repo"`
	Git                  CollectorGitSummary     `json:"git"`
	TopRead              TopRead                 `json:"top_read"`
	Tooling              ToolingReport           `json:"tooling"`
	AgenticReadiness     AgenticReadiness        `json:"agentic_readiness"`
	SourceCoverage       SourceCoverage          `json:"source_coverage"`
	DataGaps             []DataGap               `json:"data_gaps"`
	Recommended          []RecommendedConnection `json:"recommended_connections"`
	AttributionReadiness AttributionReadiness    `json:"attribution_readiness"`
	WorkUnitCandidates   []WorkUnitCandidate     `json:"work_unit_candidates"`
	AgentArtifacts       []AgentArtifactMetadata `json:"agent_artifacts,omitempty"`
	SetupActions         []SetupAction           `json:"setup_actions"`
	Limitations          []string                `json:"limitations"`
	Privacy              PrivacySummary          `json:"privacy"`
}

// Finding is a human-readable conclusion with evidence and confidence.
type Finding struct {
	Label        string     `json:"label"`
	Evidence     string     `json:"evidence"`
	Confidence   Confidence `json:"confidence"`
	WhyItMatters string     `json:"why_it_matters,omitempty"`
	NextAction   string     `json:"next_action,omitempty"`
}

// TopFinding is a deterministic, first-read conclusion for the report summary.
type TopFinding struct {
	ID           string     `json:"id"`
	Label        string     `json:"label"`
	Evidence     string     `json:"evidence"`
	Severity     Severity   `json:"severity"`
	Confidence   Confidence `json:"confidence"`
	WhyItMatters string     `json:"why_it_matters,omitempty"`
	NextAction   string     `json:"next_action,omitempty"`
	Source       string     `json:"source"`
}

// TopRead is the report-first deterministic summary.
type TopRead struct {
	Headline   string       `json:"headline"`
	Summary    string       `json:"summary"`
	Findings   []TopFinding `json:"findings"`
	NextPRPlan []string     `json:"next_pr_plan"`
	Confidence Confidence   `json:"confidence"`
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

// TrendWindow summarizes one local history window for before/after comparison.
type TrendWindow struct {
	Label                     string    `json:"label"`
	Since                     time.Time `json:"since"`
	Until                     time.Time `json:"until"`
	Commits                   int       `json:"commits"`
	SourceCommits             int       `json:"source_commits"`
	TestTouchedCommits        int       `json:"test_touched_commits"`
	SourceWithoutTestsCommits int       `json:"source_without_tests_commits"`
	LargeCommits              int       `json:"large_commits"`
	RiskyWithoutTestsCommits  int       `json:"risky_without_tests_commits"`
	FixLikeCommits            int       `json:"fix_like_commits"`
	HighChurnFiles            int       `json:"high_churn_files"`
}

// TrendMetric compares one receipt-style quality signal across two windows.
type TrendMetric struct {
	ID           string     `json:"id"`
	Label        string     `json:"label"`
	CurrentValue float64    `json:"current_value"`
	PriorValue   float64    `json:"prior_value"`
	Delta        float64    `json:"delta"`
	Unit         string     `json:"unit"`
	Direction    string     `json:"direction"`
	Evidence     string     `json:"evidence"`
	Confidence   Confidence `json:"confidence"`
	WhyItMatters string     `json:"why_it_matters,omitempty"`
	NextAction   string     `json:"next_action,omitempty"`
}

// TrendComparison compares recent work with the immediately prior window.
type TrendComparison struct {
	Status        string        `json:"status"`
	CurrentWindow TrendWindow   `json:"current_window"`
	PriorWindow   TrendWindow   `json:"prior_window"`
	Metrics       []TrendMetric `json:"metrics"`
	Findings      []Finding     `json:"findings"`
	Confidence    Confidence    `json:"confidence"`
	Reason        string        `json:"reason,omitempty"`
}

// FollowUpComparison compares the current report with the latest prior report.
type FollowUpComparison struct {
	Status              string     `json:"status"`
	PreviousGeneratedAt time.Time  `json:"previous_generated_at,omitempty"`
	CurrentGeneratedAt  time.Time  `json:"current_generated_at,omitempty"`
	Summary             string     `json:"summary,omitempty"`
	Improved            []Finding  `json:"improved"`
	Regressed           []Finding  `json:"regressed"`
	Resolved            []Finding  `json:"resolved"`
	Persistent          []Finding  `json:"persistent"`
	NextAction          string     `json:"next_action,omitempty"`
	Confidence          Confidence `json:"confidence"`
	Reason              string     `json:"reason,omitempty"`
}

// DeepDiveArtifact is a private-first reference to the artifact behind a pattern.
type DeepDiveArtifact struct {
	ID           string     `json:"id,omitempty"`
	Label        string     `json:"label"`
	Title        string     `json:"title,omitempty"`
	Scope        string     `json:"scope,omitempty"`
	TestEvidence string     `json:"test_evidence,omitempty"`
	MainRisk     string     `json:"main_risk,omitempty"`
	NextAction   string     `json:"next_action,omitempty"`
	Confidence   Confidence `json:"confidence,omitempty"`
}

// HighChurnDeepDive explains which artifacts are behind a high-churn file.
type HighChurnDeepDive struct {
	Path       string             `json:"path"`
	Touches    int                `json:"touches"`
	Artifacts  []DeepDiveArtifact `json:"artifacts"`
	NextAction string             `json:"next_action"`
	Confidence Confidence         `json:"confidence"`
}

// NoTestArtifactDeepDive explains behavior-changing artifacts without test files.
type NoTestArtifactDeepDive struct {
	Artifact           DeepDiveArtifact `json:"artifact"`
	ChangedSourceFiles []string         `json:"changed_source_files,omitempty"`
	Risk               string           `json:"risk"`
	NextAction         string           `json:"next_action"`
	Confidence         Confidence       `json:"confidence"`
}

// AnalysisDeepDives stores report-ready evidence for single-player coaching.
type AnalysisDeepDives struct {
	HighChurn       []HighChurnDeepDive      `json:"high_churn"`
	NoTestArtifacts []NoTestArtifactDeepDive `json:"no_test_artifacts"`
}

// SetupAction is a concrete next command that would raise report confidence.
type SetupAction struct {
	ID               string `json:"id"`
	Label            string `json:"label"`
	Command          string `json:"command,omitempty"`
	Why              string `json:"why"`
	ConfidenceImpact string `json:"confidence_impact"`
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

// CoverageSummary summarizes optional whole-report coverage import.
type CoverageSummary struct {
	Status           string                  `json:"status"`
	CoveredLines     int                     `json:"covered_lines,omitempty"`
	TotalLines       int                     `json:"total_lines,omitempty"`
	Percent          float64                 `json:"percent,omitempty"`
	Files            []PreflightFileCoverage `json:"files,omitempty"`
	LowCoverageFiles []PreflightFileCoverage `json:"low_coverage_files,omitempty"`
	Sources          []string                `json:"sources,omitempty"`
	Reason           string                  `json:"reason,omitempty"`
}

// PreflightChangedFile is structured current-diff evidence.
type PreflightChangedFile struct {
	Path       string      `json:"path"`
	Additions  int         `json:"additions,omitempty"`
	Deletions  int         `json:"deletions,omitempty"`
	LineRanges []LineRange `json:"line_ranges,omitempty"`
	Class      string      `json:"class,omitempty"`
	Language   string      `json:"language,omitempty"`
	Risky      bool        `json:"risky,omitempty"`
}

// AnalysisReport is the canonical machine-readable V1 output.
type AnalysisReport struct {
	Version                int                     `json:"version"`
	GeneratedAt            time.Time               `json:"generated_at"`
	Repo                   RepoMetadata            `json:"repo"`
	Config                 AnalysisConfigSnapshot  `json:"config"`
	Tooling                ToolingReport           `json:"tooling"`
	Inventory              FileSummary             `json:"inventory"`
	Coverage               CoverageSummary         `json:"coverage"`
	TopRead                TopRead                 `json:"top_read"`
	AnalyzerFindings       []AnalyzerFinding       `json:"analyzer_findings"`
	Signals                []Signal                `json:"signals"`
	PRCards                []PRQualityCard         `json:"pr_quality_cards"`
	WeaknessMap            WeaknessMap             `json:"weakness_map"`
	Trends                 TrendComparison         `json:"trends"`
	FollowUp               FollowUpComparison      `json:"follow_up"`
	DeepDives              AnalysisDeepDives       `json:"deep_dives"`
	Profile                ProfileSummary          `json:"profile"`
	AgenticReadiness       AgenticReadiness        `json:"agentic_readiness"`
	SourceCoverage         SourceCoverage          `json:"source_coverage"`
	DataGaps               []DataGap               `json:"data_gaps"`
	RecommendedConnections []RecommendedConnection `json:"recommended_connections"`
	AttributionReadiness   AttributionReadiness    `json:"attribution_readiness"`
	WorkUnitCandidates     []WorkUnitCandidate     `json:"work_unit_candidates"`
	AgentArtifacts         []AgentArtifactMetadata `json:"agent_artifacts,omitempty"`
	SetupActions           []SetupAction           `json:"setup_actions"`
	Limitations            []string                `json:"limitations"`
	Privacy                PrivacySummary          `json:"privacy"`
	PrivacySummary         PrivacySummary          `json:"privacy_summary"`
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

// PreflightCoverage summarizes changed-line coverage evidence.
type PreflightCoverage struct {
	Status       string                  `json:"status"`
	CoveredLines int                     `json:"covered_lines,omitempty"`
	TotalLines   int                     `json:"total_lines,omitempty"`
	Percent      float64                 `json:"percent,omitempty"`
	Files        []PreflightFileCoverage `json:"files,omitempty"`
	Sources      []string                `json:"sources,omitempty"`
	Reason       string                  `json:"reason,omitempty"`
}

// PreflightFileCoverage summarizes changed-line coverage for one file.
type PreflightFileCoverage struct {
	Path         string  `json:"path"`
	CoveredLines int     `json:"covered_lines"`
	TotalLines   int     `json:"total_lines"`
	Percent      float64 `json:"percent"`
}

// PreflightRubricItem is a structured review-readiness check.
type PreflightRubricItem struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	Status         string `json:"status"`
	Severity       string `json:"severity"`
	Evidence       string `json:"evidence"`
	Recommendation string `json:"recommendation,omitempty"`
}

// PreflightReport summarizes current-diff review readiness.
type PreflightReport struct {
	Version           int                       `json:"version"`
	GeneratedAt       time.Time                 `json:"generated_at"`
	Repo              RepoMetadata              `json:"repo"`
	Base              string                    `json:"base"`
	Head              string                    `json:"head"`
	RiskLevel         string                    `json:"risk_level"`
	Why               []string                  `json:"why"`
	ChangedFiles      []PreflightChangedFile    `json:"changed_files,omitempty"`
	FileSummary       FileSummary               `json:"file_summary"`
	TotalChangedLines int                       `json:"total_changed_lines"`
	Coverage          PreflightCoverage         `json:"coverage"`
	AnalyzerFindings  []AnalyzerFinding         `json:"analyzer_findings"`
	Rubric            []PreflightRubricItem     `json:"rubric"`
	TestEvidence      string                    `json:"test_evidence"`
	Tooling           ToolingReport             `json:"tooling"`
	ReviewerFocus     []string                  `json:"reviewer_focus"`
	PersonalContext   *PersonalPreflightContext `json:"personal_context,omitempty"`
	Limitations       []string                  `json:"limitations"`
	Privacy           PrivacySummary            `json:"privacy"`
}

// PersonalPreflightContext stores recent single-player patterns used by preflight.
type PersonalPreflightContext struct {
	HighChurnFiles           []string `json:"high_churn_files,omitempty"`
	RecentSourceWithoutTests int      `json:"recent_source_without_tests,omitempty"`
	TypicalFiles             int      `json:"typical_files,omitempty"`
	TypicalLines             int      `json:"typical_lines,omitempty"`
	ArtifactsAnalyzed        int      `json:"artifacts_analyzed,omitempty"`
}

// ReviewRubricQuestion is a structured reviewer prompt in a friend packet.
type ReviewRubricQuestion struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
	Focus  string `json:"focus,omitempty"`
}

// FriendReviewPacket is the bridge artifact for friend feedback.
type FriendReviewPacket struct {
	Version       int                    `json:"version"`
	GeneratedAt   time.Time              `json:"generated_at"`
	PacketID      string                 `json:"packet_id"`
	Repo          RepoMetadata           `json:"repo"`
	PRNumber      int                    `json:"pr_number"`
	ArtifactLabel string                 `json:"artifact_label"`
	Context       string                 `json:"context"`
	Card          PRQualityCard          `json:"card"`
	Evidence      []string               `json:"evidence"`
	Rubric        []ReviewRubricQuestion `json:"rubric"`
	Confidence    Confidence             `json:"confidence"`
	PublicSafe    bool                   `json:"public_safe"`
}

// FriendFeedbackAnswer is one structured reviewer response.
type FriendFeedbackAnswer struct {
	QuestionID string `json:"question_id"`
	Question   string `json:"question,omitempty"`
	Answer     string `json:"answer"`
}

// FriendFeedbackExport is the public-safe feedback import contract.
type FriendFeedbackExport struct {
	Version       int                    `json:"version"`
	PacketID      string                 `json:"packet_id"`
	SubmittedAt   time.Time              `json:"submitted_at"`
	ReviewerLabel string                 `json:"reviewer_label,omitempty"`
	OverallTrust  string                 `json:"overall_trust"`
	Confidence    Confidence             `json:"confidence"`
	Answers       []FriendFeedbackAnswer `json:"answers"`
	PublicSafe    bool                   `json:"public_safe"`
}
