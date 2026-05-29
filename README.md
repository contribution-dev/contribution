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
pnpm tools:preflight
pnpm checks:changed
```

Run the CLI locally:

```bash
scripts/with-tools go run ./cmd/contribution analyze --repo . --output /tmp/contribution-report --format all --no-external-tools
scripts/with-tools go run ./cmd/contribution preflight --base main --head HEAD --output /tmp/contribution-preflight --format all
```

For higher-confidence personal dogfooding, import coverage and GitHub metadata
when available:

```bash
go test ./... -coverprofile=coverage.out
scripts/with-tools go run ./cmd/contribution analyze --repo . --coverage coverage.out --coverage-format go --github-token gh --format all
```

Core product commands:

- `contribution init` creates safe default `.contribution.yml` config with
  risky-path presets and coverage command hints when the repo type is known.
- `contribution doctor` reports required and optional tool availability.
- `contribution analyze` writes `analysis.json`, `report.md`,
  `profile.export.json`, `share-card.json`, and `tooling.json`, with optional
  Go/LCOV coverage import, GitHub durability enrichment, and optional analyzer
  findings when tools are installed.
- `contribution preflight` writes V2 current-diff readiness artifacts with
  changed-line ranges, optional Go/LCOV coverage, policy rubric evidence, and
  recent personal pattern checks.
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
`pre-push` hook waits for required review evidence and blocks pushes with
unresolved major or blocker findings.

On macOS, install or repair durable review workers with:

```bash
pnpm review:recover
pnpm review:status
```

Review artifacts are local-only under `.code-reviews/`.

## License

Apache-2.0. See [LICENSE](LICENSE).
