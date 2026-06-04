package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"time"

	"github.com/contribution-dev/contribution/internal/config"
	gitrepo "github.com/contribution-dev/contribution/internal/git"
)

func writef(out io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(out, format, args...)
	return err
}

func currentRepo(ctx context.Context) (gitrepo.Repo, error) {
	return gitrepo.Resolve(ctx, ".")
}

func outputRootForCurrent(flag string, repo gitrepo.Repo, cfg config.Config) (string, error) {
	if flag != "" {
		return filepath.Abs(flag)
	}
	return config.ResolveContainedOutputDir(repo.Path, cfg.Reports.OutputDir)
}

func timestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T150405Z")
}

func latestAnalysisPath(root string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(root, "*", "analysis.json"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", errors.New("no analysis.json found; run contribution analyze first")
	}
	sort.Strings(matches)
	return matches[len(matches)-1], nil
}
