#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import {
  CONTROL_PLANE_HISTORY_DOC_PATH,
  listControlPlaneFiles,
} from "./lib/control-plane-doc-sync.mjs";
import { gitChangedFilesArgs } from "./lib/git-diff-args.mjs";

const CHANGELOG_HEADER = "## Control-plane changelog";

function parseArgs(argv) {
  const args = {
    staged: false,
    diffRange: "",
  };

  for (let i = 0; i < argv.length; i += 1) {
    const token = argv[i];
    if (token === "--staged") {
      args.staged = true;
      continue;
    }
    if (token === "--diff-range") {
      args.diffRange = argv[i + 1] ?? "";
      i += 1;
      continue;
    }
    if (token === "-h" || token === "--help") {
      console.log(`Usage: node scripts/tooling-control-plane-note.mjs [options]

Options:
  --staged              Read staged files from git diff --cached
  --diff-range <range>  Override range for git diff (e.g., origin/main...HEAD)
  -h, --help            Show help
`);
      process.exit(0);
    }
    throw new Error(`Unknown argument: ${token}`);
  }

  return args;
}

function readChangedFiles(args) {
  const output = execFileSync("git", gitChangedFilesArgs(args), {
    encoding: "utf8",
  });
  return output
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
}

function currentDateStamp() {
  return new Date().toISOString().slice(0, 10);
}

function ensureTrailingNewline(value) {
  return value.endsWith("\n") ? value : `${value}\n`;
}

function buildEntry(files) {
  const lines = [
    `- ${currentDateStamp()}: TODO summarize control-plane change rationale.`,
    "  - files:",
    ...files.map((file) => `    - ${file}`),
  ];
  return `${lines.join("\n")}\n`;
}

function appendControlPlaneNote(docContent, entry) {
  const normalized = ensureTrailingNewline(docContent);
  if (!normalized.includes(CHANGELOG_HEADER)) {
    return `${normalized}\n${CHANGELOG_HEADER}\n\n${entry}`;
  }

  const headerIndex = normalized.indexOf(CHANGELOG_HEADER);
  const before = normalized.slice(0, headerIndex + CHANGELOG_HEADER.length);
  const after = normalized.slice(headerIndex + CHANGELOG_HEADER.length);
  const separator = after.startsWith("\n\n") ? "" : "\n";
  return `${before}${separator}\n${entry}${after}`;
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  const changedFiles = readChangedFiles(args);
  const controlPlaneFiles = listControlPlaneFiles(changedFiles);

  if (controlPlaneFiles.length === 0) {
    console.log(
      "[tooling-control-plane-note] No control-plane file changes detected.",
    );
    return;
  }

  if (!existsSync(CONTROL_PLANE_HISTORY_DOC_PATH)) {
    throw new Error(`Missing required doc: ${CONTROL_PLANE_HISTORY_DOC_PATH}`);
  }

  const original = readFileSync(CONTROL_PLANE_HISTORY_DOC_PATH, "utf8");
  const updated = appendControlPlaneNote(
    original,
    buildEntry(controlPlaneFiles),
  );
  writeFileSync(CONTROL_PLANE_HISTORY_DOC_PATH, updated, "utf8");

  console.log(
    `[tooling-control-plane-note] Appended note template to ${CONTROL_PLANE_HISTORY_DOC_PATH}`,
  );
  console.log(
    "[tooling-control-plane-note] Edit the TODO summary before committing, and update docs/tooling-validation.md separately when current workflow behavior changed.",
  );
}

if (
  import.meta.url === pathToFileURL(path.resolve(process.argv[1] ?? "")).href
) {
  try {
    main();
  } catch (error) {
    console.error(`[tooling-control-plane-note] ${error.message}`);
    process.exit(1);
  }
}
