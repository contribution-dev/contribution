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

test("launchctl recovery includes validation and diagnostics", () => {
  const launchctl = readFileSync("scripts/codex-review-launchctl", "utf8");
  assert.match(launchctl, /validateLaunchAgentPlan/);
  assert.match(
    launchctl,
    /validateReadableFile\(scriptPath, "Review worker script"\)/,
  );
  assert.doesNotMatch(launchctl, /validateExecutable\(scriptPath/);
  assert.match(launchctl, /plutil/);
  assert.match(launchctl, /recent_launchd_log/);
  assert.match(launchctl, /launchctl\("bootout"/);
  assert.match(launchctl, /launchctl\("bootstrap"/);
});

test("review status reports active-without-worker health", () => {
  const status = readFileSync("scripts/codex-review-status", "utf8");
  assert.match(status, /\[worker-health\]/);
  assert.match(status, /active_without_worker=1/);
  assert.match(status, /run_pnpm_review_recover/);
});

test("review status process fallback is scoped to the repo root", () => {
  const status = readFileSync("scripts/codex-review-status", "utf8");
  assert.match(status, /requireRepoRoot && !line\.includes\(repoRoot\)/);
  assert.match(
    status,
    /processSnapshot\(repoRoot, filterSha, \{\s+requireRepoRoot: true,/,
  );
  assert.match(
    status,
    /hasRunningLaunchctlWorkers\(launchctlLines\)\s+\? launchctlLines\s+: repoWorkerProcesses/,
  );
});

test("review package scripts use the repo tool bootstrap", () => {
  const pkg = JSON.parse(readFileSync("package.json", "utf8"));
  for (const name of [
    "review:recover",
    "review:status",
    "review:backfill",
    "review:queue:backlog",
    "review:remediate:backlog",
    "review:launchctl",
  ]) {
    assert.match(pkg.scripts[name], /^scripts\/with-tools /);
  }
});
