# Contributing

Thanks for contributing to `contribution`.

## Local Setup

```bash
pnpm install
scripts/with-tools go mod download
pnpm tools:preflight
```

## Workflow

- Keep changes small and focused.
- Run the narrowest relevant validation before committing.
- Use explicit staging paths; do not stage unrelated work.
- Do not push unless the branch has passed local hooks and commit review.

For code changes, the default validation command is:

```bash
pnpm checks:changed
```

For agent or workflow policy changes, run:

```bash
pnpm agents:check
```

## Pull Requests

Open a PR with a concise summary, test notes, and any intentional tradeoffs.
User-facing behavior changes should update `CHANGELOG.md`.
