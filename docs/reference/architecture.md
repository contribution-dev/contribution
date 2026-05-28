# Architecture

`contribution` is a Go CLI with a small command surface and a separate Node
tooling layer for repository automation.

## CLI layout

- `cmd/contribution` owns process setup and linker-provided build metadata.
- `internal/cli` owns command construction, stdout/stderr wiring, and command
  tests.
- Public Go packages should not be added until the project has a stable library
  contract.

## Tooling layout

- `scripts/codex-review-*` owns local commit review, queueing, push gating, and
  remediation.
- `scripts/run-changed-checks.mjs` routes changed-aware validation.
- `.github/workflows` owns CI, release, dependency audit, and review follow-up
  automation.

## Compatibility

The CLI should prefer stable flags and explicit output. Avoid hidden global
state in command handlers; pass dependencies through command construction so
tests can exercise behavior without process-level side effects.

## Product documentation

Private product strategy and implementation notes are not committed to the
public CLI repository. Public CLI contracts should be documented here or in
other public-safe docs when they stabilize.
