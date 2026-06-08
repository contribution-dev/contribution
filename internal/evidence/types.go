// Package evidence builds opt-in, derived AI work evidence bundles.
package evidence

import (
	"time"

	"github.com/contribution-dev/contribution/internal/signals"
)

const (
	// BundleSchema is the stable schema name for AI work evidence exports.
	BundleSchema = "ai_work_evidence_bundle"
	// BundleVersion is the current additive schema version.
	BundleVersion = 1

	// ExportModeOffline means the bundle was written locally without network use.
	ExportModeOffline = "offline_export"
	// UploadModeDisabled means hosted upload is not implemented.
	UploadModeDisabled = "disabled"
)

// Options controls opt-in AI work evidence discovery.
type Options struct {
	Repo             string
	Output           string
	ClaudeDir        string
	CodexDir         string
	Sources          []string
	IncludeFilePaths bool
	IncludeRepoName  bool
}

// Result is returned by preview and export commands.
type Result struct {
	Bundle               AIWorkEvidenceBundle
	BundlePath           string
	RedactionReceiptPath string
	SourceSummaries      []SourceScanSummary
	SourcesScanned       int
	SessionsFound        int
	SessionsLinked       int
	SessionsSkipped      int
	FieldsExtracted      int
	FieldsRedacted       int
	FieldsBlocked        int
}

// DoctorResult reports local source availability without reading session files.
type DoctorResult struct {
	ClaudeAvailable bool
	ClaudePath      string
	CodexAvailable  bool
	CodexPath       string
	NetworkUsed     bool
	UploadMode      string
}

// AIWorkEvidenceBundle is a derived, redacted evidence bundle.
type AIWorkEvidenceBundle struct {
	Schema           string             `json:"schema"`
	Version          int                `json:"version"`
	GeneratedAt      time.Time          `json:"generated_at"`
	BundleID         string             `json:"bundle_id"`
	Repo             RepoAnchor         `json:"repo"`
	Export           ModeSummary        `json:"export"`
	Upload           ModeSummary        `json:"upload"`
	EvidenceUpload   UploadRecord       `json:"evidence_upload"`
	Privacy          PrivacyFlags       `json:"privacy"`
	SourceLineage    []SourceLineage    `json:"source_lineage"`
	WorkSessions     []WorkSession      `json:"work_sessions"`
	RedactionReceipt RedactionReceipt   `json:"redaction_receipt"`
	Confidence       signals.Confidence `json:"confidence"`
	LinkageNotes     []ConfidenceNote   `json:"linkage_notes,omitempty"`
	Limitations      []string           `json:"limitations,omitempty"`
}

// ModeSummary records export/upload mode without contacting hosted services.
type ModeSummary struct {
	Mode        string `json:"mode"`
	Destination string `json:"destination,omitempty"`
	Enabled     bool   `json:"enabled"`
}

// UploadRecord records upload state. It remains disabled until the CLI consumes the web contract.
type UploadRecord struct {
	ID        string    `json:"id"`
	BundleID  string    `json:"bundle_id"`
	Mode      string    `json:"mode"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// RepoAnchor links local evidence to durable repo metadata without diffs.
type RepoAnchor struct {
	RepoID                string   `json:"repo_id,omitempty"`
	RepoRemoteHash        string   `json:"repo_remote_hash,omitempty"`
	RepoName              string   `json:"repo_name,omitempty"`
	Branch                string   `json:"branch,omitempty"`
	CurrentCommitSHA      string   `json:"current_commit_sha,omitempty"`
	CommitSHAs            []string `json:"commit_shas"`
	PRNumbers             []int    `json:"pr_numbers,omitempty"`
	IssueKeys             []string `json:"issue_keys,omitempty"`
	CurrentDiffFilesCount int      `json:"current_diff_files_count"`
}

// WorkSession is one derived local AI work session.
type WorkSession struct {
	SessionIDHash         string             `json:"session_id_hash,omitempty"`
	SourceTool            string             `json:"source_tool"`
	SourceKind            string             `json:"source_kind"`
	StartedAt             time.Time          `json:"started_at"`
	EndedAt               time.Time          `json:"ended_at"`
	RepoRemoteHash        string             `json:"repo_remote_hash,omitempty"`
	RepoID                string             `json:"repo_id,omitempty"`
	RepoName              string             `json:"repo_name,omitempty"`
	Branch                string             `json:"branch,omitempty"`
	CommitSHAs            []string           `json:"commit_shas"`
	PRNumbers             []int              `json:"pr_numbers,omitempty"`
	IssueKeys             []string           `json:"issue_keys,omitempty"`
	IntentSummary         string             `json:"intent_summary"`
	PlanSummary           string             `json:"plan_summary"`
	ImplementationSummary string             `json:"implementation_summary"`
	HumanSteeringCount    int                `json:"human_steering_count"`
	CorrectionCount       int                `json:"correction_count"`
	TestDebugCount        int                `json:"test_debug_count"`
	AgentActionCount      int                `json:"agent_action_count"`
	FilesTouchedCount     int                `json:"files_touched_count"`
	FilePathHashes        []string           `json:"file_path_hashes,omitempty"`
	FilePaths             []string           `json:"file_paths,omitempty"`
	EvidenceExcerptCount  int                `json:"evidence_excerpt_count"`
	ExportMode            string             `json:"export_mode"`
	UploadMode            string             `json:"upload_mode"`
	RedactionReceiptID    string             `json:"redaction_receipt_id"`
	Confidence            signals.Confidence `json:"confidence"`
	LinkageNotes          []ConfidenceNote   `json:"linkage_notes,omitempty"`
}

// RedactionReceipt is the machine-readable privacy receipt for a bundle.
type RedactionReceipt struct {
	ID                  string         `json:"id"`
	CreatedAt           time.Time      `json:"created_at"`
	BundleID            string         `json:"bundle_id"`
	DerivedEvidenceOnly bool           `json:"derived_evidence_only"`
	RawContentIncluded  bool           `json:"raw_content_included"`
	RedactionGuaranteed bool           `json:"redaction_guaranteed"`
	FieldsExtracted     int            `json:"fields_extracted"`
	BlockedContent      map[string]int `json:"blocked_content"`
	RedactedContent     map[string]int `json:"redacted_content"`
	ScannedSources      []string       `json:"scanned_sources"`
	ExtractedFieldNames []string       `json:"extracted_field_names,omitempty"`
	BlockedFieldClasses []string       `json:"blocked_field_classes,omitempty"`
	PrivacyFlags        PrivacyFlags   `json:"privacy_flags"`
	UploadMode          string         `json:"upload_mode"`
	ExportMode          string         `json:"export_mode"`
	FailureReason       string         `json:"failure_reason,omitempty"`
}

// SourceLineage records where derived evidence came from without raw paths.
type SourceLineage struct {
	ID               string             `json:"id"`
	SourceTool       string             `json:"source_tool"`
	SourceKind       string             `json:"source_kind"`
	SourcePathHash   string             `json:"source_path_hash,omitempty"`
	ArtifactsScanned int                `json:"artifacts_scanned"`
	SessionsFound    int                `json:"sessions_found"`
	SessionsLinked   int                `json:"sessions_linked"`
	UnreadableCount  int                `json:"unreadable_count"`
	ParserVersion    int                `json:"parser_version"`
	Confidence       signals.Confidence `json:"confidence"`
}

// PrivacyFlags states what the bundle contains and excludes.
type PrivacyFlags struct {
	PublicSafe              bool `json:"public_safe"`
	DerivedEvidenceOnly     bool `json:"derived_evidence_only"`
	RawPromptsIncluded      bool `json:"raw_prompts_included"`
	RawModelOutputsIncluded bool `json:"raw_model_outputs_included"`
	RawTranscriptsIncluded  bool `json:"raw_transcripts_included"`
	RawDiffsIncluded        bool `json:"raw_diffs_included"`
	RawLogsIncluded         bool `json:"raw_logs_included"`
	SourceCodeIncluded      bool `json:"source_code_included"`
	SecretsIncluded         bool `json:"secrets_included"`
	EnvValuesIncluded       bool `json:"env_values_included"`
	PrivateKeysIncluded     bool `json:"private_keys_included"`
	CredentialURLsIncluded  bool `json:"credential_urls_included"`
	FilePathsIncluded       bool `json:"file_paths_included"`
	FilePathHashesIncluded  bool `json:"file_path_hashes_included"`
	RepoNameIncluded        bool `json:"repo_name_included"`
	LocalPathsIncluded      bool `json:"local_paths_included"`
	UploadEnabled           bool `json:"upload_enabled"`
}

// ConfidenceNote explains linkage or extraction confidence.
type ConfidenceNote struct {
	Scope      string             `json:"scope"`
	Confidence signals.Confidence `json:"confidence"`
	Note       string             `json:"note"`
}

// SourceScanSummary is the preview/doctor-friendly source scan result.
type SourceScanSummary struct {
	SourceTool       string `json:"source_tool"`
	SourceKind       string `json:"source_kind"`
	Path             string `json:"path,omitempty"`
	Status           string `json:"status"`
	ArtifactsScanned int    `json:"artifacts_scanned"`
	SessionsFound    int    `json:"sessions_found"`
	SessionsLinked   int    `json:"sessions_linked"`
	UnreadableCount  int    `json:"unreadable_count"`
}
