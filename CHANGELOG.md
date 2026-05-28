# Changelog

All notable changes to this project will be documented in this file.

## Unreleased

- Add repo-owned CLI dogfood, contract coverage routing, and release artifact
  smoke validation.
- Make final validation headless-safe and include Node script regression tests
  in final and CI gates.
- Rename the local CI-style package script to `ci:local` and route CLI contract
  coverage through file-list input to keep changed-aware logs readable.
- Fix `analyze --format json` completion output so it does not point at a
  markdown report that was not written.
- Add the V1 local contribution intelligence CLI foundation with private
  analysis reports, public-safe profile exports, preflight packets, and
  environment diagnostics.
- Bootstrap the Go CLI repository with CI, release, validation, and commit
  review automation.
