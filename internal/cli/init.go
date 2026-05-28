package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/contribution-dev/contribution/internal/config"
	gitrepo "github.com/contribution-dev/contribution/internal/git"
	"github.com/spf13/cobra"
)

func newInitCommand(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create a private-by-default .contribution.yml.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			root, branch := initTarget(ctx)
			path := filepath.Join(root, config.FileName)
			if _, err := os.Stat(path); err == nil {
				fmt.Fprintf(out, "%s already exists\n", path)
				return nil
			} else if !os.IsNotExist(err) {
				return err
			}
			if err := config.WriteDefault(path, branch); err != nil {
				return err
			}
			fmt.Fprintf(out, "Created %s\n", path)
			return nil
		},
	}
}

func initTarget(ctx context.Context) (string, string) {
	cwd, err := os.Getwd()
	if err != nil {
		return ".", "main"
	}
	repo, err := gitrepo.Resolve(ctx, cwd)
	if err != nil {
		return cwd, "main"
	}
	defer func() {
		_ = repo.Close()
	}()
	return repo.Path, repo.DefaultBranch
}
