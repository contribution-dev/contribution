# CLI Contract

This document records the user-visible behavior that must stay covered as the
`contribution` CLI evolves. It is intentionally compact; executable coverage
lives in Go tests and `scripts/dogfood-cli.mjs`.

## Global behavior

- Commands must be deterministic about stdout, stderr, exit status, and output
  files.
- Successful `analyze`, `probe`, and `preflight` runs print concise terminal
  summaries with the main result, follow-up movement when available, next action
  context, capped unavailable-signal notes, and only the artifact paths actually
  written for the selected format.
- Errors return a non-zero exit code, write the error to stderr at process
  level, and do not create unrelated artifacts.
- Public-safe outputs must omit raw code, raw diffs, author emails, secrets,
  tokens, private repo roots, private remotes, `head_sha`, commit SHAs, and raw
  commit or PR titles. Artifact labels use neutral public text such as
  `Artifact 1` or `PR #123`.
- Artifact privacy objects record only exposure posture:
  `public_safe`, `raw_code_included`, `raw_diffs_included`,
  `private_paths_included_in_public_export`, and `author_emails_included`.
  They do not include upload, publish, hosted-state, or destination controls.
- Public-safe markdown must remain useful after redaction. When private
  risk/action details are omitted from JSON, rendered tables use neutral
  fallback text instead of blank cells.
- Missing optional tools or GitHub metadata should degrade reports, not fail
  local analysis.
- Optional analyzer execution is bounded. Secret scanning covers Git history
  and a filtered worktree copy of Git-visible tracked and untracked non-ignored
  files, excluding generated, dependency, tool, and report paths.
- Private markdown may show artifact titles, file paths, high-churn details,
  and no-test evidence. Public-safe markdown must keep those details redacted
  to neutral artifact labels or path basenames.
- The CLI may generate `profile.export.json`, `share-card.json`, and
  public-safe collector bundle artifacts, but it must not publish profiles,
  render OpenGraph images, call X or Discord APIs, track social mentions, run
  reply workers, upload bundles, or store hosted social state.

## Install and Invocation

- The documented source install path is
  `go install github.com/contribution-dev/contribution/cmd/contribution@latest`.
  Source installs may report development linker metadata, but
  `contribution version` must still be a valid install verification command.
- `contribution` is installed once on the user's machine and can run against any
  local Git repository. Commands that inspect the current worktree, such as
  `doctor` and `preflight --worktree`, expect to run inside the target Git repo.
- Commands with an explicit repo option, such as `analyze --repo`, must work
  from outside the target repository when given a local repo path.

## Commands

| Command           | Contract                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | Required coverage                                       |
| ----------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------- |
| root/help         | No args exits 0 and prints command help to stdout with the agentic-readiness summary.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         | Go command test and dogfood smoke.                      |
| `version`         | Exits 0 and prints version, commit, and date fields. Release artifacts must use linker-provided values.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | Go command test, dogfood smoke, release artifact smoke. |
| `init`            | Creates `.contribution.yml` in the current Git repo, uses a detected default branch when available, includes risky-path presets and repo-type coverage guidance where practical, and is idempotent.                                                                                                                                                                                                                                                                                                                                                                                                                                           | Dogfood smoke.                                          |
| `doctor`          | Exits 0 in a Git repo, reports required/optional tool status, and never prints token values. Missing optional tools are non-fatal.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            | Dogfood smoke and release artifact smoke.               |
| `analyze`         | Analyzes `--repo` or the current repo, always writes canonical `analysis.json`, readiness/source/attribution collector artifacts, profile/share artifacts, and tooling data; respects `--format`; optionally imports Go/LCOV coverage from flags or an existing configured artifact; supports explicit `--github-token gh`; imports optional analyzer findings when tools are available; can import metadata-only agent artifacts with explicit opt-in; compares recent and prior local history windows; and continues without optional tools or GitHub metadata.                                                                             | Go analysis tests, dogfood smoke, real-repo dogfood.    |
| `probe`           | Runs a public-safe JSON-only local collector for web-app import. It writes `analysis.json`, `collector.bundle.json`, `source-coverage.json`, `attribution-readiness.json`, profile/share artifacts, and `tooling.json`; does not upload; defaults to public-safe output; supports optional GitHub metadata; and requires `--include-agent-artifacts` before reading any explicit `--agent-artifact` metadata path.                                                                                                                                                                                                                            | Go command test and dogfood smoke.                      |
| `work-unit`       | `work-unit start --goal` creates a local marker under `.contribution/work-units/` by default and never stages it. `work-unit export` writes `work-units.json` for local marker import. Markers contain intent metadata only: goal, branch, commit, optional issue, repo fingerprint, and privacy classification.                                                                                                                                                                                                                                                                                                                              | Go command test and dogfood smoke.                      |
| `report`          | Requires `--input`, validates `--format`, regenerates report/profile/share artifacts, and honors `--public-safe`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             | Go command test and dogfood smoke.                      |
| `export-profile`  | Requires `--input` and `--output`, writes only public-safe `profile.export.json` and `share-card.json`, and never writes report artifacts.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    | Go command test and dogfood smoke.                      |
| `redact`          | Requires `--input` and `--output`, validates `--format`, and writes public-safe JSON, markdown, profile, and share artifacts.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | Go command test and dogfood smoke.                      |
| `preflight`       | Validates `--format`, `--coverage-format`, and `--fail-on-risk`; compares `--base` and `--head` or, with `--worktree`, compares `--base` against staged, unstaged, and untracked worktree changes; imports optional Go/LCOV coverage from flags or an existing configured artifact; with `--run-coverage`, runs configured `coverage.command` without shell expansion before importing coverage; imports bounded optional analyzer findings for changed files unless `--no-external-tools` is set; adds recent personal pattern checks when history is available; writes V2 preflight artifacts; exits nonzero for risk only when configured. | Go command test, dogfood smoke, action contract test.   |
| `packet`          | Requires `--pr`, reads the latest analysis under `--output`, writes V2 friend-review packet artifacts with stable packet IDs and structured rubric, and redacts by default.                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | Go command test and dogfood smoke.                      |
| `import-feedback` | Requires `--analysis`, `--feedback`, and `--output`; imports public-safe `friend-feedback.export.json` files as feedback signals; validates `--format`; and honors `--public-safe`.                                                                                                                                                                                                                                                                                                                                                                                                                                                           | Go command test and dogfood smoke.                      |

## Signal and export schema

- Repository inventory is based on Git's visible file set: tracked files plus
  untracked non-ignored files from `git ls-files`. Ignored local artifacts are
  excluded through Git ignore rules, and missing deleted paths are skipped.
- `inventory.config_files` is additive and backward-compatible. Existing
  consumers can continue to use `by_class`, `source_files`, `test_files`, and
  related counts.
- Local history cards use `git log --numstat` line counts for changed-file
  scope. Local-only weakness-map and profile confidence is capped at `medium`;
  `high` is reserved for enough direct evidence plus relevant enrichment.
- When GitHub PR metadata is available, unmatched local commits can still
  appear as cards so direct merges and sparse PR metadata do not hide local
  evidence. Merge commit SHAs are used to avoid duplicates when available, and
  limitations must call out workflows that cannot be matched precisely.
- `profile.export.json` and `share-card.json` are the stable public-safe export
  contract for the separate website/app repo. Profile exports prefer non-risky
  selected artifacts and must not expose risky artifacts by default. The CLI
  does not upload or host them.
- `analysis.json`, `collector.bundle.json`, `source-coverage.json`,
  `attribution-readiness.json`, `profile.export.json`, `share-card.json`,
  `preflight.json`, `friend-review-packet.json`, and
  `friend-feedback.export.json` have
  behavior-level contract tests for their top-level JSON shape.
- `analysis.json` includes `coverage`, `analyzer_findings`, `trends`,
  `follow_up`, `deep_dives`, and `setup_actions`. `coverage` summarizes
  explicitly imported Go/LCOV coverage, including the lowest-coverage files.
  `analyzer_findings` stores normalized optional-tool findings without raw
  code. `trends` compares the recent local-history window with the immediately
  prior window for test evidence, large changes, fix/revert-like churn, risky
  untested changes, and high-churn concentration. `follow_up` compares the
  current report with the latest prior local `analysis.json` under the same
  output root and records improved, regressed, resolved, and persistent
  patterns. `deep_dives` explains high-churn and source-without-test patterns
  for the private report. `setup_actions` gives concrete commands that would
  raise confidence.
- `analysis.json` also includes additive value-pipeline fields:
  `agentic_readiness`, `source_coverage`, `data_gaps`,
  `recommended_connections`, `attribution_readiness`,
  `work_unit_candidates`, `agent_artifacts` when imported, and
  `privacy_summary`. These fields are deterministic and must not claim token or
  cost ROI unless explicit telemetry or metadata supplies that numeric evidence.
- `collector.bundle.json` is public-safe by construction. It includes schema
  version, generated time, redacted repo metadata, local git summary, tooling,
  agentic readiness, source coverage, data gaps, recommended connections,
  attribution readiness, candidate work units, optional metadata-only agent
  artifact summaries, setup actions, limitations, and privacy posture.
- `preflight.json` is V2. It includes structured changed files, additions and
  deletions, new-side changed line ranges, total changed lines, optional
  changed-line coverage, bounded optional `analyzer_findings` for changed
  files, rubric items, and optional `personal_context` when recent local history
  is available. `--run-coverage` is explicit opt-in and rejects shell chaining,
  redirects, and control characters in `coverage.command`; complex coverage
  setup belongs in a repo-owned script. `--worktree` includes staged, unstaged,
  and untracked non-ignored files. Missing coverage is `unknown`, not
  `uncovered`.
- `friend-review-packet.json` is V2. It includes a stable `packet_id`, neutral
  public-safe artifact labels by default, and structured reviewer rubric
  questions.
- `friend-feedback.export.json` is V1. It must be public-safe and contains a
  packet id, submitted time, overall trust, confidence, optional reviewer label,
  and rubric answers. Imported usefulness is based on specificity and
  completeness, not reviewer identity.

## GitHub Action

The root `action.yml` wraps `contribution preflight` for pull request workflows.
It supports `version: local` for repo dogfood and released versions through
`go install`. The action writes `risk-level`, `artifact-dir`,
`preflight-json`, and `preflight-markdown` outputs, and appends markdown to the
GitHub step summary. It does not upload artifacts or comment on PRs by itself.

## Updating the contract

When changing command names, flags, stdout/stderr text, exit behavior, generated
artifacts, report schemas, privacy posture, or release packaging, update at
least one matching coverage artifact in the same change:

- Go tests under `internal/**`
- `scripts/dogfood-cli.mjs`
- this contract document
- workflow or validation docs that route the new behavior
