# contribution

`contribution` is an open-source CLI for private contribution-quality analysis.
It scans local Git evidence, writes a deterministic contribution report, and
emits public-safe artifacts that a separate web app can import.

The CLI is local-first. It does not upload raw code, publish profiles, call
social APIs, or store hosted state.

## Requirements

- Go 1.26.3
- Node.js 24.16.0 and pnpm 11.4.0 for repository automation
- `golangci-lint` and `govulncheck` for the full local gate

The repo is bootstrapped to use local tools from `.tools/`. `pnpm` scripts and
Make targets load that toolchain automatically. For direct shell use of `go`,
`golangci-lint`, or `govulncheck`, run:

```bash
source scripts/codex-env.sh
```

## Development

```bash
pnpm install
pnpm tools:check
pnpm checks:changed
```

Run the CLI locally:

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

Core product commands:

- `contribution init` creates safe default `.contribution.yml` config with
  risky-path presets and coverage command hints when the repo type is known.
- `contribution doctor` reports required and optional tool availability.
- `contribution analyze` writes `analysis.json`, `report.md`,
  `profile.export.json`, `share-card.json`, and `tooling.json`, with optional
  Go/LCOV coverage import from flags or configured artifacts, GitHub durability
  enrichment, optional analyzer findings when tools are installed, and
  recent-vs-prior trend comparison for solo dogfooding.
- `contribution preflight` writes V2 current-diff readiness artifacts with
  changed-line ranges, optional Go/LCOV coverage from flags or configured
  artifacts, `--run-coverage` for generating configured coverage before import,
  bounded optional analyzer findings for changed files, policy rubric evidence,
  recent personal pattern checks, `--no-external-tools` for fast local-only
  runs, and a `--worktree` mode for staged, unstaged, and untracked local
  changes.
- `contribution packet` writes a public-safe V2 friend-review packet.
- `contribution import-feedback` imports public-safe friend feedback exports.
- `contribution export-profile` writes only public-safe web profile artifacts.
- `contribution redact` regenerates public-safe JSON and markdown from an
  existing `analysis.json`.

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
blocks pushes with unresolved major or blocker findings.

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
