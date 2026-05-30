# Architecture

`contribution` is a Go CLI with a small command surface and a separate Node
tooling layer for repository automation.

## CLI layout

- `cmd/contribution` owns process setup and linker-provided build metadata.
- `internal/cli` owns command construction, stdout/stderr wiring, and command
  tests. It should delegate product behavior to narrow internal packages.
- `internal/analysis` owns the analyze workflow orchestration.
- `internal/preflight` owns changed-file policy, risk classification, coverage,
  personal pattern checks, and fail-on-risk decisions.
- `internal/fileclass` owns shared repository path classification used by git,
  receipt, and preflight flows.
- `internal/friend` owns friend-review packet creation and feedback import.
- `internal/report` owns report rendering, private report explainability,
  public-safe exports, and shared output format validation.
- `internal/receipt` owns PR quality cards, weakness map, trend comparison,
  deep dives, profile summary, and receipt confidence.
- `internal/privacy` owns redaction primitives. Callers should not create
  parallel public-safe sanitizers.
- Public Go packages should not be added until the project has a stable library
  contract.

## Tooling layout

- `scripts/codex-review-*` owns the Codex commit-review lane, queueing, push
  gating, and remediation.
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

## Social sharing boundary

The CLI may emit public-safe sharing artifacts such as `profile.export.json`
and `share-card.json`. `export-profile` is the dedicated command for writing
only those contract artifacts, while `redact` is the dedicated command for
regenerating public-safe JSON and markdown from an existing `analysis.json`.
Both reuse the same redaction engine as `analyze --public-safe` and
`report --public-safe`.

V2 workflow artifacts stay CLI-owned. `preflight.json` carries current-diff
readiness, changed-line coverage, recent personal pattern evidence, and rubric
evidence; `friend-review-packet.json` and `friend-feedback.export.json` bridge
public-safe human feedback. The root GitHub Action is a wrapper around local
CLI preflight only: it produces files and action outputs, but does not upload,
comment, host, or persist state.

Single-player coaching stays local-first. Private `report.md` may explain
artifact titles, high-churn files, no-test artifacts, PR durability evidence,
coverage import, optional analyzer findings, recent-vs-prior trend comparison,
and confidence setup actions. Public-safe analysis/redaction must neutralize
those details before profile, share-card, packet, or public-safe markdown
output.

The CLI should not contain hosted profile pages, OpenGraph rendering, X API
integrations, Discord-specific sharing code, share buttons, social mention
tracking, reply workers, auth, storage, or hosted background jobs.

Those website and social surfaces belong in the private Contribution.dev
website and web app repo, which consumes the CLI exports.
