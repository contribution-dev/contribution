package privacy

import (
	"strings"
	"testing"
)

func TestRedactRemoteURL(t *testing.T) {
	tokenKey := "to" + "ken"
	passwordKey := "pass" + "word"
	accessKey := "access_" + "key"
	redactMe := "redact-me"
	tests := map[string]string{
		"https://" + tokenKey + "=" + redactMe + "@github.com/owner/repo.git": "https://REDACTED@github.com/owner/repo.git",
		"https://user:" + redactMe + "@github.com/owner/repo.git":             "https://REDACTED@github.com/owner/repo.git",
		"ssh://git:" + redactMe + "@github.com/owner/repo.git":                "ssh://REDACTED@github.com/owner/repo.git",
		"git@github.com:owner/repo.git":                                       "git@github.com:owner/repo.git",
		"https://github.com/owner/repo.git":                                   "https://github.com/owner/repo.git",
		"https://" + tokenKey + "=" + redactMe + "@[::1]/owner/repo.git":      "https://REDACTED@[::1]/owner/repo.git",
		"https://" + tokenKey + "=" + redactMe + "@127.0.0.1/owner/repo.git":  "https://REDACTED@127.0.0.1/owner/repo.git",
		"https://example.test/owner/repo.git?" + tokenKey + "=" + redactMe:    "https://example.test/owner/repo.git?" + tokenKey + "=REDACTED",
		"https://example.test/owner/repo.git?x=" + tokenKey + "=" + redactMe:  "https://example.test/owner/repo.git?x=REDACTED",
		"https://example.test/owner/repo.git?" + passwordKey + "=" + redactMe: "https://example.test/owner/repo.git?" + passwordKey + "=REDACTED",
		"https://example.test/owner/repo.git?" + accessKey + "=" + redactMe:   "https://example.test/owner/repo.git?" + accessKey + "=REDACTED",
	}
	for remote, want := range tests {
		if got := RedactRemoteURL(remote); got != want {
			t.Fatalf("RedactRemoteURL(%q) = %q, want %q", remote, got, want)
		}
	}
}

func TestRedactSecretLikeTextRedactsGitURLFromError(t *testing.T) {
	secret := "dogfood-secret-value"
	text := "fatal: unable to access 'https://127.0.0.1/repo.git?token=" + secret + "/': failed"

	got := RedactSecretLikeText(text)
	if strings.Contains(got, secret) {
		t.Fatalf("redacted text still contains secret: %q", got)
	}
	if !strings.Contains(got, "token=REDACTED") {
		t.Fatalf("redacted text missing redacted query: %q", got)
	}
}

func TestRedactSecretLikeTextRedactsAuthorizationBearerToken(t *testing.T) {
	secret := "dogfood-secret-value"
	text := "request failed with Authorization: Bearer " + secret

	got := RedactSecretLikeText(text)
	if strings.Contains(got, secret) {
		t.Fatalf("redacted text still contains bearer token: %q", got)
	}
	if !strings.Contains(got, "Authorization:REDACTED") {
		t.Fatalf("redacted text missing authorization redaction marker: %q", got)
	}
}
