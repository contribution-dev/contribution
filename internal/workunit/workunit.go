// Package workunit creates and exports local intent markers.
package workunit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	gitrepo "github.com/contribution-dev/contribution/internal/git"
	"github.com/contribution-dev/contribution/internal/signals"
)

var nowUTC = func() time.Time { return time.Now().UTC() }

// StartOptions controls marker creation.
type StartOptions struct {
	Repo   string
	Goal   string
	Issue  string
	Output string
}

// ExportOptions controls marker export.
type ExportOptions struct {
	Repo   string
	Output string
}

// DefaultMarkerDir returns the default repo-local marker directory.
func DefaultMarkerDir(repoPath string) string {
	return filepath.Join(repoPath, ".contribution", "work-units")
}

// Start creates one local work-unit marker and returns its path and content.
func Start(ctx context.Context, opts StartOptions) (string, signals.WorkUnitMarker, error) {
	goal := strings.TrimSpace(opts.Goal)
	if goal == "" {
		return "", signals.WorkUnitMarker{}, fmt.Errorf("--goal is required")
	}
	repo, err := gitrepo.Resolve(ctx, opts.Repo)
	if err != nil {
		return "", signals.WorkUnitMarker{}, err
	}
	defer func() {
		_ = repo.Close()
	}()
	branch, _ := gitrepo.CurrentBranch(ctx, repo.Path)
	createdAt := nowUTC()
	marker := signals.WorkUnitMarker{
		Version:               1,
		ID:                    markerID(createdAt, goal, opts.Issue, branch, repo.HeadSHA),
		CreatedAt:             createdAt,
		RepoRootFingerprint:   fingerprint(repo.Path),
		RepoName:              repo.Name,
		Branch:                branch,
		Commit:                repo.HeadSHA,
		Goal:                  goal,
		Issue:                 strings.TrimSpace(opts.Issue),
		PrivacyClassification: "local_intent_metadata",
	}
	outputDir := strings.TrimSpace(opts.Output)
	if outputDir == "" {
		outputDir = DefaultMarkerDir(repo.Path)
	}
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return "", signals.WorkUnitMarker{}, fmt.Errorf("create marker directory: %w", err)
	}
	path := filepath.Join(outputDir, marker.ID+".json")
	if err := writeJSON(path, marker); err != nil {
		return "", signals.WorkUnitMarker{}, err
	}
	return path, marker, nil
}

// Export writes a work-unit marker export artifact.
func Export(ctx context.Context, opts ExportOptions) (string, signals.WorkUnitMarkerExport, error) {
	repo, err := gitrepo.Resolve(ctx, opts.Repo)
	if err != nil {
		return "", signals.WorkUnitMarkerExport{}, err
	}
	defer func() {
		_ = repo.Close()
	}()
	markers, _ := ReadMarkers(repo.Path)
	outputDir := strings.TrimSpace(opts.Output)
	if outputDir == "" {
		outputDir = DefaultMarkerDir(repo.Path)
	}
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return "", signals.WorkUnitMarkerExport{}, fmt.Errorf("create marker export directory: %w", err)
	}
	export := signals.WorkUnitMarkerExport{
		Version:     1,
		GeneratedAt: nowUTC(),
		Repo:        repo.Metadata(false),
		Markers:     markers,
		Privacy: signals.PrivacySummary{
			PublicSafe:                         false,
			RawCodeIncluded:                    false,
			RawDiffsIncluded:                   false,
			PrivatePathsIncludedInPublicExport: false,
			AuthorEmailsIncluded:               false,
		},
	}
	path := filepath.Join(outputDir, "work-units.json")
	if err := writeJSON(path, export); err != nil {
		return "", signals.WorkUnitMarkerExport{}, err
	}
	return path, export, nil
}

// ReadMarkers loads repo-local work-unit markers, skipping malformed files.
func ReadMarkers(repoPath string) ([]signals.WorkUnitMarker, []string) {
	dir := DefaultMarkerDir(repoPath)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{"Work-unit markers could not be read: " + err.Error()}
	}
	markers := make([]signals.WorkUnitMarker, 0, len(entries))
	var limitations []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || entry.Name() == "work-units.json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		// #nosec G304 -- path is constrained to the repo-local marker directory.
		data, err := os.ReadFile(path)
		if err != nil {
			limitations = append(limitations, "Work-unit marker "+entry.Name()+" could not be read.")
			continue
		}
		var marker signals.WorkUnitMarker
		if err := json.Unmarshal(data, &marker); err != nil || marker.ID == "" || marker.Goal == "" {
			limitations = append(limitations, "Work-unit marker "+entry.Name()+" is malformed.")
			continue
		}
		markers = append(markers, marker)
	}
	sort.Slice(markers, func(i, j int) bool {
		return markers[i].CreatedAt.Before(markers[j].CreatedAt)
	})
	return markers, limitations
}

func markerID(createdAt time.Time, parts ...string) string {
	sum := sha256.Sum256([]byte(createdAt.Format(time.RFC3339Nano) + "\x00" + strings.Join(parts, "\x00")))
	return "awu-" + createdAt.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(sum[:])[:8]
}

func fingerprint(value string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(value)))
	return hex.EncodeToString(sum[:])[:16]
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", filepath.Base(path), err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}
