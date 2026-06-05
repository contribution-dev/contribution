# Changelog

All notable changes to this project will be documented in this file.

## Unreleased

- Keep public-safe share-card highlights from falling back to raw Top Read
  weakness labels; generated Top Read findings now map to positive labels or
  safe fallback highlights.
- Put `Top Read` first in generated markdown reports and make low-evidence
  share cards use useful readiness/setup highlights instead of duplicate
  placeholder copy while aligning share-card confidence with the displayed
  readiness score and deduplicating repeated report limitation notes.
- Harden security defaults: repo-local optional analyzer binaries now require
  explicit local trust and are never trusted for remote clones, GitHub metadata
  import requires an explicit `--github-token` source, config-derived report
  output directories must stay repo-contained, public-safe exports remove
  agent telemetry, and review automation now requires trusted finding authors
  plus same-repository PRs before mutating pull requests.
- Bump the repository Go toolchain baseline to 1.26.4 so `govulncheck` no
  longer reports the fixed Go standard-library vulnerabilities present in
  1.26.3.
- Add a public-safe share-card handoff to `analyze`, `probe`, `report`, and
  `export-profile` terminal output, pointing developers at
  `https://contribution.dev/share` with the generated profile/share artifact
  paths.
- Keep one-off fix-like commits from being promoted above stronger Top Read
  findings such as large work units.
- Add a `PR Inspection Priorities` digest before the full ledger, align
  readiness-gap ordering with the Top Read finding, make contribution.dev
  handoffs specific to the missing context, and treat zero-commit local evidence
  as neutral context instead of a profile strength.
- Improve report readability with magnitude-aware repair-loop top-read ranking,
  singular/plural finding evidence, readiness-only source gap prioritization,
  a repeated end-of-report next PR plan, and non-salesy contribution.dev
  handoffs for evidence that requires web-connected sources.
- Add deterministic `top_read` summaries to `analysis.json`,
  `collector.bundle.json`, markdown reports, and terminal receipts, and label
  imported PRs with no changed-file metadata as `insufficient_data` instead of
  treating sparse metadata as proof of a focused PR.
- Reposition `analyze` around agentic readiness, add deterministic
  `agentic_readiness`, `source_coverage`, `data_gaps`,
  `recommended_connections`, `attribution_readiness`,
  `work_unit_candidates`, and `privacy_summary` fields, and emit public-safe
  `collector.bundle.json`, `source-coverage.json`, and
  `attribution-readiness.json` artifacts for web-app import.
- Add `probe` for public-safe local collector bundles, metadata-only
  `--agent-artifact` import with explicit opt-in, and `work-unit start/export`
  for optional local intent markers that improve future work-unit attribution.
- Harden collector follow-ups so public-safe redaction preserves useful
  slash-separated product terms, work-unit candidates omit empty anchors, and
  exported marker bundles are not reread as malformed source markers.
- Redact unknown two-segment slash path candidates in public-safe text while
  preserving known slash-separated product terms.
- Add a `follow_up` report comparison that checks the latest prior local
  `analysis.json`, shows what improved, regressed, resolved, or persisted since
  the last report, and surfaces that loop in markdown and terminal summaries.
- Add terminal receipt summaries for `analyze` and `preflight`, including
  confidence, top evidence, next actions, capped unavailable signals, and
  format-aware artifact paths while keeping durable report files on disk.
- Harden public-safe boundaries and foundation checks: public-safe analysis and
  packet transformations now live outside report rendering, worktree preflight
  no longer follows untracked symlinks for line evidence, coverage import is
  bounded and deterministic for ambiguous suffix matches, and changed-aware CLI
  contract routing includes all core behavior packages.
- Harden local foundation checks and shared CLI internals: `pnpm tools:check`
  now verifies local tooling without repairing launchd workers, `pnpm build`
  creates `bin/` in clean checkouts, GitHub enrichment paginates large PR
  metadata sets, and changed-file detection now fails explicit bad refs instead
  of silently falling back.
- Discover repo-local optional analyzer tools from `.tools/` during `doctor`,
  `analyze`, and `preflight`, keep zero-PR GitHub metadata runs at local-only
  confidence, and make `pnpm review:queue:backlog` a read-only status command
  when run without action flags.
- Add `preflight --run-coverage` to run the configured `coverage.command`
  without shell expansion before importing changed-line coverage.
- Add pinned `pnpm tools:install:optional` and `pnpm tools:optional:check`
  flows for Semgrep, Gitleaks, OSV Scanner, and Trivy; `pnpm tools:check` now
  points missing analyzer tools at that bootstrap path.
- Add bounded Gitleaks worktree scanning over Git-visible files so uncommitted
  tracked and non-ignored untracked secrets can be reported without scanning
  ignored/generated directories.
- Harden Codex review launchd recovery with plist/executable validation,
  repo toolchain bootstrapping, richer diagnostics, and `review:status` health
  output for active jobs with no worker.
- Scope `review:status` process-fallback worker counts to the current repo so
  unrelated checkout workers cannot mask unhealthy active jobs.
- Scope required complete review evidence to pushed branch tips while still
  blocking already-known unresolved major or blocker findings on older outgoing
  commits.
- Add single-player preflight and coverage polish: `preflight --worktree` now
  checks staged, unstaged, and untracked local changes; `analyze` and
  `preflight` auto-import an existing configured coverage artifact; and public
  profile exports omit risky selected artifacts by default.
- Import bounded optional analyzer findings into `preflight` reports for
  changed files, with `--no-external-tools` available for deterministic fast
  preflight runs.
- Preserve unmatched local commit cards when GitHub PR metadata is imported,
  using merge commit SHAs to avoid duplicates and surfacing limitations for
  direct, squash, or rebase merge workflows.
- Prioritize behavior and risky files over docs-only noise in high-churn
  evidence while still retaining docs churn when it is significant.
- Add recent-vs-prior trend comparison to `analysis.json` and private
  `report.md`, covering test-evidence rate, large-change rate, fix/revert-like
  churn, risky untested changes, and high-churn concentration without turning
  the receipt into a score.
- Add the next single-player evidence pass: PR durability now ties imported
  GitHub PRs to later fix/revert-like same-file churn, Go coverage paths are
  normalized to repo-relative files with lowest-coverage file summaries,
  optional analyzers can run and import normalized findings, and generated
  config includes coverage/risky-path setup guidance.
- Exclude default `.contribution/reports` output artifacts from Git-visible
  repository inventory so repeated default analyses do not count prior reports
  as source files.
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
