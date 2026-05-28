#!/usr/bin/env node

import { existsSync, readFileSync } from "node:fs";

const REQUIRED_FILES = [
  "AGENTS.md",
  "CLAUDE.md",
  "docs/AGENTS.md",
  "docs/agent-system.md",
  "docs/tooling-validation.md",
  "docs/reference/architecture.md",
  "scripts/AGENTS.md",
];

let failed = false;

for (const file of REQUIRED_FILES) {
  if (!existsSync(file)) {
    console.error(`Missing required agent/doc file: ${file}`);
    failed = true;
  }
}

if (existsSync("CLAUDE.md")) {
  const claude = readFileSync("CLAUDE.md", "utf8");
  if (!claude.includes("AGENTS.md")) {
    console.error(
      "CLAUDE.md must remain compatibility-only and point to AGENTS.md.",
    );
    failed = true;
  }
}

if (existsSync("AGENTS.md")) {
  const root = readFileSync("AGENTS.md", "utf8");
  for (const heading of [
    "## Repo-wide rules",
    "## Git and worktree hygiene",
    "## Validation and completion",
    "## Routing",
  ]) {
    if (!root.includes(heading)) {
      console.error(`AGENTS.md missing required section: ${heading}`);
      failed = true;
    }
  }
}

if (failed) {
  process.exit(1);
}

console.log("AGENTS/docs drift check passed.");
