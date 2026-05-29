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

  // nosemgrep: javascript.lang.security.audit.detect-non-literal-regexp.detect-non-literal-regexp -- policy globs are normalized and regex metacharacters are escaped above.
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

export async function readJson(filePath) {
  const raw = await readFile(filePath, "utf8");
  return JSON.parse(raw);
}

export async function loadRiskContract(contractPath = DEFAULT_CONTRACT_PATH) {
  const contract = await readJson(contractPath);
  return contract;
}
