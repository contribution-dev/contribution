#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import {
  buildControlPlaneDocSyncErrorMessage,
  hasControlPlaneChanges,
  isControlPlaneDocUpdated,
} from "./lib/control-plane-doc-sync.mjs";
import { gitChangedFilesArgs } from "./lib/git-diff-args.mjs";

function parseArgs(argv) {
  const args = {
    staged: false,
    stdinFiles: false,
    diffRange: "",
  };

  for (let i = 0; i < argv.length; i += 1) {
    const token = argv[i];
    if (token === "--staged") {
      args.staged = true;
      continue;
    }
    if (token === "--stdin-files") {
      args.stdinFiles = true;
      continue;
    }
    if (token === "--diff-range") {
      args.diffRange = argv[i + 1] ?? "";
      i += 1;
      continue;
    }
    if (token === "-h" || token === "--help") {
      console.log(`Usage: node scripts/check-control-plane-doc-sync.mjs [options]

Options:
  --stdin-files         Read newline-delimited file paths from stdin
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

function readFilesFromGit(args) {
  const output = execFileSync("git", gitChangedFilesArgs(args), {
    encoding: "utf8",
  });
  return output
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
}

function readFilesFromStdin() {
  if (process.stdin.isTTY) {
    return [];
  }
  const raw = readFileSync(0, "utf8");
  return raw
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  const changedFiles = args.stdinFiles
    ? readFilesFromStdin()
    : readFilesFromGit(args);
  if (!hasControlPlaneChanges(changedFiles)) {
    return;
  }

  if (!isControlPlaneDocUpdated(changedFiles)) {
    throw new Error(
      buildControlPlaneDocSyncErrorMessage({
        changedFiles,
        diffRange: args.diffRange || undefined,
      }),
    );
  }
}

if (
  import.meta.url === pathToFileURL(path.resolve(process.argv[1] ?? "")).href
) {
  try {
    main();
  } catch (error) {
    console.error(`[check-control-plane-doc-sync] ${error.message}`);
    process.exit(1);
  }
}
