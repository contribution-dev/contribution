#!/usr/bin/env node

import { existsSync } from "node:fs";
import path from "node:path";

function fail(message, details = []) {
  console.error(message);
  for (const detail of details) {
    console.error(`- ${detail}`);
  }
  process.exit(1);
}

const root = process.cwd();
const nodeModulesPath = path.join(root, "node_modules");
const virtualStorePath = path.join(nodeModulesPath, ".pnpm");

if (!existsSync(nodeModulesPath) || !existsSync(virtualStorePath)) {
  fail("agents:check requires installed dependencies.", [
    "Expected workspace install state at node_modules/.pnpm.",
    "Run `pnpm install` before `pnpm agents:check`.",
  ]);
}

console.log("Install prerequisites check passed.");
