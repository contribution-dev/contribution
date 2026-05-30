# contribution

`contribution` is an open-source CLI for private contribution-quality analysis.
It scans local Git evidence, writes a deterministic contribution report, and
emits public-safe artifacts that a separate web app can import.

The CLI is local-first. It does not upload raw code, publish profiles, call
social APIs, or store hosted state.

## Install

First, check whether Go is installed:

```bash
go version
```

Expected output looks like:

```text
go version go1.26.3 darwin/arm64
```

If you see `command not found: go`, install Go first. On macOS with Homebrew:

```bash
brew install go
```

Or use the official installer for your platform from
[go.dev/doc/install](https://go.dev/doc/install). After installing Go, open a
new terminal and rerun `go version`.

Then install the CLI directly from GitHub:

```bash
go install github.com/contribution-dev/contribution/cmd/contribution@latest
```

Make sure Go's binary install directory is on your `PATH`:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

To make that persistent in zsh:

```bash
echo 'export PATH="$(go env GOPATH)/bin:$PATH"' >> ~/.zshrc
```

Verify the install:

```bash
contribution version
```

A source install may print `contribution dev`, `commit: none`, and
`date: unknown`. That still confirms the binary is installed; release artifacts
include linker-provided version metadata.

Requirements for normal CLI use:

- Go 1.26.3 or newer to install from source
- Git available on `PATH`
- Optional analyzer tools for richer findings; missing optional tools are
  reported by `contribution doctor` and do not block local analysis

## Quickstart

Start in any Git repo you want to inspect:

```bash
cd /path/to/your/repo
contribution doctor
```

Run a first report without optional external analyzers:

```bash
contribution analyze \
  --repo . \
  --output /tmp/contribution-report \
  --format all \
  --no-external-tools
```

Expected output files:

```text
/tmp/contribution-report/analysis.json
/tmp/contribution-report/report.md
/tmp/contribution-report/profile.export.json
/tmp/contribution-report/share-card.json
/tmp/contribution-report/tooling.json
```

Read the markdown report:

```bash
sed -n '1,160p' /tmp/contribution-report/report.md
```

The default workflow writes artifacts only to the output directory you choose.
Use `/tmp/contribution-*` while testing so reports do not appear in your Git
working tree.

## Preflight Local Changes

Use `preflight` before review to inspect staged, unstaged, and untracked
non-ignored files:

```bash
cd /path/to/your/repo
contribution preflight \
  --base main \
  --worktree \
  --output /tmp/contribution-preflight \
  --format all \
  --no-external-tools
```

If your repo uses another default branch, replace `main` with that branch.

Expected output files:

```text
/tmp/contribution-preflight/preflight.json
/tmp/contribution-preflight/preflight.md
```

Read the preflight summary:

```bash
sed -n '1,180p' /tmp/contribution-preflight/preflight.md
```

This is the fastest everyday loop for checking whether a local change has
missing tests, risky files, large diffs, or review-readiness issues.

## Add Coverage

Coverage is optional, but it makes reports more useful. For Go repos:

```bash
go test ./... -coverprofile=coverage.out
contribution analyze \
  --repo . \
  --coverage coverage.out \
  --coverage-format go \
  --output /tmp/contribution-report-coverage \
  --format all \
  --no-external-tools
```

For JavaScript or TypeScript repos that produce LCOV:

```bash
contribution analyze \
  --repo . \
  --coverage coverage/lcov.info \
  --coverage-format lcov \
  --output /tmp/contribution-report-coverage \
  --format all \
  --no-external-tools
```

If you run `contribution init`, the CLI creates a `.contribution.yml` with safe
defaults and coverage hints for known repo types. Commit that file only if you
want shared repo configuration.

## Add GitHub Metadata

Reports are local-first without GitHub metadata. If you want PR enrichment, pass
a token reference:

```bash
contribution analyze \
  --repo . \
  --github-token gh \
  --output /tmp/contribution-report-github \
  --format all
```

`--github-token gh` asks the CLI to resolve a token from the GitHub CLI. Missing
or unavailable GitHub metadata degrades the report instead of failing local
analysis.

## What Gets Written

- `analyze` writes `analysis.json`, `report.md`, `profile.export.json`,
  `share-card.json`, and `tooling.json`.
- `preflight` writes `preflight.json` and `preflight.md`.
- `init` writes `.contribution.yml` in the current repo.
- Coverage commands may write repo-specific coverage artifacts such as
  `coverage.out`.

The CLI does not upload raw code, raw diffs, tokens, credentials, private repo
paths, or hosted state. Public-safe artifacts are designed to omit private
identifiers while preserving useful summary evidence.

## Useful Commands

```bash
# Check install and available tools.
contribution doctor

# Analyze a repo with only built-in local evidence.
contribution analyze --repo . --output /tmp/contribution-report --format all --no-external-tools

# Analyze current worktree changes before review.
contribution preflight --base main --worktree --output /tmp/contribution-preflight --format all --no-external-tools

# Generate default repo configuration.
contribution init

# Regenerate public-safe artifacts from an existing analysis.
contribution redact --input /tmp/contribution-report/analysis.json --output /tmp/contribution-redacted --format all
```

## Dogfood From A Clean Directory

This flow tests the GitHub install path without using a source checkout:

```bash
mkdir -p /tmp/contribution-clean-test
cd /tmp/contribution-clean-test
go install github.com/contribution-dev/contribution/cmd/contribution@latest
export PATH="$(go env GOPATH)/bin:$PATH"
contribution version
```

Then run the installed binary against a real repo:

```bash
cd /path/to/your/repo
contribution doctor
contribution analyze --repo . --output /tmp/contribution-clean-analyze --format all --no-external-tools
contribution preflight --base main --worktree --output /tmp/contribution-clean-preflight --format all --no-external-tools
```

Check that:

- The install command is clear and succeeds.
- `doctor` explains missing optional tools without blocking you.
- `report.md` is specific enough to be useful.
- Public-safe artifacts do not expose private paths, remotes, commit SHAs,
  emails, tokens, or raw code.
- No generated reports appear in your repo when `--output` points at `/tmp`.

## Development

Repository development uses Go for product code and Node/pnpm for automation.

Requirements for working on this repo:

- Go 1.26.3
- Node.js 24.16.0 and pnpm 11.4.0
- `golangci-lint` and `govulncheck` for the full local gate

The repo is bootstrapped to use local tools from `.tools/`. `pnpm` scripts load
that toolchain automatically. For direct shell use of `go`, `golangci-lint`, or
`govulncheck`, run:

```bash
source scripts/codex-env.sh
```

Bootstrap the repo:

```bash
pnpm install
pnpm tools:check
pnpm checks:changed
```

Run the CLI from source:

```bash
scripts/with-tools go run ./cmd/contribution analyze --repo . --output /tmp/contribution-report --format all --no-external-tools
scripts/with-tools go run ./cmd/contribution preflight --base main --head HEAD --output /tmp/contribution-preflight --format all
scripts/with-tools go run ./cmd/contribution preflight --base main --worktree --run-coverage --output /tmp/contribution-preflight --format all
```

For higher-confidence personal dogfooding, import coverage and GitHub metadata
when available. If `.contribution.yml` has coverage guidance and the configured
coverage artifact exists, `analyze` and `preflight` import it automatically:

```bash
go test ./... -coverprofile=coverage.out
scripts/with-tools go run ./cmd/contribution analyze --repo . --coverage coverage.out --coverage-format go --github-token gh --format all
```

Optional scanner evidence comes from locally installed tools. For this repo,
install pinned repo-local analyzer versions into `.tools/` with:

```bash
pnpm tools:install:optional
pnpm tools:optional:check
```

Common development commands:

```bash
pnpm test
scripts/with-tools go run ./cmd/contribution version
pnpm validate:final
pnpm review:status
```

## Commit Review

Every local commit is enqueued for Codex review by the `post-commit` hook. The
`pre-push` hook waits for required review evidence on pushed branch tips and
blocks pushes with unresolved major or blocker findings, including already-known
findings on older outgoing commits.

On macOS, install or repair durable review workers with:

```bash
pnpm review:recover
pnpm review:status
```

`review:status` includes worker health; an active queue item with no running
worker is reported as unhealthy after the warmup threshold. Review artifacts are
local-only under `.code-reviews/`.

To intentionally bypass local Git hooks for an emergency commit or push, use
Git's standard `--no-verify` flag and record the skipped validation in the PR.

## License

Apache-2.0. See [LICENSE](LICENSE).
