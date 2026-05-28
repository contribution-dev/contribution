// Package privacy contains public-safe redaction helpers.
package privacy

import (
	"path/filepath"
	"strings"
)

// RedactPath removes local filesystem detail from a path for public exports.
func RedactPath(path string, includePrivatePaths bool) string {
	if includePrivatePaths {
		return filepath.ToSlash(path)
	}
	path = filepath.ToSlash(path)
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}

// ContainsSecretLikeValue is a conservative guard for obvious secret material.
func ContainsSecretLikeValue(value string) bool {
	lower := strings.ToLower(value)
	keywords := []string{"token=", "secret=", "password=", "api_key=", "apikey=", "authorization:", "bearer "}
	for _, keyword := range keywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}
