import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import {
  normalizeReviewRootOverride,
  resolveReviewRootOverride,
  REVIEW_QUEUE_LANES,
  REVIEW_QUEUE_STATUSES,
} from "./codex-review-state.mjs";
import {
  ensureReviewQueue,
  listReviewJobs,
} from "../codex-review-queue-lib.mjs";

test("review queue lanes stay Codex-only", () => {
  assert.deepEqual(REVIEW_QUEUE_LANES, ["codex"]);
});

test("review queue statuses stay pending and active only", () => {
  assert.deepEqual(REVIEW_QUEUE_STATUSES, ["pending", "active"]);
});

test("review root override uses canonical env only", () => {
  const previousCodeReviewDir = process.env.CODE_REVIEW_DIR;
  const previousCodexReviewDir = process.env.CODEX_REVIEW_DIR;
  try {
    delete process.env.CODE_REVIEW_DIR;
    process.env.CODEX_REVIEW_DIR = "/tmp/ignored-legacy-review-dir";
    assert.equal(resolveReviewRootOverride(), "");

    process.env.CODE_REVIEW_DIR = "/tmp/current-review-dir";
    assert.equal(resolveReviewRootOverride(), "/tmp/current-review-dir");
  } finally {
    restoreEnvValue("CODE_REVIEW_DIR", previousCodeReviewDir);
    restoreEnvValue("CODEX_REVIEW_DIR", previousCodexReviewDir);
  }
});

test("review root override collapses only canonical log paths", () => {
  const repoRoot = "/tmp/repo";
  assert.equal(
    normalizeReviewRootOverride(repoRoot, ".code-reviews/logs/logs", repoRoot),
    path.join(repoRoot, ".code-reviews"),
  );
  assert.equal(
    normalizeReviewRootOverride(repoRoot, ".codex/reviews/logs", repoRoot),
    path.join(repoRoot, ".codex/reviews/logs"),
  );
});

test("canonical queue setup does not migrate top-level queue jobs", async () => {
  const tempRoot = await mkdtemp(path.join(tmpdir(), "contribution-queue-"));
  try {
    const reviewsDir = path.join(tempRoot, ".code-reviews");
    const sha = "0123456789abcdef0123456789abcdef01234567";
    const legacyPendingDir = path.join(reviewsDir, "queue", "pending");
    await mkdir(legacyPendingDir, { recursive: true });
    await writeFile(
      path.join(legacyPendingDir, `${sha}.json`),
      `${JSON.stringify({ sha, status: "pending" })}\n`,
      "utf8",
    );

    await ensureReviewQueue(reviewsDir, "codex");
    assert.deepEqual(
      (await listReviewJobs(reviewsDir, undefined, { lane: "codex" })).map(
        (entry) => entry.job.sha,
      ),
      [],
    );
    assert.equal(
      await readFile(path.join(legacyPendingDir, `${sha}.json`), "utf8"),
      `${JSON.stringify({ sha, status: "pending" })}\n`,
    );
  } finally {
    await rm(tempRoot, { recursive: true, force: true });
  }
});

function restoreEnvValue(key, value) {
  if (value === undefined) {
    delete process.env[key];
    return;
  }
  process.env[key] = value;
}
