# Project pause

This repository is paused. The pause is intended to stop autonomous GitHub and
local review automation without changing published artifacts or deleting remote
state.

## Paused surfaces

- GitHub workflow triggers in `.github/workflows/*.yml` were reduced to
  `workflow_dispatch` only.
- `.github/dependabot.yml` was removed so Dependabot version-update PRs are not
  opened from repository config.
- Local Codex review LaunchAgents for this checkout were uninstalled:
  - `com.contribution.codex-review-worker.codex.*`
  - `com.contribution.codex-review-remediation.*`
  - `com.contribution.codex-review-watchdog.*`
- Local Git hooks were disabled for this checkout by setting
  `core.hooksPath=/dev/null` in `.git/config`.

## Restore

To resume repository automation, restore the removed workflow triggers and
Dependabot config from git history, then run:

```sh
pnpm review:launchctl install --lane all
```

Verify local workers with:

```sh
pnpm review:launchctl status --lane all
```

Restore Husky hooks for this checkout with:

```sh
git config --local core.hooksPath .husky/_
```

GitHub Actions can also be re-enabled from repository settings if they were
disabled remotely.
