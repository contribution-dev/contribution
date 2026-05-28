#!/usr/bin/env node

import { getChangedFiles } from "./lib/changed-files.mjs";

function normalizePath(value) {
  return String(value ?? "").replaceAll("\\", "/");
}

function hasAnyPrefix(file, prefixes) {
  return prefixes.some((prefix) => file.startsWith(prefix));
}

function parseArgs(argv) {
  const args = {
    base: "",
    files: [],
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
  -h, --help             Show help
`);
}

function isCliFacingPath(file) {
  if (
    hasAnyPrefix(file, [
      "cmd/contribution/",
      "internal/cli/",
      "internal/report/",
      "internal/git/",
      "internal/config/",
    ])
  ) {
    return true;
  }
  return [
    ".goreleaser.yml",
    "README.md",
    "docs/prd-cli.md",
    "docs/product-architecture.md",
    "docs/cli-contract.md",
  ].includes(file);
}

function isCoveragePath(file) {
  if (/^internal\/.*_test\.go$/u.test(file)) {
    return true;
  }
  return [
    "scripts/dogfood-cli.mjs",
    "scripts/check-cli-contract-coverage.mjs",
    "docs/cli-contract.md",
    ".github/workflows/ci.yml",
    ".github/workflows/release.yml",
    "docs/tooling-validation.md",
  ].includes(file);
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  const files =
    args.files.length > 0
      ? args.files
      : getChangedFiles({ explicitBase: args.base }).files;
  const normalized = files.map(normalizePath).filter(Boolean);
  const cliFacing = normalized.filter(isCliFacingPath);

  if (cliFacing.length === 0) {
    console.log("CLI contract coverage check passed: no CLI-facing changes.");
    return;
  }

  const coverage = normalized.filter(isCoveragePath);
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
