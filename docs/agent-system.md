# AGENTS system guide

This repository uses `AGENTS.md` as the only active instruction format.

## Precedence

1. System, developer, and user instructions
2. Nearest applicable `AGENTS.md`
3. Referenced repo docs

If two instructions conflict and both cannot be satisfied, stop and ask.

## What belongs in `AGENTS.md`

Keep `AGENTS.md` short, behavioral, and enforceable:

- scope and precedence
- hard constraints
- validation and completion requirements
- path-local rules
- links to durable docs

Do not use `AGENTS.md` for roadmap narrative, long architecture maps, or large
implementation examples.

## What belongs in docs

- `docs/reference/` for architecture, technical contracts, and conventions
- `docs/runbooks/` for operational workflows
- `docs/strategy/` for vision, roadmap support, and deferred work

## Validation

Run `pnpm agents:check` for changes to `AGENTS.md`, `CLAUDE.md`, workflow
policy, or policy docs.
