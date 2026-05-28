# contribution

`contribution` is an open-source CLI for contribution workflows.

The project is intentionally small today: the repository is wired for Go
development, release automation, CI, and the local commit-review workflow. New
product commands should start under `internal/cli` and be exposed from
`cmd/contribution`.

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

Common commands:

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
