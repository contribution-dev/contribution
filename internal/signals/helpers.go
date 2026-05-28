package signals

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// StableID creates a short deterministic signal id from stable parts.
func StableID(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return "sig_" + hex.EncodeToString(h.Sum(nil))[:12]
}

// New creates a normalized signal with a deterministic id.
func New(repoID, source, signalType, subjectType, subjectID string, severity Severity, direction Direction, confidence Confidence, value float64, unit, message string, publicSafe bool, createdAt time.Time) Signal {
	return Signal{
		ID:          StableID(repoID, source, signalType, subjectType, subjectID, message),
		RepoID:      repoID,
		Source:      source,
		Type:        signalType,
		SubjectType: subjectType,
		SubjectID:   subjectID,
		Severity:    severity,
		Direction:   direction,
		Confidence:  confidence,
		Value:       value,
		Unit:        unit,
		Message:     strings.TrimSpace(message),
		PublicSafe:  publicSafe,
		CreatedAt:   createdAt.UTC(),
	}
}
