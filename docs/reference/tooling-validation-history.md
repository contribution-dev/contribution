# Tooling Validation History

This file is reserved for dated workflow notes when validation or review
control-plane behavior changes. Current operator guidance lives in
[docs/tooling-validation.md](../tooling-validation.md).

- 2026-05-29: Moved CLI repository automation from Node.js 22 / pnpm 10 to
  Node.js 24 LTS / pnpm 11.4.0 and made `pnpm tools:preflight` enforce the
  required Node, pnpm, and Go versions.
