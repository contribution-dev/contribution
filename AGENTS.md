# Contribution agent guide

## Scope

This file governs the whole repository. Nested `AGENTS.md` files add stricter
path-local instructions.

## Precedence

1. System, developer, and user instructions
2. Nearest applicable `AGENTS.md`
3. Referenced repo docs for implementation details

If two instructions conflict and both cannot be satisfied, stop and ask.

## Repo-wide rules

- This is a Go CLI. Product code should live under `cmd/` and `internal/`
  unless there is a clear reason to publish a package API.
- Keep CLI commands deterministic, testable, and explicit about stdout,
  stderr, and exit behavior.
- Prefer the simplest design that preserves correctness and documented
  contracts.
- Prefer editing or deleting existing code over adding parallel paths.
- Do not log secrets, raw tokens, credentials, or PII.
- Before changing shared CLI behavior, inspect unchanged callers, tests, and
  docs so compatibility changes are intentional.
- Remove superseded code in the same change unless a documented rollout window
  requires both paths.

## Git and worktree hygiene

- Assume unrelated local changes may exist. Do not revert or restage them.
- Stage files explicitly by path.
- Never use broad staging unless the user explicitly asks for it.
- Autonomous agents with auto-commit enabled should work directly on the
  current working branch.
- If you modify files for the task, create a commit before finishing unless the
  user explicitly says not to commit.
- Commit only files directly changed for the task.
- Never include unrelated local changes in your commit, even if they are already
  staged.
- After creating a task commit, verify with `git status --short` that no
  task-related files remain staged or unstaged.
- Do not push unless the user explicitly asks.

## Validation and completion

- For code changes, run the smallest relevant validation before finishing.
  Default to `pnpm checks:changed`.
- For docs or policy changes, run `pnpm agents:check`.
- For release-sensitive changes, run `pnpm validate:final`.
- For user-facing behavior changes, update `CHANGELOG.md`.
- Do not mark work complete while temporary files or stale debug artifacts
  remain in the repo.

## Execution expectations

- For non-trivial tasks, make the plan explicit before implementation.
- If new information invalidates the plan, stop and re-plan before continuing.
- Prefer root-cause fixes over temporary patches unless a temporary fix is
  explicitly requested.
- For behavior changes and bug fixes, add or update the narrowest regression
  test that would fail before the change unless no practical harness exists.

## Sub-agent coordination

- Use sub-agents when bounded parallel work makes the task safer or faster,
  especially read-only investigation, code review, test or log analysis, Go
  package-specific discovery, or drafting isolated docs.
- Do not assign multiple agents to edit overlapping files, shared packages,
  public interfaces, command behavior, or generated/vendor surfaces unless the
  work is explicitly partitioned.
- For write tasks, give each sub-agent exclusive paths or packages, or have
  sub-agents return proposed patches or drafts for the lead agent to apply.
- The lead agent owns final integration, consistency with repo patterns,
  `gofmt`, `go test` or required repo validation, explicit staging, and
  commits.
- Sub-agents must not push, run destructive commands, or include unrelated local
  changes.

## Working norms

- Keep progress updates and final responses concise and high signal.
- Prefer repository instructions and durable docs over per-task scratch files.
- Capture durable guidance in `AGENTS.md` or `docs/`; do not create status or
  phase-log markdown files for completed work.

## Docs and temporary files

- Keep active instructions in `AGENTS.md`, not tool-specific rule files.
- Keep reference material, specs, runbooks, and roadmap content in `docs/`.
- Local checkouts may have private shared product docs at `docs-shared/`.
  This path is intentionally gitignored and may be a symlink to the private
  website repo. If it exists, read `docs-shared/AGENTS.md` and the relevant
  shared docs before changing product behavior. Use commands that follow or
  explicitly traverse the symlink, such as `ls docs-shared/`,
  `find -L docs-shared -maxdepth 1 -type f`, or `rg --files docs-shared`.
  Do not stage or commit `docs-shared` or its contents from this repo.
  If a task intentionally changes shared docs, edit and commit those files in
  their owning source repo, such as `/Users/gabe/Sites/contribution-website`,
  and keep that commit separate from CLI repo changes.
- Temporary repo files must use a `temp-` prefix. Prefer `/tmp/contribution-*`
  for disposable harnesses and scratch runs.

## Routing

- CLI implementation: [cmd/contribution](cmd/contribution)
- CLI internals: [internal/cli](internal/cli)
- Scripts: [scripts/AGENTS.md](scripts/AGENTS.md)
- Docs: [docs/AGENTS.md](docs/AGENTS.md)

## Reference docs

- Agent-system guidance: [docs/agent-system.md](docs/agent-system.md)
- Architecture reference: [docs/reference/architecture.md](docs/reference/architecture.md)
- Tooling validation: [docs/tooling-validation.md](docs/tooling-validation.md)
