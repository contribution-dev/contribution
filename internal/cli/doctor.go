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
	"github.com/contribution-dev/contribution/internal/repoguide"
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
			repo, repoErr := gitrepo.Resolve(ctx, ".")
			repoPath := ""
			if repoErr == nil {
				defer func() {
					_ = repo.Close()
				}()
				repoPath = repo.Path
			}
			tooling := tools.DiscoverForRepo(ctx, true, now, repoPath)
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
			} else if github.GHTokenAvailable() {
				fmt.Fprintln(&buf, "GitHub token: available from gh auth; pass --github-token gh to import PR metadata")
			} else {
				fmt.Fprintln(&buf, "GitHub token: unavailable; PR review metadata will be skipped")
			}
			var nextSteps []string
			coverageStepAdded := false
			if repoErr != nil {
				fmt.Fprintf(&buf, "Repo state: unavailable (%v)\n", repoErr)
			} else {
				fmt.Fprintf(&buf, "Repo state: ok (%s on %s)\n", repo.Name, repo.DefaultBranch)
				cfg, warnings, cfgErr := config.Load(repo.Path)
				if cfgErr != nil {
					fmt.Fprintf(&buf, "Config: invalid (%v)\n", cfgErr)
				} else {
					configPath := repo.Path + string(os.PathSeparator) + config.FileName
					if _, statErr := os.Stat(configPath); os.IsNotExist(statErr) {
						fmt.Fprintln(&buf, "Config: not found; safe defaults will be used")
						nextSteps = append(nextSteps, "Run `contribution init` to record repo-local defaults and preflight policy.")
					} else {
						fmt.Fprintf(&buf, "Config: ok (since_days=%d, max_prs=%d)\n", cfg.Analysis.SinceDays, cfg.Analysis.MaxPRs)
						if cfg.Coverage.Command != "" && cfg.Coverage.Path != "" {
							format := cfg.Coverage.Format
							if format == "" {
								format = "auto"
							}
							nextSteps = append(nextSteps, fmt.Sprintf("Run `contribution preflight --base %s --worktree --run-coverage` to generate %s with `%s` and import it as %s coverage.", repo.DefaultBranch, cfg.Coverage.Path, cfg.Coverage.Command, format))
							coverageStepAdded = true
						}
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
			if len(nextSteps) == 0 {
				nextSteps = append(nextSteps, "Run `contribution analyze --repo . --format all` for the private contribution receipt.")
			}
			if github.GHTokenAvailable() {
				nextSteps = append(nextSteps, "Run `contribution analyze --repo . --github-token gh --format all` to include GitHub PR metadata.")
			} else {
				nextSteps = append(nextSteps, "Set `GITHUB_TOKEN` or pass `--github-token gh` after `gh auth login` to include PR metadata.")
			}
			if !coverageStepAdded {
				nextSteps = append(nextSteps, repoguide.CoverageDoctorStep(repoPath))
			}
			fmt.Fprintln(&buf)
			fmt.Fprintln(&buf, "Next steps:")
			for _, step := range uniqueDoctorSteps(nextSteps) {
				fmt.Fprintf(&buf, "- %s\n", step)
			}
			_, writeErr := out.Write(buf.Bytes())
			return writeErr
		},
	}
}

func uniqueDoctorSteps(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
