# contribution

`contribution` is an open-source CLI for private agentic-readiness analysis. It
scans local Git evidence, writes a deterministic report, and emits public-safe
artifacts that a separate web app can import.

The CLI is local-first. It helps answer whether a repo is prepared for
AI-assisted development and which changes would make agents faster, safer, and
less likely to waste review time. It does not upload raw code, raw diffs, raw
AI transcripts, raw prompts, raw model outputs, terminal logs, publish profiles,
call social APIs, or store hosted state.

## Install

Install `contribution` once on your machine, then run it from any Git repo.
The repo you analyze does not need to be a Go project. Go is only required for
this source-install path.

First, check whether Go is installed on your machine:

```bash
go version
```

Expected output looks like:

```text
go version go1.26.4 darwin/arm64
```

If you see `command not found: go`, install Go first. On macOS with Homebrew:

```bash
brew install go
```

Or use the official installer for your platform from
[go.dev/doc/install](https://go.dev/doc/install). After installing Go, open a
new terminal and rerun `go version`.

Then install the CLI directly from GitHub. This installs a `contribution`
binary into Go's machine-level binary directory, usually `~/go/bin`:

```bash
go install github.com/contribution-dev/contribution/cmd/contribution@latest
```

Make sure Go's binary install directory is on your `PATH` for the current
terminal:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

To make that persistent in zsh, run this once:

```bash
echo 'export PATH="$(go env GOPATH)/bin:$PATH"' >> ~/.zshrc
```

Then open a new terminal, or run `source ~/.zshrc`.

Verify the install:

```bash
contribution version
```

If that prints `command not found: contribution`, your current shell still
cannot see Go's binary install directory. Run:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
command -v contribution
contribution version
```

If that works, make the `PATH` update persistent with the `~/.zshrc` command
above. You do not need to repeat this in every repo.

A source install may print `contribution dev`, `commit: none`, and
`date: unknown`. That still confirms the binary is installed; release artifacts
include linker-provided version metadata.

Requirements for normal CLI use:

- Go 1.26.4 or newer to install from source
- Git available on `PATH`
- A local Git repo to analyze
- Optional analyzer tools for richer findings; missing optional tools are
  reported by `contribution doctor` and do not block local analysis

## Quickstart

Run `doctor` from inside a Git repo you want to inspect:

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

Expected output files inside the timestamped run directory:

```text
/tmp/contribution-report/<run>/analysis.json
/tmp/contribution-report/<run>/report.md
/tmp/contribution-report/<run>/collector.bundle.json
/tmp/contribution-report/<run>/source-coverage.json
/tmp/contribution-report/<run>/attribution-readiness.json
/tmp/contribution-report/<run>/profile.export.json
/tmp/contribution-report/<run>/share-card.json
/tmp/contribution-report/<run>/tooling.json
```

Read the markdown report:

```bash
sed -n '1,160p' /tmp/contribution-report/<run>/report.md
```

You can also run `analyze` from anywhere by passing a repo path:

```bash
contribution analyze \
  --repo /path/to/your/repo \
  --output /tmp/contribution-report \
  --format all \
  --no-external-tools
```

The default workflow writes artifacts only to the output directory you choose.
Use `/tmp/contribution-*` while testing so reports do not appear in your Git
working tree.

## Preflight Local Changes

Use `preflight` before review to inspect staged, unstaged, and untracked
non-ignored files. Run it from the repo whose worktree you want to inspect:

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

## Web-App Probe Bundle

Use `probe` when the web app asks for a local collector bundle:

```bash
contribution probe \
  --repo . \
  --output /tmp/contribution-probe \
  --no-external-tools
```

The probe is public-safe by default and writes JSON artifacts only inside a
timestamped run directory, including `collector.bundle.json`,
`source-coverage.json`, `attribution-readiness.json`, and `tooling.json`.
Collector artifacts are always public-safe; `analysis.json` is redacted by
default unless you explicitly disable `--public-safe` for local debugging. The
CLI does not upload anything.

You can explicitly enrich the probe with metadata-only agent artifacts:

```bash
contribution probe \
  --repo . \
  --output /tmp/contribution-probe \
  --include-agent-artifacts \
  --agent-artifact /path/to/metadata.json
```

The CLI only imports supported metadata fields such as provider, session
fingerprint, branch, commit, token count, or cost. It does not store prompts,
completions, transcripts, raw logs, credentials, or raw code.

## AI Work Evidence Bundle

Use `evidence` only when you want the CLI to inspect local AI session artifacts.
This is explicit opt-in; `analyze` and `probe` do not scan `~/.claude/projects`
or `~/.codex/sessions`.

Preview what would be collected:

```bash
contribution evidence preview --repo .
```

Export a local offline bundle:

```bash
contribution evidence export --repo . --output /tmp/contribution-evidence
```

The export writes `ai-work-evidence.bundle.json` and `redaction-receipt.json`
inside a timestamped output directory. The default bundle is derived evidence
only: source tool, session timing, repo/branch/hashed commit anchors, summaries
when safe, steering/correction/test-debug counts, agent action counts, commit
SHA hashes, file path hashes, confidence, source lineage, and a redaction
receipt. It blocks raw prompts, model outputs, transcript JSONL, diffs,
terminal logs, source code, secrets, environment values, private keys, and
credential URLs by default.

Hosted upload is disabled until the CLI consumes a finalized website receiving
contract:

```bash
contribution evidence upload
```

## Work Unit Markers

Use a local work-unit marker when a branch or PR will not be a clean unit of
intent:

```bash
contribution work-unit start --goal "Build onboarding" --issue ENG-123
```

Markers are written to `.contribution/work-units/` by default and are never
staged automatically. They contain goal, branch, commit, and optional issue
metadata so future reports can group agentic work with better confidence.

Export markers when another system asks for them:

```bash
contribution work-unit export --repo . --output /tmp/contribution-work-units
```

## What Gets Written

- `analyze` writes `analysis.json`, `report.md`, `profile.export.json`,
  `share-card.json`, `tooling.json`, `collector.bundle.json`,
  `source-coverage.json`, and `attribution-readiness.json`.
- `probe` writes public-safe JSON collector artifacts for web-app import.
- `evidence export` writes `ai-work-evidence.bundle.json` and
  `redaction-receipt.json` after explicit opt-in local session discovery.
- `work-unit start` writes local intent markers under
  `.contribution/work-units/` unless another output directory is supplied.
- `work-unit export` writes `work-units.json`.
- `preflight` writes `preflight.json` and `preflight.md`.
- `init` writes `.contribution.yml` in the current repo.
- Coverage commands may write repo-specific coverage artifacts such as
  `coverage.out`.

The CLI does not upload raw code, raw diffs, raw AI transcripts, raw prompts,
raw model outputs, terminal logs, tokens, credentials, private repo paths, or
hosted state. Public-safe artifacts are designed to omit private identifiers
while preserving useful summary evidence.

## Useful Commands

```bash
# Check install and available tools.
contribution doctor

# Analyze a repo with only built-in local evidence.
contribution analyze --repo . --output /tmp/contribution-report --format all --no-external-tools

# Generate a public-safe collector bundle for the web app.
contribution probe --repo . --output /tmp/contribution-probe --no-external-tools

# Preview opt-in local AI work evidence without writing or uploading.
contribution evidence preview --repo .

# Mark a unit of intent before starting agentic work.
contribution work-unit start --goal "Build onboarding" --issue ENG-123

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
go version
go install github.com/contribution-dev/contribution/cmd/contribution@latest
export PATH="$(go env GOPATH)/bin:$PATH"
contribution version
```

Then run the installed binary against a real repo. You can analyze by path:

```bash
contribution analyze \
  --repo /path/to/your/repo \
  --output /tmp/contribution-clean-analyze \
  --format all \
  --no-external-tools
```

For `doctor` and `preflight`, `cd` into the target repo first:

```bash
cd /path/to/your/repo
contribution doctor
contribution preflight --base main --worktree --output /tmp/contribution-clean-preflight --format all --no-external-tools
```

Check that:

- The install command is clear and succeeds from a directory that is not the
  target repo.
- `contribution` works from any terminal after the Go bin directory is on
  `PATH`.
- `doctor` explains missing optional tools without blocking you.
- `report.md` is specific enough to be useful.
- Public-safe artifacts do not expose private paths, remotes, commit SHAs,
  emails, tokens, or raw code.
- No generated reports appear in your repo when `--output` points at `/tmp`.

## Development

Repository development uses Go for product code and Node/pnpm for automation.

Requirements for working on this repo:

- Go 1.26.4
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
