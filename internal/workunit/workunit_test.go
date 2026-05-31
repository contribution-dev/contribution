package workunit

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/contribution-dev/contribution/internal/signals"
)

func TestReadMarkersSkipsExportFile(t *testing.T) {
	root := t.TempDir()
	dir := DefaultMarkerDir(root)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir marker dir: %v", err)
	}
	if err := writeJSON(filepath.Join(dir, "awu-one.json"), signals.WorkUnitMarker{
		Version:               1,
		ID:                    "awu-one",
		CreatedAt:             time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Goal:                  "Build onboarding",
		PrivacyClassification: "local_intent_metadata",
	}); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	if err := writeJSON(filepath.Join(dir, "work-units.json"), signals.WorkUnitMarkerExport{
		Version: 1,
		Markers: []signals.WorkUnitMarker{{
			ID:   "exported-copy",
			Goal: "Ignore export artifact",
		}},
	}); err != nil {
		t.Fatalf("write export artifact: %v", err)
	}

	markers, limitations := ReadMarkers(root)
	if len(limitations) != 0 {
		t.Fatalf("limitations = %+v, want none", limitations)
	}
	if len(markers) != 1 || markers[0].ID != "awu-one" {
		t.Fatalf("markers = %+v, want only source marker", markers)
	}
}
