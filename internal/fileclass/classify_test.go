package fileclass

import "testing"

func TestClassifyPath(t *testing.T) {
	tests := []struct {
		path       string
		class      string
		risky      bool
		test       bool
		dependency bool
		config     bool
		language   string
	}{
		{path: "internal/auth/session.go", class: "source", risky: true},
		{path: "internal/auth/session_test.go", class: "test", risky: true, test: true},
		{path: "scripts/codex-review-hooks.test.mjs", class: "test", test: true, language: "JavaScript"},
		{path: "docs/vision.md", class: "docs"},
		{path: "go.mod", class: "dependency", dependency: true},
		{path: ".contribution.yml", class: "config", config: true, language: "YAML"},
		{path: "LICENSE", class: "config", config: true, language: "Other"},
		{path: "lint-staged.config.js", class: "config", config: true, language: "JavaScript"},
		{path: "scripts/codex-review-worker", class: "source", language: "Shell"},
		{path: "vendor/example/file.go", class: "vendor"},
		{path: ".github/workflows/ci.yml", class: "infrastructure"},
	}
	for _, tt := range tests {
		got := ClassifyPath(tt.path)
		if got.Class != tt.class {
			t.Fatalf("ClassifyPath(%q).Class = %q, want %q", tt.path, got.Class, tt.class)
		}
		if got.IsSecurityRelated != tt.risky {
			t.Fatalf("ClassifyPath(%q).IsSecurityRelated = %v, want %v", tt.path, got.IsSecurityRelated, tt.risky)
		}
		if got.IsTest != tt.test {
			t.Fatalf("ClassifyPath(%q).IsTest = %v, want %v", tt.path, got.IsTest, tt.test)
		}
		if got.IsDependency != tt.dependency {
			t.Fatalf("ClassifyPath(%q).IsDependency = %v, want %v", tt.path, got.IsDependency, tt.dependency)
		}
		if got.IsConfig != tt.config {
			t.Fatalf("ClassifyPath(%q).IsConfig = %v, want %v", tt.path, got.IsConfig, tt.config)
		}
		if tt.language != "" && got.Language != tt.language {
			t.Fatalf("ClassifyPath(%q).Language = %q, want %q", tt.path, got.Language, tt.language)
		}
	}
}
