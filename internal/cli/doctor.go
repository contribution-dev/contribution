package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/contribution-dev/contribution/internal/config"
	gitrepo "github.com/contribution-dev/contribution/internal/git"
	"github.com/contribution-dev/contribution/internal/github"
	"github.com/contribution-dev/contribution/internal/tools"
	"github.com/spf13/cobra"
)

func newDoctorCommand(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check repo state and optional analysis tools.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			now := time.Now().UTC()
			tooling := tools.Discover(ctx, true, now)
			var buf bytes.Buffer
			fmt.Fprintln(&buf, "Contribution.dev doctor")
			fmt.Fprintln(&buf)
			fmt.Fprintln(&buf, "Tools:")
			for _, tool := range tooling.Tools {
				status := "missing"
				if tool.Available {
					status = "ok"
				}
				required := "optional"
				if tool.Required {
					required = "required"
				}
				version := tool.Version
				if version == "" {
					version = tool.Reason
				}
				fmt.Fprintf(&buf, "- %s: %s (%s) %s\n", tool.Name, status, required, version)
			}
			fmt.Fprintln(&buf)
			if token, ok := github.ResolveToken(""); ok && token != "" {
				fmt.Fprintln(&buf, "GitHub token: available from environment")
			} else {
				fmt.Fprintln(&buf, "GitHub token: unavailable; PR review metadata will be skipped")
			}
			repo, err := gitrepo.Resolve(ctx, ".")
			if err != nil {
				fmt.Fprintf(&buf, "Repo state: unavailable (%v)\n", err)
			} else {
				defer func() {
					_ = repo.Close()
				}()
				fmt.Fprintf(&buf, "Repo state: ok (%s on %s)\n", repo.Name, repo.DefaultBranch)
				cfg, warnings, cfgErr := config.Load(repo.Path)
				if cfgErr != nil {
					fmt.Fprintf(&buf, "Config: invalid (%v)\n", cfgErr)
				} else {
					configPath := repo.Path + string(os.PathSeparator) + config.FileName
					if _, statErr := os.Stat(configPath); os.IsNotExist(statErr) {
						fmt.Fprintln(&buf, "Config: not found; safe defaults will be used")
					} else {
						fmt.Fprintf(&buf, "Config: ok (since_days=%d, max_prs=%d)\n", cfg.Analysis.SinceDays, cfg.Analysis.MaxPRs)
					}
					for _, warning := range warnings {
						fmt.Fprintf(&buf, "- warning: %s\n", warning)
					}
				}
			}
			if len(tooling.Limitations) > 0 {
				fmt.Fprintln(&buf)
				fmt.Fprintln(&buf, "Degraded functionality:")
				for _, limitation := range tooling.Limitations {
					fmt.Fprintf(&buf, "- %s\n", limitation)
				}
			}
			_, err = out.Write(buf.Bytes())
			return err
		},
	}
}
