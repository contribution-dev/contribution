#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { classifyChangedFiles, getChangedFiles } from "./lib/changed-files.mjs";
import {
  getContractCoverageValidationCommands,
  getContractFastValidationCommands,
} from "./lib/contract-sensitive-domains.mjs";
import {
  buildControlPlaneDocSyncErrorMessage,
  hasControlPlaneChanges,
  isControlPlaneDocUpdated,
} from "./lib/control-plane-doc-sync.mjs";
import { cleanupStaleProjectTempRoots } from "./lib/temp-cleanup.mjs";

const MODES = new Set(["lint", "typecheck", "test", "all"]);

function parseArgs(argv) {
  const args = {
    mode: "all",
    full: false,
    base: "",
    prePush: false,
  };

  for (let i = 0; i < argv.length; i += 1) {
    const token = argv[i];
    if (token === "--mode") {
      args.mode = argv[i + 1] ?? "";
      i += 1;
      continue;
    }
    if (token === "--full") {
      args.full = true;
      continue;
    }
    if (token === "--base") {
      args.base = argv[i + 1] ?? "";
      i += 1;
      continue;
    }
    if (token === "--pre-push") {
      args.prePush = true;
      continue;
    }
    if (token === "-h" || token === "--help") {
      printHelp();
      process.exit(0);
    }
    throw new Error(`Unknown argument: ${token}`);
  }

  if (!MODES.has(args.mode)) {
    throw new Error(`Invalid --mode value: ${args.mode}`);
  }

  return args;
}

function printHelp() {
  console.log(`Usage: node scripts/run-changed-checks.mjs [options]

Options:
  --mode <lint|typecheck|test|all>  Which checks to run (default: all)
  --base <sha/ref>                  Override diff base ref
  --full                            Force full checks for the selected mode
  --pre-push                        Apply pre-push routing
  -h, --help                        Show help
`);
}

function runCommand(command, args) {
  const printable = [command, ...args].join(" ");
  console.log(`[run-changed-checks] START command=${printable}`);
  console.log(`-> ${printable}`);
  const result = spawnSync(command, args, {
    stdio: "inherit",
    cwd: process.cwd(),
    env: process.env,
  });
  if (result.status !== 0) {
    throw new Error(`FAIL command=${printable} exit=${result.status ?? 1}`);
  }
  console.log(`[run-changed-checks] PASS command=${printable}`);
}

export function buildFullCommands(mode) {
  if (mode === "lint") {
    return [
      ["pnpm", ["lint:scripts"]],
      ["pnpm", ["format:check"]],
      ["pnpm", ["lint:go"]],
    ];
  }
  if (mode === "typecheck") {
    return [["go", ["vet", "./..."]]];
  }
  if (mode === "test") {
    return [
      ["pnpm", ["test:scripts"]],
      ["go", ["test", "./..."]],
    ];
  }
  return [
    ["pnpm", ["lint:scripts"]],
    ["pnpm", ["test:scripts"]],
    ["pnpm", ["format:check"]],
    ["go", ["vet", "./..."]],
    ["go", ["test", "./..."]],
    ["go", ["test", "-race", "./..."]],
    ["pnpm", ["build"]],
  ];
}

export function buildChangedCommands(mode, files, classification) {
  const commands = [];

  if (mode === "lint" || mode === "all") {
    if (classification.tooling) {
      commands.push(["pnpm", ["lint:scripts"]]);
    }
    if (classification.goRelevant || classification.rootConfig) {
      commands.push(["pnpm", ["format:check"]]);
      commands.push(["pnpm", ["lint:go"]]);
    }
  }

  if (mode === "typecheck" || mode === "all") {
    if (classification.goRelevant || classification.rootConfig) {
      commands.push(["go", ["vet", "./..."]]);
    }
  }

  if (mode === "test" || mode === "all") {
    if (classification.tooling) {
      commands.push(["pnpm", ["test:scripts"]]);
    }
    if (classification.goRelevant || classification.rootConfig) {
      commands.push(["go", ["test", "./..."]]);
    }
  }

  return dedupeCommands(commands);
}

function normalizePath(value) {
  return String(value ?? "").replaceAll("\\", "/");
}

function withChangedFilesList(files, callback) {
  const dir = mkdtempSync(join(tmpdir(), "contribution-changed-files-"));
  const file = join(dir, "files.txt");
  writeFileSync(file, `${files.map(normalizePath).join("\n")}\n`);
  try {
    return callback(file);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

function appendContractCommands(commands, mode, files, filesFrom) {
  if (mode !== "test" && mode !== "all") {
    return commands;
  }
  const contractCommands = getContractFastValidationCommands(files, {
    filesFrom,
  }).map((command) => [command.cmd, command.args]);
  return dedupeCommands([...commands, ...contractCommands]);
}

function runContractCoverageForDocsOnly(files, filesFrom) {
  const commands = getContractCoverageValidationCommands(files, {
    filesFrom,
  }).map((command) => [command.cmd, command.args]);
  runPlan(commands, "Running CLI contract coverage validation");
}

function dedupeCommands(commands) {
  const seen = new Set();
  const unique = [];
  for (const command of commands) {
    const key = [command[0], ...command[1]].join("\u0000");
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    unique.push(command);
  }
  return unique;
}

const AGENTS_CHECK_RELEVANT_PREFIXES = [
  "AGENTS.md",
  "CLAUDE.md",
  "docs/agent-system.md",
  "docs/tooling-validation.md",
  "docs/reference/",
  "docs/runbooks/",
  "docs/strategy/",
];

function hasAnyPrefix(file, prefixes) {
  return prefixes.some((prefix) => file.startsWith(prefix));
}

export function hasAgentsCheckRelevantChanges(files) {
  return files.some(
    (file) =>
      file === "AGENTS.md" ||
      file === "CLAUDE.md" ||
      file.endsWith("/AGENTS.md") ||
      hasAnyPrefix(file, AGENTS_CHECK_RELEVANT_PREFIXES),
  );
}

function runPlan(commands, label) {
  if (commands.length === 0) {
    console.log(`${label}: no matching checks required.`);
    return;
  }
  console.log(`${label}:`);
  for (const [command, args] of commands) {
    runCommand(command, args);
  }
}

function shouldUseDocsOnlyFastPath(classification, args) {
  return Boolean(classification.docsOnly && !args.full);
}

function assertPrePushBase(args) {
  if (args.prePush && !(args.base && args.base.trim().length > 0)) {
    throw new Error(
      "Pre-push routing requires --base with a fetched canonical base SHA.",
    );
  }
}

function runPrePushRoute(changes, classification, args, changedFilesList) {
  const shouldRunAgentsCheck = hasAgentsCheckRelevantChanges(changes.files);
  if (shouldUseDocsOnlyFastPath(classification, args)) {
    console.log("Pre-push mode: docs-only change set, skipping code checks.");
    runContractCoverageForDocsOnly(changes.files, changedFilesList);
    if (shouldRunAgentsCheck) {
      runPlan([["pnpm", ["agents:check"]]], "Running AGENTS/docs validation");
    }
    return;
  }

  const commands = appendContractCommands(
    args.full || classification.rootConfig || classification.tooling
      ? buildFullCommands("all")
      : buildChangedCommands("all", changes.files, classification),
    "all",
    changes.files,
    changedFilesList,
  );
  runPlan(commands, "Running changed-aware check set");

  if (shouldRunAgentsCheck) {
    runPlan([["pnpm", ["agents:check"]]], "Running AGENTS/docs validation");
  }

  if (hasControlPlaneChanges(changes.files)) {
    if (!isControlPlaneDocUpdated(changes.files)) {
      throw new Error(
        buildControlPlaneDocSyncErrorMessage({
          changedFiles: changes.files,
          diffRange: changes.diffRange,
        }),
      );
    }
  }
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  cleanupStaleProjectTempRoots({ quiet: true });
  try {
    assertPrePushBase(args);
    const changes = getChangedFiles({ explicitBase: args.base });
    const classification = classifyChangedFiles(changes.files);
    const shouldRunAgentsCheck = hasAgentsCheckRelevantChanges(changes.files);

    console.log(`Changed-checks diff range: ${changes.diffRange}`);

    if (changes.files.length === 0) {
      console.log("No changed files detected. Nothing to run.");
      return;
    }

    withChangedFilesList(changes.files, (changedFilesList) => {
      if (args.prePush) {
        runPrePushRoute(changes, classification, args, changedFilesList);
        return;
      }

      if (shouldUseDocsOnlyFastPath(classification, args)) {
        console.log("Docs-only change set detected.");
        runContractCoverageForDocsOnly(changes.files, changedFilesList);
        if (shouldRunAgentsCheck) {
          runPlan(
            [["pnpm", ["agents:check"]]],
            "Running AGENTS/docs validation",
          );
        }
        return;
      }

      const commands = appendContractCommands(
        args.full || classification.rootConfig || classification.tooling
          ? buildFullCommands(args.mode)
          : buildChangedCommands(args.mode, changes.files, classification),
        args.mode,
        changes.files,
        changedFilesList,
      );
      runPlan(commands, "Running changed-aware check set");

      if (shouldRunAgentsCheck) {
        runPlan([["pnpm", ["agents:check"]]], "Running AGENTS/docs validation");
      }
    });
  } finally {
    cleanupStaleProjectTempRoots({ quiet: true });
  }
}

export function isDirectExecution(importMetaUrl, argv1) {
  if (!argv1) {
    return false;
  }
  return importMetaUrl === pathToFileURL(resolve(argv1)).href;
}

if (isDirectExecution(import.meta.url, process.argv[1])) {
  try {
    main();
  } catch (error) {
    console.error(`[run-changed-checks] ${error.message}`);
    process.exit(1);
  }
}
