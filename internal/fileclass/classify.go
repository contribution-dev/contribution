// Package fileclass classifies repository-relative paths for analysis.
package fileclass

import (
	"path/filepath"
	"strings"

	"github.com/contribution-dev/contribution/internal/signals"
)

// Classification describes a V1 file path classification.
type Classification struct {
	Class             string
	Language          string
	IsTest            bool
	IsDocs            bool
	IsSource          bool
	IsDependency      bool
	IsConfig          bool
	IsGenerated       bool
	IsVendor          bool
	IsInfrastructure  bool
	IsBuildArtifact   bool
	IsMigration       bool
	IsSecurityRelated bool
}

// ClassifyPath classifies a repository-relative path.
func ClassifyPath(path string) Classification {
	p := filepath.ToSlash(strings.TrimPrefix(path, "./"))
	lower := strings.ToLower(p)
	base := strings.ToLower(filepath.Base(p))
	class := Classification{Class: "source", Language: languageForPath(lower), IsSource: true}

	switch {
	case isVendorPath(lower):
		class = Classification{Class: "vendor", Language: languageForPath(lower), IsVendor: true}
	case isGeneratedPath(lower):
		class = Classification{Class: "generated", Language: languageForPath(lower), IsGenerated: true}
	case isBuildArtifactPath(lower):
		class = Classification{Class: "build_artifact", Language: languageForPath(lower), IsBuildArtifact: true}
	case isTestPath(lower):
		class = Classification{Class: "test", Language: languageForPath(lower), IsTest: true}
	case isDocsPath(lower, base):
		class = Classification{Class: "docs", Language: "Markdown", IsDocs: true}
	case isDependencyFile(lower, base):
		class = Classification{Class: "dependency", Language: languageForPath(lower), IsDependency: true}
	case isConfigPath(lower, base):
		class = Classification{Class: "config", Language: languageForPath(lower), IsConfig: true}
	case isInfrastructurePath(lower, base):
		class = Classification{Class: "infrastructure", Language: languageForPath(lower), IsInfrastructure: true}
	case isMigrationPath(lower):
		class = Classification{Class: "migration", Language: languageForPath(lower), IsMigration: true, IsSource: true}
	case isExtensionlessScriptPath(lower, base):
		class = Classification{Class: "source", Language: "Shell", IsSource: true}
	case languageForPath(lower) == "Other":
		class = Classification{Class: "unknown", Language: "Other"}
	}

	class.IsSecurityRelated = isSecuritySensitivePath(lower)
	return class
}

// NewSummary returns an empty file summary with initialized maps.
func NewSummary() signals.FileSummary {
	return signals.FileSummary{
		ByClass:    map[string]int{},
		ByLanguage: map[string]int{},
	}
}

// AddToSummary classifies path and increments summary counters.
func AddToSummary(summary *signals.FileSummary, path string) {
	class := ClassifyPath(path)
	summary.TotalFiles++
	summary.ByClass[class.Class]++
	summary.ByLanguage[class.Language]++
	switch {
	case class.IsTest:
		summary.TestFiles++
	case class.IsDocs:
		summary.DocsFiles++
	case class.IsDependency:
		summary.DependencyFiles++
	case class.IsConfig:
		summary.ConfigFiles++
	case class.IsGenerated:
		summary.GeneratedFiles++
	case class.IsVendor:
		summary.VendorFiles++
	case class.IsSource:
		summary.SourceFiles++
	}
	if class.IsSecurityRelated {
		summary.RiskyFiles++
	}
}

func languageForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "Go"
	case ".js", ".mjs", ".cjs":
		return "JavaScript"
	case ".ts", ".tsx":
		return "TypeScript"
	case ".jsx":
		return "JavaScript"
	case ".py":
		return "Python"
	case ".rb":
		return "Ruby"
	case ".rs":
		return "Rust"
	case ".java":
		return "Java"
	case ".kt", ".kts":
		return "Kotlin"
	case ".swift":
		return "Swift"
	case ".c", ".h":
		return "C"
	case ".cc", ".cpp", ".cxx", ".hpp":
		return "C++"
	case ".cs":
		return "C#"
	case ".php":
		return "PHP"
	case ".md", ".mdx":
		return "Markdown"
	case ".yaml", ".yml":
		return "YAML"
	case ".json":
		return "JSON"
	case ".toml":
		return "TOML"
	case ".tf":
		return "Terraform"
	case ".sh", ".bash", ".zsh":
		return "Shell"
	case ".sql":
		return "SQL"
	default:
		return "Other"
	}
}

func isTestPath(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, "_test.go") ||
		strings.HasSuffix(base, ".test.ts") ||
		strings.HasSuffix(base, ".test.tsx") ||
		strings.HasSuffix(base, ".spec.ts") ||
		strings.HasSuffix(base, ".spec.tsx") ||
		strings.HasSuffix(base, ".test.js") ||
		strings.HasSuffix(base, ".spec.js") ||
		strings.HasSuffix(base, ".test.mjs") ||
		strings.HasSuffix(base, ".spec.mjs") ||
		strings.HasSuffix(base, ".test.cjs") ||
		strings.HasSuffix(base, ".spec.cjs") ||
		strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py") ||
		strings.HasSuffix(base, "_test.py") ||
		strings.HasPrefix(path, "tests/") ||
		strings.HasPrefix(path, "spec/") ||
		strings.HasPrefix(path, "__tests__/")
}

func isDocsPath(path, base string) bool {
	return strings.HasPrefix(base, "readme") ||
		strings.HasPrefix(path, "docs/") ||
		strings.HasSuffix(path, ".md") ||
		strings.HasSuffix(path, ".mdx")
}

func isDependencyFile(path, base string) bool {
	switch base {
	case "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "go.mod", "go.sum",
		"requirements.txt", "poetry.lock", "pipfile", "pipfile.lock", "cargo.toml", "cargo.lock",
		"gemfile", "gemfile.lock", "pom.xml", "build.gradle":
		return true
	}
	return path == "pipfile" || path == "pipfile.lock"
}

func isConfigPath(path, base string) bool {
	switch base {
	case ".editorconfig", ".gitignore", ".gitattributes", ".npmrc", ".nvmrc",
		".prettierrc", ".prettierignore", ".eslintrc", ".golangci.yml", ".golangci.yaml",
		".goreleaser.yml", ".goreleaser.yaml", ".dockerignore", "makefile", "justfile",
		".contribution.yml", ".contribution.yaml", "lint-staged.config.js", "pnpm-workspace.yaml", "tsconfig.json", "jsconfig.json",
		"license", "licence", "copying", "notice":
		return true
	}
	return strings.HasPrefix(base, ".prettierrc.") ||
		strings.HasPrefix(base, ".eslintrc.") ||
		strings.HasPrefix(path, ".codex/")
}

func isExtensionlessScriptPath(path, base string) bool {
	return strings.HasPrefix(path, "scripts/") && filepath.Ext(base) == ""
}

func isVendorPath(path string) bool {
	return strings.HasPrefix(path, "vendor/") || strings.HasPrefix(path, "node_modules/")
}

func isGeneratedPath(path string) bool {
	base := filepath.Base(path)
	return strings.HasPrefix(path, "generated/") ||
		strings.Contains(base, ".generated.") ||
		strings.HasSuffix(base, ".pb.go") ||
		strings.Contains(base, ".gen.")
}

func isBuildArtifactPath(path string) bool {
	return strings.HasPrefix(path, "dist/") ||
		strings.HasPrefix(path, "build/") ||
		strings.HasPrefix(path, "coverage/")
}

func isInfrastructurePath(path, base string) bool {
	return base == "dockerfile" ||
		strings.HasPrefix(base, "docker-compose") ||
		strings.HasPrefix(path, ".github/") ||
		strings.HasPrefix(path, "terraform/") ||
		strings.HasSuffix(path, ".tf") ||
		strings.HasPrefix(path, "k8s/") ||
		strings.HasPrefix(path, "helm/") ||
		strings.HasPrefix(path, "charts/")
}

func isMigrationPath(path string) bool {
	return strings.Contains(path, "migration") || strings.Contains(path, "migrations/")
}

func isSecuritySensitivePath(path string) bool {
	keywords := []string{"auth", "oauth", "permission", "permissions", "rbac", "security", "secret", "secrets", "token", "tokens", "billing", "payment", "payments", "checkout", "session", "sessions", "crypto", "password", "passwords", "admin"}
	return containsAny(path, keywords)
}

func containsAny(value string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
