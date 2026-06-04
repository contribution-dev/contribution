import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { processLineReferencesRepoRoot } from "./codex-review-status";
import {
  codexExecSucceeded,
  extractCodexReviewOutput,
} from "./codex-review-commit.mjs";

test("hooks enqueue commit review and gate pushes", () => {
  const postCommit = readFileSync(".husky/post-commit", "utf8");
  const prePush = readFileSync(".husky/pre-push", "utf8");
  assert.match(postCommit, /codex-review-enqueue/);
  assert.match(prePush, /codex-review-push-gate/);
  assert.match(prePush, /run-changed-checks\.mjs/);
  assert.doesNotMatch(postCommit, /CODEX_REVIEW_DIR/);
  assert.doesNotMatch(prePush, /CODEX_REVIEW_DIR/);
});

test("pre-commit hook reuses the shared bootstrap", () => {
  const preCommit = readFileSync(".husky/pre-commit", "utf8");
  assert.match(preCommit, /codex-bootstrap\.sh/);
  assert.doesNotMatch(preCommit, /\.tools\/go\/bin/);
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
  assert.match(status, /processLineReferencesRepoRoot\(line, repoRoot\)/);
  assert.match(
    status,
    /processSnapshot\(repoRoot, filterSha, \{\s+requireRepoRoot: true,/,
  );
  assert.match(
    status,
    /hasRunningLaunchctlWorkers\(launchctlLines\)\s+\? launchctlLines\s+: repoWorkerProcesses/,
  );
});

test("review status repo process matching rejects sibling path prefixes", () => {
  const repoRoot = "/Users/gabe/Sites/contribution";
  assert.equal(
    processLineReferencesRepoRoot(
      `123 00:01 /node ${repoRoot}/scripts/codex-review-worker --lane codex --reviews-dir ${repoRoot}/.code-reviews`,
      repoRoot,
    ),
    true,
  );
  assert.equal(
    processLineReferencesRepoRoot(
      `124 00:01 /node ${repoRoot}-website/scripts/codex-review-worker --lane codex --reviews-dir ${repoRoot}-website/.code-reviews`,
      repoRoot,
    ),
    false,
  );
  assert.equal(
    processLineReferencesRepoRoot(
      `125 00:01 /SkyComputerUseClient turn-ended {"cwd":"${repoRoot}","message":"codex-review-worker --lane codex"}`,
      repoRoot,
    ),
    false,
  );
});

test("commit review subprocess is isolated from user plugins and rules", () => {
  const commitReview = readFileSync("scripts/codex-review-commit.mjs", "utf8");
  assert.match(
    commitReview,
    /mkdtempSync\(\s*path\.join\(tmpdir\(\), "contribution-codex-home-"\)/,
  );
  assert.match(
    commitReview,
    /symlinkSync\(sourceAuthPath, path\.join\(isolatedHome, "auth\.json"\)\)/,
  );
  assert.match(commitReview, /CODEX_HOME: isolatedCodexHome\.path/);
  for (const feature of [
    "plugins",
    "apps",
    "browser_use",
    "browser_use_external",
    "computer_use",
    "multi_agent",
  ]) {
    assert.match(commitReview, new RegExp(`"--disable",\\s+"${feature}"`));
  }
  assert.match(commitReview, /"--ignore-user-config"/);
  assert.match(commitReview, /"--ignore-rules"/);
  assert.match(commitReview, /"--ephemeral"/);
});

test("backlog remediation subprocess is isolated from user plugins and rules", () => {
  const remediator = readFileSync(
    "scripts/codex-review-remediate-backlog",
    "utf8",
  );
  assert.match(
    remediator,
    /mkdtempSync\(\s*path\.join\(tmpdir\(\), "contribution-codex-home-"\)/,
  );
  assert.match(
    remediator,
    /symlinkSync\(sourceAuthPath, path\.join\(isolatedHome, "auth\.json"\)\)/,
  );
  assert.match(remediator, /CODEX_HOME: isolatedCodexHome\.path/);
  for (const feature of [
    "plugins",
    "apps",
    "browser_use",
    "browser_use_external",
    "computer_use",
    "multi_agent",
  ]) {
    assert.match(remediator, new RegExp(`"--disable",\\s+"${feature}"`));
  }
  assert.match(remediator, /"--ignore-user-config"/);
  assert.match(remediator, /"--ignore-rules"/);
  assert.match(remediator, /"--ephemeral"/);
});

test("commit review accepts final output despite non-zero codex exit", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "contribution-codex-output-"));
  try {
    const outputPath = path.join(dir, "review.json");
    writeFileSync(
      outputPath,
      '{"schema_version":2,"summary":"","findings":[]}\n',
    );
    assert.equal(codexExecSucceeded({ code: 1, outputPath }), true);
    writeFileSync(outputPath, "not json\n");
    assert.equal(codexExecSucceeded({ code: 1, outputPath }), false);
    assert.equal(codexExecSucceeded({ code: 1, outputPath: "" }), false);
    assert.equal(
      codexExecSucceeded({ code: 0, outputPath: path.join(dir, "missing") }),
      true,
    );
    assert.equal(
      codexExecSucceeded({ code: 1, outputPath, timedOut: true }),
      false,
    );
    assert.equal(
      codexExecSucceeded({
        code: 1,
        outputPath,
        spawnError: new Error("spawn failed"),
      }),
      false,
    );
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("commit review clears stale output before each codex attempt", () => {
  const commitReview = readFileSync("scripts/codex-review-commit.mjs", "utf8");
  assert.match(commitReview, /rmSync\(outputPath,\s*{\s*force:\s*true\s*}\)/);
});

test("commit review recovers final JSON from codex stdout", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "contribution-codex-output-"));
  try {
    const outputPath = path.join(dir, "review.json");
    const transcript = [
      "codex",
      '{"schema_version":2,"summary":"early","findings":[]}',
      "tokens used",
      '\u001b[2mcodex\u001b[0m {"schema_version":2,"summary":"final","findings":[{"severity":"major"}]}',
    ].join("\n");
    const carriageTranscript = [
      "codex",
      '{"schema_version":2,"summary":"early","findings":[]}',
      "tokens used",
      "42,000",
      '{"schema_version":2,"summary":"final","findings":[{"severity":"major"}]}',
    ].join("\r");
    const singleLineTranscript = [
      'codex {"schema_version":2,"summary":"early","findings":[]}',
      'tokens used 42,000 {"schema_version":2,"summary":"final","findings":[{"severity":"major"}]}',
    ].join(" ");
    assert.equal(
      extractCodexReviewOutput(transcript),
      '{\n  "schema_version": 2,\n  "summary": "final",\n  "findings": [\n    {\n      "severity": "major"\n    }\n  ]\n}\n',
    );
    assert.equal(
      extractCodexReviewOutput(carriageTranscript),
      '{\n  "schema_version": 2,\n  "summary": "final",\n  "findings": [\n    {\n      "severity": "major"\n    }\n  ]\n}\n',
    );
    assert.equal(
      extractCodexReviewOutput(singleLineTranscript),
      '{\n  "schema_version": 2,\n  "summary": "final",\n  "findings": [\n    {\n      "severity": "major"\n    }\n  ]\n}\n',
    );
    assert.equal(
      codexExecSucceeded({ code: 1, outputPath, outputText: transcript }),
      true,
    );
    assert.match(readFileSync(outputPath, "utf8"), /"summary": "final"/);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
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
