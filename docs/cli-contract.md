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
  tokens, private repo roots, and private remotes.
- Missing optional tools or GitHub metadata should degrade reports, not fail
  local analysis.

## Commands

| Command     | Contract                                                                                                                                        | Required coverage                                       |
| ----------- | ----------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------- |
| root/help   | No args exits 0 and prints command help to stdout.                                                                                              | Go command test and dogfood smoke.                      |
| `version`   | Exits 0 and prints version, commit, and date fields. Release artifacts must use linker-provided values.                                         | Go command test, dogfood smoke, release artifact smoke. |
| `init`      | Creates `.contribution.yml` in the current Git repo, uses a detected default branch when available, and is idempotent.                          | Dogfood smoke.                                          |
| `doctor`    | Exits 0 in a Git repo, reports required/optional tool status, and never prints token values. Missing optional tools are non-fatal.              | Dogfood smoke and release artifact smoke.               |
| `analyze`   | Analyzes `--repo` or the current repo, respects `--format`, writes expected artifacts, and continues without optional tools or GitHub metadata. | Dogfood smoke and release artifact smoke.               |
| `report`    | Requires `--input`, validates `--format`, regenerates report/profile/share artifacts, and honors `--public-safe`.                               | Go command test and dogfood smoke.                      |
| `preflight` | Validates `--format`, compares `--base` and `--head`, writes preflight artifacts, and classifies source/test/risky/dependency evidence.         | Go command test and dogfood smoke.                      |
| `packet`    | Requires `--pr`, reads the latest analysis under `--output`, writes friend-review packet artifacts, and redacts by default.                     | Go command test and dogfood smoke.                      |

## Updating the contract

When changing command names, flags, stdout/stderr text, exit behavior, generated
artifacts, report schemas, privacy posture, or release packaging, update at
least one matching coverage artifact in the same change:

- Go tests under `internal/**`
- `scripts/dogfood-cli.mjs`
- this contract document
- workflow or validation docs that route the new behavior
