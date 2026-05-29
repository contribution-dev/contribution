import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

test("hooks enqueue commit review and gate pushes", () => {
  const postCommit = readFileSync(".husky/post-commit", "utf8");
  const prePush = readFileSync(".husky/pre-push", "utf8");
  assert.match(postCommit, /codex-review-enqueue/);
  assert.match(prePush, /codex-review-push-gate/);
  assert.match(prePush, /run-changed-checks\.mjs/);
});

test("post-push hook uses canonical Codex queue only", () => {
  const postPush = readFileSync("scripts/codex-review-post-push", "utf8");
  assert.match(postPush, /queue\/codex\/pending/);
  assert.match(postPush, /queue\/codex\/active/);
  assert.doesNotMatch(postPush, /codex-review-repair-state/);
  assert.doesNotMatch(postPush, /queue\/pending/);
  assert.doesNotMatch(postPush, /CODEX_REVIEW_DIR/);
});
