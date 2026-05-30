import assert from "node:assert/strict";
import {
  mkdtemp,
  mkdir,
  readFile,
  rename,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import {
  normalizeReviewRootOverride,
  resolveReviewRootOverride,
  createReviewQueueBaseJob,
  REVIEW_QUEUE_LANES,
  REVIEW_QUEUE_STATUSES,
} from "./codex-review-state.mjs";
import {
  ensureReviewQueue,
  listReviewJobs,
  reclaimStaleActiveJobs,
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

test("stale active reclaim does not delete a concurrently claimed job", async () => {
  const tempRoot = await mkdtemp(path.join(tmpdir(), "contribution-queue-"));
  try {
    const reviewsDir = path.join(tempRoot, ".code-reviews");
    const sha = "0123456789abcdef0123456789abcdef01234567";
    await ensureReviewQueue(reviewsDir, "codex");
    const activePath = path.join(
      reviewsDir,
      "queue",
      "codex",
      "active",
      `${sha}.json`,
    );
    const pendingPath = path.join(
      reviewsDir,
      "queue",
      "codex",
      "pending",
      `${sha}.json`,
    );
    const oldJob = {
      ...createReviewQueueBaseJob({
        sha,
        trigger: "post-commit",
        source: "test",
        lane: "codex",
      }),
      status: "active",
      updated_at: "2026-01-01T00:00:00.000Z",
      started_at: "2026-01-01T00:00:00.000Z",
      worker: {
        id: "old-worker",
        pid: 999999,
        host: "test",
        claimed_at: "2026-01-01T00:00:00.000Z",
        heartbeat_at: "2026-01-01T00:00:00.000Z",
      },
      lane_states: {
        codex: {
          status: "running",
          started_at: "2026-01-01T00:00:00.000Z",
          completed_at: "",
          finding_count: 0,
          blocker_count: 0,
          major_count: 0,
          minor_count: 0,
        },
      },
    };
    await writeFile(activePath, `${JSON.stringify(oldJob, null, 2)}\n`, "utf8");

    await reclaimStaleActiveJobs(reviewsDir, {
      staleAfterMs: 0,
      nowMs: Date.parse("2026-01-01T00:01:00.000Z"),
      beforeRemoveActive: async ({ activePath, pendingPath, job }) => {
        await rename(pendingPath, activePath);
        await writeFile(
          activePath,
          `${JSON.stringify(
            {
              ...job,
              status: "active",
              worker: {
                id: "new-worker",
                pid: process.pid,
                host: "test",
                claimed_at: "2026-01-01T00:01:00.000Z",
                heartbeat_at: "2026-01-01T00:01:00.000Z",
              },
            },
            null,
            2,
          )}\n`,
          "utf8",
        );
      },
    });

    const active = JSON.parse(await readFile(activePath, "utf8"));
    assert.equal(active.worker.id, "new-worker");
    await assert.rejects(() => readFile(pendingPath, "utf8"), /ENOENT/u);
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
