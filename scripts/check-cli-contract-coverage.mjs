#!/usr/bin/env node

import { readFileSync } from "node:fs";
import { getChangedFiles } from "./lib/changed-files.mjs";
import {
  isCliContractCoveragePath,
  isCliContractPath,
} from "./lib/contract-sensitive-domains.mjs";

function normalizePath(value) {
  return String(value ?? "").replaceAll("\\", "/");
}

function parseArgs(argv) {
  const args = {
    base: "",
    files: [],
    filesFrom: "",
  };

  for (let i = 0; i < argv.length; i += 1) {
    const token = argv[i];
    if (token === "--base") {
      args.base = argv[i + 1] ?? "";
      i += 1;
      continue;
    }
    if (token === "--files") {
      args.files = argv.slice(i + 1);
      break;
    }
    if (token === "--files-from") {
      args.filesFrom = argv[i + 1] ?? "";
      i += 1;
      continue;
    }
    if (token === "-h" || token === "--help") {
      printHelp();
      process.exit(0);
    }
    throw new Error(`Unknown argument: ${token}`);
  }

  return args;
}

function printHelp() {
  console.log(`Usage: node scripts/check-cli-contract-coverage.mjs [options]

Options:
  --base <sha/ref>       Diff base when discovering changed files
  --files <paths...>     Explicit changed file list
  --files-from <path>    Newline-delimited explicit changed file list
  -h, --help             Show help
`);
}

function readFilesFromList(path) {
  return readFileSync(path, "utf8")
    .split(/\r?\n/u)
    .map((value) => value.trim())
    .filter(Boolean);
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  if (args.files.length > 0 && args.filesFrom) {
    throw new Error("Use only one of --files or --files-from.");
  }
  const files =
    args.files.length > 0
      ? args.files
      : args.filesFrom
        ? readFilesFromList(args.filesFrom)
        : getChangedFiles({ explicitBase: args.base }).files;
  const normalized = files.map(normalizePath).filter(Boolean);
  const cliFacing = normalized.filter(isCliContractPath);

  if (cliFacing.length === 0) {
    console.log("CLI contract coverage check passed: no CLI-facing changes.");
    return;
  }

  const coverage = normalized.filter(isCliContractCoveragePath);
  if (coverage.length > 0) {
    console.log(
      `CLI contract coverage check passed: ${coverage.length} coverage artifact(s) changed.`,
    );
    return;
  }

  console.error("CLI-facing changes require matching contract coverage.");
  console.error("Changed CLI-facing files:");
  for (const file of cliFacing) {
    console.error(`- ${file}`);
  }
  console.error("Update at least one of:");
  console.error("- internal/**/*_test.go");
  console.error("- scripts/dogfood-cli.mjs");
  console.error("- docs/cli-contract.md");
  console.error("- docs/tooling-validation.md");
  console.error("- .github/workflows/ci.yml or .github/workflows/release.yml");
  process.exit(1);
}

try {
  main();
} catch (error) {
  console.error(`[check-cli-contract-coverage] ${error.message}`);
  process.exit(1);
}
