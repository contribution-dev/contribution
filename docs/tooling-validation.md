# Tooling Validation Workflow

This repo uses Go for product code and Node/pnpm for repository automation.

## Default local workflow

- For code changes, use the smallest changed-aware command that covers the
  task. Default to `pnpm checks:changed`.
- For AGENTS or policy-doc changes, run `pnpm agents:check`.
- Use `pnpm validate:final` for full-gate verification or release-sensitive
  changes.
- CLI-contract-sensitive changes also run `pnpm dogfood:smoke` through
  changed-aware validation.

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
Go tests, race tests, build, CLI dogfood smoke, and vulnerability scan when the
required tools are available.

## Changed-aware commands

- `pnpm agents:check`
- `pnpm lint:changed`
- `pnpm typecheck:changed`
- `pnpm test:changed`
- `pnpm checks:changed`

Changed-aware commands fall back to broad checks for root config or tooling
changes.

## CLI dogfood

- `pnpm dogfood:smoke` builds the real CLI and exercises it against temporary
  Git repos under `/tmp/contribution-*`.
- `pnpm dogfood:release` runs the smoke flow, creates a GoReleaser snapshot,
  unpacks the current runner artifact, and runs a clean-environment smoke.
- `docs/cli-contract.md` is the compact contract map for user-visible command
  behavior and required coverage.

## Manual release-candidate dogfood

For meaningful product releases, optionally ask an AI agent to install or use
the candidate from public instructions in a clean workspace, analyze a realistic
repo, generate a public-safe report, and judge install clarity, command
discoverability, report usefulness, privacy confidence, and confusing output.
This is qualitative release confidence, not a deploy gate.
