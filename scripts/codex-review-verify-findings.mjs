#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { isAuthoritativeContractEvidencePath } from "./lib/contract-sensitive-domains.mjs";

function parseArgs(argv) {
  let jsonPath = "";
  for (let i = 2; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--json") {
      jsonPath = String(argv[i + 1] ?? "");
      i += 1;
      continue;
    }
    if (arg === "-h" || arg === "--help") {
      process.stdout.write(
        "Usage: node scripts/codex-review-verify-findings.mjs --json <review-json-path>\n",
      );
      process.exit(0);
    }
    throw new Error(`Unknown argument: ${arg}`);
  }
  if (!jsonPath) {
    throw new Error("Missing required --json argument");
  }
  return { jsonPath };
}

function isTestPath(filePath) {
  return (
    /\.test\.(mjs|cjs|js|mts|cts|ts|tsx)$/.test(filePath) ||
    /_test\.go$/.test(filePath)
  );
}

function looksLikeTestFailureClaim(finding) {
  const haystack = [
    finding.title,
    finding.hypothesis,
    finding.impact,
    finding.recommended_direction,
    ...(Array.isArray(finding.evidence)
      ? finding.evidence.map((item) => String(item?.reason ?? ""))
      : []),
  ]
    .join(" ")
    .toLowerCase();

  return /\btest(s)?\b[\s\S]{0,48}\b(fail|fails|failed|failing)\b/.test(
    haystack,
  );
}

function toPackageTestInvocation(filePath) {
  const normalized = filePath.replace(/\\/g, "/");
  if (!isTestPath(normalized)) return null;

  if (normalized.endsWith("_test.go")) {
    return {
      cmd: "go",
      args: ["test", "./..."],
    };
  }
  if (normalized.startsWith("scripts/") && normalized.endsWith(".test.mjs")) {
    return {
      cmd: "node",
      args: ["--test", normalized],
    };
  }

  return null;
}

function runTestCheck(invocation, cwd, spawn = spawnSync) {
  const result = spawn(invocation.cmd, invocation.args, {
    cwd,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  });
  const output = `${result.stdout ?? ""}\n${result.stderr ?? ""}`.toLowerCase();
  const noTestsDetected =
    output.includes("no tests found") ||
    output.includes("no test files found") ||
    output.includes("no matching tests") ||
    output.includes("# tests 0") ||
    /test files\s+0\b/.test(output) ||
    /\b0 passing\b/.test(output);
  const executedTests =
    /# tests\s+[1-9]\d*\b/.test(output) ||
    /\b[1-9]\d*\s+tests?\b/.test(output) ||
    /test files\s+[1-9]\d*\b/.test(output) ||
    /\b[1-9]\d*\s+passing\b/.test(output) ||
    /^ok\s+/m.test(output);
  const passed = result.status === 0 && executedTests && !noTestsDetected;
  return { passed, executedTests };
}

function normalizeCheckResult(value) {
  if (typeof value === "boolean") {
    return { passed: value, executedTests: value };
  }
  if (!value || typeof value !== "object") {
    return { passed: false, executedTests: false };
  }

  return {
    passed: Boolean(value.passed),
    executedTests: Boolean(value.executedTests),
  };
}

function hasPathEvidence(files, pattern) {
  return files.some((file) => pattern.test(file));
}

export function hasChangelogOnlyContractEvidence(finding) {
  const evidence = Array.isArray(finding?.evidence) ? finding.evidence : [];
  const files = evidence
    .map((entry) => String(entry?.file ?? "").trim())
    .filter(Boolean);

  if (!hasPathEvidence(files, /(^|\/)CHANGELOG\.md$/)) {
    return false;
  }

  return (
    files.length > 0 &&
    files.every((file) => /(^|\/)CHANGELOG\.md$/.test(file)) &&
    !files.some((file) => isAuthoritativeContractEvidencePath(file)) &&
    !files.some((file) => isTestPath(file))
  );
}

function verifyFindings(data, cwd, runCheck = runTestCheck) {
  if (!Array.isArray(data.findings)) return { changed: false };

  const cache = new Map();
  let changed = false;

  for (const finding of data.findings) {
    if (!finding || typeof finding !== "object") continue;

    if (
      hasChangelogOnlyContractEvidence(finding) &&
      (finding.severity === "blocker" || finding.severity === "major")
    ) {
      finding.severity = "minor";
      const note =
        "Auto-verified: CHANGELOG.md is not authoritative contract evidence without AGENTS.md, docs/reference, or stable test support, so severity was downgraded.";
      const direction = String(finding.recommended_direction ?? "");
      finding.recommended_direction = direction.includes(note)
        ? direction
        : `${direction}${direction ? " " : ""}${note}`;
      changed = true;
    }

    if (!looksLikeTestFailureClaim(finding)) continue;

    const evidence = Array.isArray(finding.evidence) ? finding.evidence : [];
    const invocations = [];
    for (const entry of evidence) {
      const evidenceFile = String(entry?.file ?? "").trim();
      if (!evidenceFile) continue;
      const invocation = toPackageTestInvocation(evidenceFile);
      if (!invocation) continue;
      const cacheKey = `${invocation.cmd} ${invocation.args.join(" ")}`;
      invocations.push({ cacheKey, invocation });
    }

    if (invocations.length === 0) continue;

    let allPass = true;
    for (const { cacheKey, invocation } of invocations) {
      if (!cache.has(cacheKey)) {
        cache.set(cacheKey, normalizeCheckResult(runCheck(invocation, cwd)));
      }
      const check = cache.get(cacheKey);
      if (!check?.passed || !check.executedTests) {
        allPass = false;
        break;
      }
    }

    if (!allPass) continue;
    if (finding.severity !== "blocker" && finding.severity !== "major")
      continue;

    finding.severity = "minor";
    const note =
      "Auto-verified: referenced test currently passes, so severity was downgraded.";
    const direction = String(finding.recommended_direction ?? "");
    finding.recommended_direction = direction.includes(note)
      ? direction
      : `${direction}${direction ? " " : ""}${note}`;
    changed = true;
  }

  return { changed };
}

function main() {
  const { jsonPath } = parseArgs(process.argv);
  const absoluteJsonPath = path.resolve(jsonPath);
  const cwd = process.cwd();

  const data = JSON.parse(readFileSync(absoluteJsonPath, "utf8"));
  const result = verifyFindings(data, cwd);
  if (result.changed) {
    writeFileSync(
      absoluteJsonPath,
      `${JSON.stringify(data, null, 2)}\n`,
      "utf8",
    );
  }
}

if (
  process.argv[1] !== undefined &&
  import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href
) {
  try {
    main();
  } catch (error) {
    console.error(
      "[codex-review-verify-findings]",
      error instanceof Error ? error.message : String(error),
    );
    process.exit(1);
  }
}

export {
  looksLikeTestFailureClaim,
  runTestCheck,
  toPackageTestInvocation,
  verifyFindings,
};
