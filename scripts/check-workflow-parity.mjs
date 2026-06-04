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

for (const workflow of [
  ".github/workflows/review-agent-rerun.yml",
  ".github/workflows/review-auto-resolve-threads.yml",
  ".github/workflows/review-remediation.yml",
]) {
  if (!existsSync(workflow)) {
    fail(`Missing review workflow: ${workflow}`);
    continue;
  }
  const content = readFileSync(workflow, "utf8");
  if (
    !/workflow_run:/u.test(content) ||
    !/pull-requests:\s*write/u.test(content)
  ) {
    continue;
  }
  if (
    !/github\.event\.workflow_run\.head_repository\.full_name\s*==\s*github\.event\.repository\.full_name/u.test(
      content,
    )
  ) {
    fail(`${workflow} is missing a same-repository workflow_run guard.`);
  }
}

if (failed) {
  process.exit(1);
}

console.log("Workflow parity check passed.");
