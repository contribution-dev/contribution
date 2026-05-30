package coverage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/contribution-dev/contribution/internal/privacy"
)

// RunCommand executes a configured coverage command from the repository root.
// It intentionally avoids shell expansion; commands that require pipes,
// redirects, or command chaining should live in a repo-owned script.
func RunCommand(ctx context.Context, repoRoot string, command string) error {
	args, err := SplitCommand(command)
	if err != nil {
		return err
	}
	executable, err := resolveExecutable(repoRoot, args[0])
	if err != nil {
		return err
	}
	// #nosec G204 -- executable comes from explicit local config and is executed without shell expansion.
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- coverage.command is parsed without shell syntax and the executable is resolved before execution.
	cmd := exec.CommandContext(ctx, executable, args[1:]...)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("coverage command timed out")
	}
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("coverage command failed: %s", truncateCommandOutput(privacy.RedactSecretLikeText(text)))
	}
	return nil
}

// SplitCommand tokenizes a simple command line without invoking a shell.
func SplitCommand(command string) ([]string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, fmt.Errorf("coverage.command is empty")
	}
	var args []string
	var current strings.Builder
	var quote rune
	for _, ch := range command {
		if ch == '\x00' || ch == '\n' || ch == '\r' {
			return nil, fmt.Errorf("coverage.command contains an unsupported control character")
		}
		if quote == 0 && strings.ContainsRune("|&;<>()", ch) {
			return nil, fmt.Errorf("coverage.command uses shell syntax; put complex coverage setup in a repo script")
		}
		switch {
		case quote == 0 && (ch == '\'' || ch == '"'):
			quote = ch
		case quote != 0 && ch == quote:
			quote = 0
		case quote == 0 && (ch == ' ' || ch == '\t'):
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(ch)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("coverage.command has an unterminated quote")
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("coverage.command is empty")
	}
	return args, nil
}

func resolveExecutable(repoRoot string, name string) (string, error) {
	if strings.TrimSpace(name) == "" || strings.HasPrefix(name, "-") {
		return "", fmt.Errorf("coverage.command has an invalid executable")
	}
	if strings.ContainsAny(name, `/\`) {
		candidate := name
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(repoRoot, candidate)
		}
		clean, err := filepath.Abs(candidate)
		if err != nil {
			return "", fmt.Errorf("resolve coverage executable: %w", err)
		}
		root, err := filepath.Abs(repoRoot)
		if err != nil {
			return "", fmt.Errorf("resolve repo root: %w", err)
		}
		rel, err := filepath.Rel(root, clean)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("coverage.command executable must stay inside the repository")
		}
		info, err := os.Stat(clean)
		if err != nil {
			return "", fmt.Errorf("coverage.command executable %s is unavailable: %w", name, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("coverage.command executable %s is a directory", name)
		}
		return clean, nil
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("coverage.command executable %s was not found on PATH", name)
	}
	return path, nil
}

func truncateCommandOutput(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= 400 {
		return value
	}
	return value[:400] + "..."
}
