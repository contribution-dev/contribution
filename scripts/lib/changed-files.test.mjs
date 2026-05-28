import test from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { classifyChangedFiles, getChangedFiles } from "./changed-files.mjs";

test("classifies Go CLI changes as app-relevant for changed checks", () => {
  const result = classifyChangedFiles(["cmd/contribution/main.go"]);
  assert.equal(result.goRelevant, true);
  assert.equal(result.appRelevant, true);
});

test("classifies docs-only changes", () => {
  const result = classifyChangedFiles([
    "docs/tooling-validation.md",
    "README.md",
  ]);
  assert.equal(result.docsOnly, true);
  assert.equal(result.goRelevant, false);
});

test("collects unstaged modified files without truncating path names", () => {
  const repo = mkdtempSync(path.join(tmpdir(), "contribution-changed-files-"));
  execFileSync("git", ["init", "-b", "main"], { cwd: repo, stdio: "ignore" });
  execFileSync("git", ["config", "user.email", "test@example.test"], {
    cwd: repo,
  });
  execFileSync("git", ["config", "user.name", "Test User"], { cwd: repo });
  writeFileSync(path.join(repo, "CHANGELOG.md"), "# Changelog\n");
  execFileSync("git", ["add", "CHANGELOG.md"], { cwd: repo });
  execFileSync("git", ["commit", "-m", "initial"], {
    cwd: repo,
    stdio: "ignore",
  });

  writeFileSync(path.join(repo, "CHANGELOG.md"), "# Changelog\n\nChanged.\n");
  const result = getChangedFiles({ cwd: repo });

  assert(result.files.includes("CHANGELOG.md"));
  assert(!result.files.includes("HANGELOG.md"));
});
