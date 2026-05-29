# contribution

`contribution` is an open-source CLI for private contribution-quality analysis.
It scans local Git evidence, writes a deterministic contribution report, and
emits public-safe artifacts that a separate web app can import.

The CLI is local-first. It does not upload raw code, publish profiles, call
social APIs, or store hosted state.

## Requirements

- Go 1.26.3
- Node.js 22.22.2 and pnpm 10.20.0 for repository automation
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

Core product commands:

- `contribution init` creates safe default `.contribution.yml` config.
- `contribution doctor` reports required and optional tool availability.
- `contribution analyze` writes `analysis.json`, `report.md`,
  `profile.export.json`, `share-card.json`, and `tooling.json`.
- `contribution preflight` writes V2 current-diff readiness artifacts with
  changed-line ranges, optional Go/LCOV coverage, and policy rubric evidence.
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
