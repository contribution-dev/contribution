# Scripts agent guide

## Scope

Applies to `scripts/**`. Use with [AGENTS.md](../AGENTS.md).

## Script rules

- Prefer deterministic behavior and stable output formats.
- Do not execute dynamic shell commands built from untrusted input.
- Fix real security issues; only suppress false positives with explicit local
  rationale.
- Never log secrets, tokens, credentials, or PII.
- Keep review and validation routing centralized in shared scripts instead of
  adding ad hoc checks in multiple places.
- Use `scripts/lib/temp-cleanup.mjs` for repo-owned temp cleanup. New
  disposable top-level temp paths should use the `contribution-*` prefix under
  `/tmp` or `$TMPDIR`; preserve durable review roots such as
  `contribution-code-reviews-*`.
- When script behavior changes, update validating docs or runbooks in the same
  change.

## References

- Validation workflow: [docs/tooling-validation.md](../docs/tooling-validation.md)
