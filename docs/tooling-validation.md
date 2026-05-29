# Tooling Validation Workflow

This repo uses Go for product code and Node/pnpm for repository automation.

## Toolchain baseline

- Product code builds with Go 1.26.3.
- Repository automation runs on Node.js 24 LTS with pnpm 11.4.0.
  `pnpm tools:preflight` enforces Node.js `>=24.16.0 <25`, pnpm `>=11.4.0`,
  and Go `>=1.26.3`.

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
- Codex review is the only active review lane. Do not add dormant lane plumbing
  without a runnable producer, gate, and tests.
- Review state is canonical under `.code-reviews` with Codex queue jobs in
  `.code-reviews/queue/codex/{pending,active}`. Historical `.codex/reviews`
  roots and top-level `queue/{pending,active}` layouts are not auto-migrated.
- Review severity parsing and rank comparisons are centralized in
  `scripts/lib/review-severity.mjs`; control-plane and risk-policy scripts
  should import that helper instead of carrying local rank tables.
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

The command runs install when needed, AGENTS checks, script tests, formatting
checks, Go vet, Go tests, race tests, build, CLI dogfood smoke, real-repo CLI
dogfood, and
vulnerability scan when the required tools are available. Its install step runs
with `CI=true` when `CI` is unset and `HUSKY=0`, so the full gate is safe in
headless shells and does not rewrite local Git hook state.

## Local CI parity

Use `pnpm ci:local` when you want the CI-style fast gate without the final
validation install and vulnerability-scan steps. Do not use `pnpm ci`; that is a
pnpm built-in command, not a repo script.

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
  Git repos under `/tmp/contribution-*`, including V2 preflight coverage,
  analyze-time coverage import, analyzer-finding schema coverage, packet,
  feedback-import flows, personal preflight context, and public-safe report
  quality checks.
- `pnpm dogfood:real` builds the real CLI, analyzes this repository into
  `/tmp/contribution-*`, checks inventory against Git-visible files, confirms
  local-only confidence is not `high`, and asserts public-safe outputs do not
  contain local roots, remotes, commit SHAs, tokens, emails, private paths,
  stale phase wording, or blank PR-ledger risk/action cells.
- `pnpm dogfood:release` runs the smoke flow, creates a GoReleaser snapshot,
  unpacks the current runner artifact, and runs a clean-environment smoke. This
  current-runner artifact check is the intended default; add cross-OS artifact
  execution only if release risk justifies the extra CI cost.
- `docs/cli-contract.md` is the compact contract map for user-visible command
  behavior and required coverage. CLI architecture references under
  `docs/reference/architecture.md` also count as contract evidence when they
  define CLI boundaries.
- `action.yml` is covered by script contract tests and CI dogfoods it with
  `version: local`; the action should remain a thin wrapper around
  `contribution preflight`.

## Manual release-candidate dogfood

For meaningful release candidates, optionally ask an AI agent to install or use
the candidate from public instructions in a clean workspace, analyze a realistic
repo, generate a public-safe report, and judge install clarity, command
discoverability, report usefulness, privacy confidence, and confusing output.
This is qualitative release confidence, not an automated deploy gate or a check
for every patch release.
