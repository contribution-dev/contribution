# Changelog

All notable changes to this project will be documented in this file.

## Unreleased

- Add single-player dogfooding improvements: private report ledger
  explainability, high-churn and no-test deep dives, analyze-time Go/LCOV
  coverage import, concrete confidence setup actions, GitHub CLI token support,
  richer GitHub PR review/check/file metadata, and personal preflight pattern
  checks.
- Lock generated artifact JSON contract shapes, remove the placeholder
  `privacy.upload_enabled` field, and guard review automation lanes as
  Codex-only.
- Remove obsolete review-state migration paths so review automation uses the
  canonical `.code-reviews` Codex queue directly.
- Add direct `internal/analysis.Run` coverage for output format behavior and
  GitHub metadata degradation; `analyze --format markdown` now still writes the
  canonical `analysis.json`.
- Split CLI command wiring from analysis, preflight, friend-review, report, and
  privacy behavior; remove stale config options, obsolete review scripts, the
  unused UI-runtime review lane, and duplicate helper logic.
- Harden public-safe exports by redacting emails and path evidence across the
  full analysis payload, not just local signal records.
- Add regression coverage for config validation, GitHub metadata fetching,
  tool discovery, preflight policy, friend feedback validation, and shared
  review severity handling.
- Harden public-safe markdown reports so redacted PR-ledger rows keep neutral
  risk/action fallback text and generated report copy no longer uses stale V1
  phase wording.
- Add V2 preflight artifacts with structured changed files, changed-line
  coverage import, rubric evidence, repo policy checks, fail-on-risk behavior,
  and a reusable GitHub Action wrapper.
- Add V2 friend-review packets and `import-feedback` for public-safe feedback
  exports, including feedback usefulness signals based on specificity and
  completeness.
- Make repository inventory Git-aware, add config-file counts, line-aware
  history scope, confidence caps for local-only conclusions, and stricter
  public-safe SHA/title redaction.
- Add `export-profile`, `redact`, and real-repo CLI dogfood coverage for the
  public-safe export contract.
- Add repo-owned CLI dogfood, contract coverage routing, and release artifact
  smoke validation.
- Make final validation headless-safe and include Node script regression tests
  in final and CI gates.
- Rename the local CI-style package script to `ci:local` and route CLI contract
  coverage through file-list input to keep changed-aware logs readable.
- Redact authorization bearer credentials and repo-relative path evidence from
  public-safe outputs.
- Redact public-safe analysis output directories and credential-bearing remote
  URLs in clone errors.
- Redact credentialed remotes from stored analysis metadata, sanitize
  regenerated public-safe reports, and dogfood credential/error leak paths.
- Fix `analyze --format json` completion output so it does not point at a
  markdown report that was not written.
- Add the V1 local contribution intelligence CLI foundation with private
  analysis reports, public-safe profile exports, preflight packets, and
  environment diagnostics.
- Bootstrap the Go CLI repository with CI, release, validation, and commit
  review automation.
