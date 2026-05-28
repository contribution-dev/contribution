#!/usr/bin/env node

import { existsSync, readFileSync, readdirSync } from "node:fs";
import path from "node:path";

const workflowDir = ".github/workflows";
let failed = false;

function fail(message) {
  console.error(message);
  failed = true;
}

if (!existsSync(workflowDir)) {
  fail("Missing .github/workflows directory.");
} else {
  for (const entry of readdirSync(workflowDir).sort()) {
    if (!/\.ya?ml$/i.test(entry)) {
      continue;
    }
    const filePath = path.join(workflowDir, entry);
    const content = readFileSync(filePath, "utf8");
    if (
      /actions\/upload-artifact@/u.test(content) &&
      !/retention-days:/u.test(content)
    ) {
      fail(`${filePath} uploads artifacts without retention-days.`);
    }
  }
}

for (const script of [
  "scripts/risk-policy/review-rerun.mjs",
  "scripts/risk-policy/review-remediation-dispatch.mjs",
  "scripts/risk-policy/auto-resolve-bot-threads.mjs",
]) {
  if (!existsSync(script)) {
    fail(`Missing review workflow script: ${script}`);
  }
}

if (failed) {
  process.exit(1);
}

console.log("Workflow parity check passed.");
