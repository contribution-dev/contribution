// Package privacy contains public-safe redaction helpers.
package privacy

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	urlPattern           = regexp.MustCompile(`[a-z][a-z0-9+.-]*://[^\s'"]+`)
	authorizationPattern = regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*(?:[A-Za-z]+\s+)?[^\s'"]+`)
	assignmentPattern    = regexp.MustCompile(`(?i)\b(token|secret|password|api_?key|apikey|access_key)\s*[:=]\s*[^\s'"]+`)
	bearerPattern        = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`)
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
	keywords := []string{"token=", "secret=", "password=", "api_key=", "apikey=", "access_key=", "authorization:", "bearer "}
	for _, keyword := range keywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

// RedactRemoteURL removes URL credentials and secret-like query parameters.
func RedactRemoteURL(remote string) string {
	remote = strings.TrimSpace(remote)
	parsed, err := url.Parse(remote)
	if err == nil && parsed.User != nil {
		parsed.User = url.User("REDACTED")
	}
	if err == nil && parsed.RawQuery != "" {
		query := parsed.Query()
		changed := false
		for key, values := range query {
			if isSecretQueryKey(key) {
				query[key] = []string{"REDACTED"}
				changed = true
				continue
			}
			for _, value := range values {
				if ContainsSecretLikeValue(value) {
					query[key] = []string{"REDACTED"}
					changed = true
					break
				}
			}
		}
		if changed {
			parsed.RawQuery = query.Encode()
		}
	}
	if err == nil && (parsed.User != nil || parsed.RawQuery != "") {
		return parsed.String()
	}
	schemeIndex := strings.Index(remote, "://")
	atIndex := strings.LastIndex(remote, "@")
	if schemeIndex >= 0 && atIndex > schemeIndex+3 {
		return remote[:schemeIndex+3] + "REDACTED@" + remote[atIndex+1:]
	}
	return remote
}

// RedactSecretLikeText removes obvious token material from command output.
func RedactSecretLikeText(value string) string {
	value = urlPattern.ReplaceAllStringFunc(value, RedactRemoteURL)
	value = authorizationPattern.ReplaceAllStringFunc(value, redactAssignment)
	value = assignmentPattern.ReplaceAllStringFunc(value, redactAssignment)
	value = bearerPattern.ReplaceAllString(value, "Bearer REDACTED")
	return value
}

func redactAssignment(value string) string {
	for _, sep := range []string{":", "="} {
		if index := strings.Index(value, sep); index >= 0 {
			return value[:index+1] + "REDACTED"
		}
	}
	return "REDACTED"
}

func isSecretQueryKey(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "token") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "password") ||
		strings.Contains(key, "api_key") ||
		strings.Contains(key, "apikey") ||
		strings.Contains(key, "access_key") ||
		strings.Contains(key, "credential") ||
		strings.Contains(key, "authorization") ||
		strings.Contains(key, "signature") ||
		strings.Contains(key, "sig")
}
