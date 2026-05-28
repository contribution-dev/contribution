#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { readdirSync, statSync } from "node:fs";
import path from "node:path";

const ROOT = process.cwd();
const DIRECTORIES = ["scripts"];
const EXTENSIONS = new Set([".js", ".mjs"]);

function collectFiles(dir) {
  const absolute = path.join(ROOT, dir);
  const results = [];
  for (const entry of readdirSync(absolute)) {
    const full = path.join(absolute, entry);
    const relative = path.relative(ROOT, full);
    if (relative.includes("node_modules")) {
      continue;
    }
    const stats = statSync(full);
    if (stats.isDirectory()) {
      results.push(...collectFiles(relative));
      continue;
    }
    if (EXTENSIONS.has(path.extname(full))) {
      results.push(relative);
    }
  }
  return results;
}

let failed = false;
for (const file of DIRECTORIES.flatMap(collectFiles).sort()) {
  const result = spawnSync("node", ["--check", file], {
    cwd: ROOT,
    stdio: "inherit",
  });
  if (result.status !== 0) {
    failed = true;
  }
}

if (failed) {
  process.exit(1);
}
