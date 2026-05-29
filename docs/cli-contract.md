# CLI Contract

This document records the user-visible behavior that must stay covered as the
`contribution` CLI evolves. It is intentionally compact; executable coverage
lives in Go tests and `scripts/dogfood-cli.mjs`.

## Global behavior

- Commands must be deterministic about stdout, stderr, exit status, and output
  files.
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
- The CLI may generate `profile.export.json` and `share-card.json`, but it must
  not publish profiles, render OpenGraph images, call X or Discord APIs, track
  social mentions, run reply workers, or store hosted social state.

## Commands

| Command           | Contract                                                                                                                                                                                                        | Required coverage                                       |
| ----------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------- |
| root/help         | No args exits 0 and prints command help to stdout.                                                                                                                                                              | Go command test and dogfood smoke.                      |
| `version`         | Exits 0 and prints version, commit, and date fields. Release artifacts must use linker-provided values.                                                                                                         | Go command test, dogfood smoke, release artifact smoke. |
| `init`            | Creates `.contribution.yml` in the current Git repo, uses a detected default branch when available, and is idempotent.                                                                                          | Dogfood smoke.                                          |
| `doctor`          | Exits 0 in a Git repo, reports required/optional tool status, and never prints token values. Missing optional tools are non-fatal.                                                                              | Dogfood smoke and release artifact smoke.               |
| `analyze`         | Analyzes `--repo` or the current repo, always writes canonical `analysis.json`, respects `--format` for markdown report generation, and continues without optional tools or GitHub metadata.                    | Go analysis tests, dogfood smoke, real-repo dogfood.    |
| `report`          | Requires `--input`, validates `--format`, regenerates report/profile/share artifacts, and honors `--public-safe`.                                                                                               | Go command test and dogfood smoke.                      |
| `export-profile`  | Requires `--input` and `--output`, writes only public-safe `profile.export.json` and `share-card.json`, and never writes report artifacts.                                                                      | Go command test and dogfood smoke.                      |
| `redact`          | Requires `--input` and `--output`, validates `--format`, and writes public-safe JSON, markdown, profile, and share artifacts.                                                                                   | Go command test and dogfood smoke.                      |
| `preflight`       | Validates `--format`, `--coverage-format`, and `--fail-on-risk`; compares `--base` and `--head`; imports optional Go/LCOV coverage; writes V2 preflight artifacts; exits nonzero for risk only when configured. | Go command test, dogfood smoke, action contract test.   |
| `packet`          | Requires `--pr`, reads the latest analysis under `--output`, writes V2 friend-review packet artifacts with stable packet IDs and structured rubric, and redacts by default.                                     | Go command test and dogfood smoke.                      |
| `import-feedback` | Requires `--analysis`, `--feedback`, and `--output`; imports public-safe `friend-feedback.export.json` files as feedback signals; validates `--format`; and honors `--public-safe`.                             | Go command test and dogfood smoke.                      |

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
- `profile.export.json` and `share-card.json` are the stable public-safe export
  contract for the separate website/app repo. The CLI does not upload or host
  them.
- `analysis.json`, `profile.export.json`, `share-card.json`, `preflight.json`,
  `friend-review-packet.json`, and `friend-feedback.export.json` have
  behavior-level contract tests for their top-level JSON shape.
- `preflight.json` is V2. It includes structured changed files, additions and
  deletions, new-side changed line ranges, total changed lines, optional
  changed-line coverage, and rubric items. Missing coverage is `unknown`, not
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
