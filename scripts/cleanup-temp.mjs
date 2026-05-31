#!/usr/bin/env node

import { pathToFileURL } from "node:url";
import {
  cleanupProjectTempRoots,
  DEFAULT_STALE_TEMP_HOURS,
} from "./lib/temp-cleanup.mjs";

function parseArgs(argv) {
  const args = {
    olderThanHours: DEFAULT_STALE_TEMP_HOURS,
    dryRun: false,
    quiet: false,
  };

  for (let i = 0; i < argv.length; i += 1) {
    const token = argv[i];
    if (token === "--older-than-hours") {
      args.olderThanHours = Number(argv[i + 1] ?? "");
      i += 1;
      continue;
    }
    if (token === "--all") {
      args.olderThanHours = 0;
      continue;
    }
    if (token === "--dry-run") {
      args.dryRun = true;
      continue;
    }
    if (token === "--quiet") {
      args.quiet = true;
      continue;
    }
    if (token === "-h" || token === "--help") {
      printHelp();
      process.exit(0);
    }
    throw new Error(`Unknown argument: ${token}`);
  }

  if (!Number.isFinite(args.olderThanHours) || args.olderThanHours < 0) {
    throw new Error("--older-than-hours must be a non-negative number");
  }

  return args;
}

function printHelp() {
  console.log(`Usage: node scripts/cleanup-temp.mjs [options]

Options:
  --older-than-hours <n>  Remove project temp paths older than n hours (default: 24)
  --all                   Remove all current project temp paths
  --dry-run               Print what would be removed without deleting
  --quiet                 Suppress summary output
  -h, --help              Show help
`);
}

function printResult(result, dryRun) {
  const verb = dryRun ? "would_remove" : "removed";
  console.log(
    `[temp-cleanup] ${verb}=${result.removed.length} fresh=${result.skippedFresh.length} preserved=${result.skippedPreserved.length}`,
  );
  for (const target of result.removed) {
    console.log(`${verb} ${target}`);
  }
  for (const warning of result.warnings) {
    console.warn(`[temp-cleanup] warning ${warning}`);
  }
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  const result = cleanupProjectTempRoots(args);
  if (!args.quiet) {
    printResult(result, args.dryRun);
  }
  if (result.warnings.length > 0) {
    process.exitCode = 1;
  }
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  try {
    main();
  } catch (error) {
    console.error(`[temp-cleanup] ${error.message}`);
    process.exit(1);
  }
}
