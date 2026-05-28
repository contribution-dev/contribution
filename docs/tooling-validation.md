# Tooling Validation Workflow

This repo uses Go for product code and Node/pnpm for repository automation.

## Default local workflow

- For code changes, use the smallest changed-aware command that covers the
  task. Default to `pnpm checks:changed`.
- For AGENTS or policy-doc changes, run `pnpm agents:check`.
- Use `pnpm validate:final` for full-gate verification or release-sensitive
  changes.

## Review automation

- `post-commit` enqueues every local commit for Codex review.
- `pre-push` waits for required review evidence and blocks unresolved major or
  blocker findings.
- On macOS, `pnpm tools:preflight` is the normal bootstrap and recovery
  entrypoint for durable review workers.
- `pnpm review:status` is the default operator status view.
- `pnpm review:recover` repairs launchd workers.

## Canonical final validation

Run:

```bash
pnpm validate:final
```

Fast rerun variant:

```bash
pnpm validate:final:skip-install
```

The command runs install when needed, AGENTS checks, formatting checks, Go vet,
Go tests, race tests, build, and vulnerability scan when the required tools are
available.

## Changed-aware commands

- `pnpm agents:check`
- `pnpm lint:changed`
- `pnpm typecheck:changed`
- `pnpm test:changed`
- `pnpm checks:changed`

Changed-aware commands fall back to broad checks for root config or tooling
changes.
