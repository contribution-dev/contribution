import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";

test("accepts changed files from a newline-delimited file list", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "contribution-contract-files-"));
  const fileList = path.join(dir, "files.txt");
  try {
    writeFileSync(
      fileList,
      ["internal/cli/root.go", "docs/cli-contract.md"].join("\n"),
    );

    const result = spawnSync(
      process.execPath,
      ["scripts/check-cli-contract-coverage.mjs", "--files-from", fileList],
      {
        cwd: process.cwd(),
        encoding: "utf8",
      },
    );

    assert.equal(result.status, 0);
    assert.match(result.stdout, /coverage artifact\(s\) changed/u);
    assert.equal(result.stderr, "");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("treats architecture reference updates as CLI contract coverage", () => {
  const result = spawnSync(
    process.execPath,
    [
      "scripts/check-cli-contract-coverage.mjs",
      "--files",
      "docs/reference/architecture.md",
    ],
    {
      cwd: process.cwd(),
      encoding: "utf8",
    },
  );

  assert.equal(result.status, 0);
  assert.match(result.stdout, /coverage artifact\(s\) changed/u);
  assert.equal(result.stderr, "");
});

test("requires coverage for CLI-facing changes", () => {
  const result = spawnSync(
    process.execPath,
    [
      "scripts/check-cli-contract-coverage.mjs",
      "--files",
      "internal/preflight/preflight.go",
    ],
    {
      cwd: process.cwd(),
      encoding: "utf8",
    },
  );

  assert.equal(result.status, 1);
  assert.match(
    result.stderr,
    /CLI-facing changes require matching contract coverage/u,
  );
});
