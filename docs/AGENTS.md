# Docs agent guide

## Scope

Applies to `docs/**`. Use with [AGENTS.md](../AGENTS.md).

## Docs rules

- Keep durable documentation in `docs/`; keep active instructions in
  `AGENTS.md`.
- Keep active workflow docs short and current-state.
- Use `docs/reference/` for architecture, specs, and conventions.
- Use `docs/runbooks/` for operational workflows.
- Use `docs/strategy/` for vision, roadmap support, and deferred work.
- Do not create recap, status, or phase-log documents.
- When renaming or moving docs, update inbound references in `AGENTS.md`,
  nearby docs, and validation tooling in the same change.

## References

- AGENTS authoring and precedence: [docs/agent-system.md](agent-system.md)
- Architecture: [docs/reference/architecture.md](reference/architecture.md)
- Validation workflow: [docs/tooling-validation.md](tooling-validation.md)
