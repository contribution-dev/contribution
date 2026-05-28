#!/usr/bin/env node

import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import {
  resolvePrePushBranchState,
  summarizeBranchState,
} from "./lib/pre-push-branch-state.mjs";

function usage() {
  console.log(`Usage: node scripts/pre-push-branch-state.mjs --stdin-file <path> --remote-name <name> [options]

Options:
  --stdin-file <path>             Path to pre-push stdin capture file
  --remote-name <name>            Remote name passed by the pre-push hook
  --json                          Print JSON output to stdout
  --write-json <path>             Persist JSON output for hook consumers
  --fetch-remote <0|1>            Refresh remote branch refs before checks (default: 1)
  --fetch-timeout-seconds <n>     Timeout for fetch/ls-remote calls (default: 20)
  --block-on-remote-divergence <0|1>
                                  Fail when remote moved and local branch does not contain it (default: 1)
`);
}

function parseArgs(argv) {
  const args = {
    stdinFile: "",
    remoteName: "",
    json: false,
    writeJson: "",
    fetchRemote:
      (process.env.CODEX_PRE_PUSH_FETCH_REMOTE ?? "1").trim() !== "0",
    fetchTimeoutSeconds: Number.parseInt(
      process.env.CODEX_PRE_PUSH_FETCH_TIMEOUT_SECONDS ?? "20",
      10,
    ),
    blockOnRemoteDivergence:
      (process.env.CODEX_PRE_PUSH_BLOCK_ON_REMOTE_DIVERGENCE ?? "1").trim() !==
      "0",
  };

  for (let i = 2; i < argv.length; i += 1) {
    const arg = argv[i];
    const next = argv[i + 1];
    switch (arg) {
      case "--stdin-file":
        args.stdinFile = String(next ?? "");
        i += 1;
        break;
      case "--remote-name":
        args.remoteName = String(next ?? "");
        i += 1;
        break;
      case "--json":
        args.json = true;
        break;
      case "--write-json":
        args.writeJson = String(next ?? "");
        i += 1;
        break;
      case "--fetch-remote":
        args.fetchRemote = String(next ?? "1") !== "0";
        i += 1;
        break;
      case "--fetch-timeout-seconds":
        args.fetchTimeoutSeconds = Number.parseInt(String(next ?? "20"), 10);
        i += 1;
        break;
      case "--block-on-remote-divergence":
        args.blockOnRemoteDivergence = String(next ?? "1") !== "0";
        i += 1;
        break;
      case "-h":
      case "--help":
        usage();
        process.exit(0);
        break;
      default:
        throw new Error(`Unknown argument: ${arg}`);
    }
  }

  if (!args.stdinFile) {
    throw new Error("--stdin-file is required");
  }
  if (!args.remoteName) {
    throw new Error("--remote-name is required");
  }
  if (
    !Number.isInteger(args.fetchTimeoutSeconds) ||
    args.fetchTimeoutSeconds < 0
  ) {
    throw new Error(
      `Invalid --fetch-timeout-seconds: ${args.fetchTimeoutSeconds}`,
    );
  }

  return args;
}

function printFailure(branchState) {
  if (branchState.relation === "fetch_failed") {
    console.error(
      `[pre-push-branch-state] FAIL branch=${branchState.branchName} fetch_error=${branchState.fetchError}`,
    );
    console.error(
      `  next: verify remote connectivity, then rerun git fetch ${branchState.remoteRef.replace(/^refs\/heads\//, "").includes("/") ? "origin" : "origin"} ${branchState.branchName}`,
    );
    return;
  }

  console.error(
    `[pre-push-branch-state] FAIL branch=${branchState.branchName} remote_moved local=${branchState.localSha} advertised=${branchState.advertisedRemoteSha} fetched=${branchState.fetchedRemoteSha}`,
  );
  console.error(
    `  next: git fetch origin ${branchState.branchName} && (git merge origin/${branchState.branchName} || git rebase origin/${branchState.branchName})`,
  );
}

async function main() {
  const args = parseArgs(process.argv);
  const stdinText = await readFile(args.stdinFile, "utf8");
  const result = resolvePrePushBranchState({
    stdinText,
    remoteName: args.remoteName,
    repoRoot: process.cwd(),
    fetchRemote: args.fetchRemote,
    fetchTimeoutMs: args.fetchTimeoutSeconds * 1000,
    blockOnRemoteDivergence: args.blockOnRemoteDivergence,
  });

  for (const branch of result.branches) {
    console.log(`[pre-push-branch-state] ${summarizeBranchState(branch)}`);
  }

  if (args.writeJson) {
    await writeFile(
      args.writeJson,
      `${JSON.stringify(result, null, 2)}\n`,
      "utf8",
    );
  }

  if (args.json) {
    process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
  }

  if (result.status !== "ok") {
    for (const branch of result.branches) {
      if (
        branch.relation === "fetch_failed" ||
        branch.relation === "remote_moved_and_local_missing_it"
      ) {
        printFailure(branch);
      }
    }
    process.exit(1);
  }
}

if (
  import.meta.url === pathToFileURL(path.resolve(process.argv[1] ?? "")).href
) {
  main().catch((error) => {
    console.error(`[pre-push-branch-state] ${error.message}`);
    process.exit(1);
  });
}
