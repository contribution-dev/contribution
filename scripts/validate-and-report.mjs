#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

function parseArgs(argv) {
  return {
    skipInstall: argv.includes("--skip-install"),
  };
}

function commandAvailable(command) {
  const result = spawnSync("sh", ["-lc", `command -v ${command}`], {
    stdio: "ignore",
  });
  return result.status === 0;
}

function run(command, args, { optional = false, env = process.env } = {}) {
  if (optional && !commandAvailable(command)) {
    console.log(
      `[validate] SKIP ${command} ${args.join(" ")} (missing ${command})`,
    );
    return;
  }
  const printable = [command, ...args].join(" ");
  console.log(`[validate] START ${printable}`);
  const result = spawnSync(command, args, {
    stdio: "inherit",
    env,
  });
  if (result.status !== 0) {
    throw new Error(`${printable} failed with exit ${result.status ?? 1}`);
  }
  console.log(`[validate] PASS ${printable}`);
}

export function validationInstallEnv(baseEnv = process.env) {
  return {
    ...baseEnv,
    CI: String(baseEnv.CI ?? "").trim() || "true",
    HUSKY: "0",
  };
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  if (!args.skipInstall) {
    run("pnpm", ["install", "--frozen-lockfile"], {
      env: validationInstallEnv(),
    });
  }
  run("pnpm", ["agents:check"]);
  run("pnpm", ["lint:scripts"]);
  run("pnpm", ["test:scripts"]);
  run("pnpm", ["format:check"]);
  run("go", ["vet", "./..."]);
  run("go", ["test", "./..."]);
  run("go", ["test", "-race", "./..."]);
  run("go", [
    "build",
    "-trimpath",
    "-o",
    "bin/contribution",
    "./cmd/contribution",
  ]);
  run("pnpm", ["dogfood:smoke"]);
  run("pnpm", ["dogfood:real"]);
  run("pnpm", ["lint:go"]);
  run("govulncheck", ["./..."], { optional: true });
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
    console.error(`[validate] ${error.message}`);
    process.exit(1);
  }
}
