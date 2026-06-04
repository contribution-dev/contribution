import test from "node:test";
import assert from "node:assert/strict";
import { gitChangedFilesArgs, validateDiffRange } from "./git-diff-args.mjs";

test("git diff argv builder never constructs a shell command", () => {
  assert.deepEqual(gitChangedFilesArgs({}), [
    "diff",
    "--name-only",
    "--diff-filter=ACMR",
  ]);
  assert.deepEqual(gitChangedFilesArgs({ staged: true }), [
    "diff",
    "--name-only",
    "--diff-filter=ACMR",
    "--cached",
  ]);
  assert.deepEqual(gitChangedFilesArgs({ diffRange: "origin/main...HEAD" }), [
    "diff",
    "--name-only",
    "--diff-filter=ACMR",
    "origin/main...HEAD",
  ]);
});

test("git diff range rejects shell and option injection input", () => {
  for (const value of [
    "HEAD;touch /tmp/contribution-security-poc",
    "HEAD && touch /tmp/contribution-security-poc",
    "--output=/tmp/contribution-security-poc",
  ]) {
    assert.throws(() => validateDiffRange(value), /--diff-range/u);
  }
});
