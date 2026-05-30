# Tooling Validation Workflow

This repo uses Go for product code and Node/pnpm for repository automation.

## Toolchain baseline

- Product code builds with Go 1.26.3.
- Repository automation runs on Node.js 24 LTS with pnpm 11.4.0.
  `pnpm tools:check` enforces Node.js `>=24.16.0 <25`, pnpm `>=11.4.0`, and
  Go `>=1.26.3`.
- `scripts/with-tools` sources `scripts/codex-env.sh`, which uses `.nvmrc`
  through `fnm` when `fnm` is installed. Prefer `scripts/with-tools pnpm ...`
  in shells that are not already on the repo's Node version.

## Default local workflow

- For code changes, use the smallest changed-aware command that covers the
  task. Default to `pnpm checks:changed`.
- Use `pnpm tools:install:optional` when optional analyzer findings should be
  available locally. It installs pinned Semgrep, Gitleaks, OSV Scanner, and
  Trivy versions into `.tools/`; use `pnpm tools:optional:check` to verify
  them without reinstalling. The CLI checks repo-local `.tools/` paths after
  `PATH`; use `scripts/with-tools ...` or source `scripts/codex-env.sh` when
  you also want the shell and package scripts on the repo toolchain. The
  `pnpm tools:check` command reports missing analyzer tools with that
  bootstrap command.
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
- Risk-policy finding limits use a bounded default when `MAX_FINDINGS` or
  `--max-findings` is missing, invalid, or too large, so malformed inputs must
  not suppress all actionable review findings.
- Stale active-job recovery requeues only the originally observed worker claim;
  it must not remove an active job that another worker claimed concurrently.
- On macOS, `pnpm tools:check` verifies durable review workers without changing
  launchd state.
- `pnpm review:status` is the default operator status view. It includes a
  worker-health line; an active queue item with no running Codex worker is
  `unhealthy` after the warmup threshold and should be followed by
  `pnpm review:recover`.
- `pnpm review:queue:backlog` prints the current parked-backlog status by
  default. Pass `--freeze-existing-pending`, `--enqueue-after`, or `--clear`
  only when intentionally changing backlog admission state.
- `pnpm review:recover` repairs launchd workers. Bootstrap failures include
  plist lint, launchctl status, and recent launchd log diagnostics when
  available.

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

## Benchmarks

Use `pnpm bench` to run the repository benchmark suite. Benchmarks currently
cover Git inventory, receipt generation, report bundle generation, and
preflight report construction.

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
  Git repos under `/tmp/contribution-*`, including V2 preflight coverage command
  execution,
  analyze-time coverage import, preflight analyzer-finding schema coverage,
  recent-vs-prior trend comparison schema coverage, packet, feedback-import
  flows, personal preflight context, and public-safe report quality checks.
- `pnpm dogfood:real` builds the real CLI, analyzes this repository into
  `/tmp/contribution-*`, checks inventory against Git-visible files, confirms
  local-only confidence is not `high`, confirms trend comparison status is
  present, and asserts public-safe outputs do not contain local roots, remotes,
  commit SHAs, tokens, emails, private paths, stale phase wording, or blank
  PR-ledger risk/action cells.
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
