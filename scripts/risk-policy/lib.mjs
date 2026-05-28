#!/usr/bin/env node

import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
export const REPO_ROOT = path.resolve(__dirname, "..", "..");
export const DEFAULT_CONTRACT_PATH = path.resolve(
  REPO_ROOT,
  ".github/policy/risk-merge-contract.v1.json",
);
export const DEFAULT_SCHEMA_PATH = path.resolve(
  REPO_ROOT,
  ".github/policy/risk-merge-contract.schema.json",
);

function escapeRegex(value) {
  return value.replace(/[|\\{}()[\]^$+?.]/g, "\\$&");
}

function globToRegex(pattern) {
  const normalized = pattern.replaceAll("\\", "/");
  let output = "";

  for (let i = 0; i < normalized.length; i += 1) {
    const char = normalized[i];
    const next = normalized[i + 1];

    if (char === "*" && next === "*") {
      output += ".*";
      i += 1;
      continue;
    }

    if (char === "*") {
      output += "[^/]*";
      continue;
    }

    output += escapeRegex(char);
  }

  return new RegExp(`^${output}$`);
}

export function normalizePath(value) {
  return value.replaceAll("\\", "/").replace(/^.\//, "");
}

export function pathMatchesGlob(value, pattern) {
  return globToRegex(pattern).test(normalizePath(value));
}

export function pathMatchesAnyGlob(value, patterns) {
  return patterns.some((pattern) => pathMatchesGlob(value, pattern));
}

export function uniqueValues(values) {
  return [...new Set(values)];
}

export async function readJson(filePath) {
  const raw = await readFile(filePath, "utf8");
  return JSON.parse(raw);
}

export async function loadRiskContract(contractPath = DEFAULT_CONTRACT_PATH) {
  const contract = await readJson(contractPath);
  return contract;
}

export function toRelativeFromRepo(filePath) {
  const relative = path.relative(REPO_ROOT, filePath);
  return normalizePath(relative || filePath);
}

export function requiredBrowserFlowIds(contract, changedFiles, riskTier) {
  const browserEvidence = contract.browserEvidence;
  if (!browserEvidence) return [];

  if (!browserEvidence.requiredByRiskTier.includes(riskTier)) {
    return [];
  }

  const ids = [];
  for (const trigger of browserEvidence.triggers) {
    const matched = changedFiles.some((changedFile) =>
      pathMatchesAnyGlob(changedFile, trigger.ifChangedAny),
    );
    if (matched) {
      ids.push(...trigger.requiredFlowIds);
    }
  }

  return uniqueValues(ids);
}
