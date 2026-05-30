import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import {
  defaultIsReviewInProgress,
  executePushGate,
} from "./codex-review-push-gate-lib.mjs";
import { enqueueReviewJob } from "./codex-review-queue-lib.mjs";

const SHA_A = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const SHA_B = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";

function cleanReviewState(overrides = {}) {
  return {
    hasReportArtifact: true,
    missingRequiredReviews: false,
    queuePaused: false,
    queueStatus: "",
    queueStale: false,
    actionable: false,
    failed: false,
    incomplete: false,
    blocking: false,
    reviewStatus: "ok",
    worstSeverity: "none",
    findingsCount: 0,
    ...overrides,
  };
}

test("push gate blocks known non-tip findings without waiting on review completion", async () => {
  const repoRoot = await mkdtemp(path.join(tmpdir(), "push-gate-repo-"));
  const reviewsDir = path.join(repoRoot, ".code-reviews");
  try {
    const result = await executePushGate({
      repoRoot,
      reviewsDir,
      stdinText: "",
      pushContext: {
        outgoingShas: [SHA_A, SHA_B],
        pushTipShas: [SHA_B],
        headBranchName: "feature",
        pushedBranchNames: ["feature"],
        shaBranches: {
          [SHA_A]: ["feature"],
          [SHA_B]: ["feature"],
        },
      },
      waitForReview: async () => ({ waited: false, timedOut: false }),
      readReviewState: async ({ sha }) =>
        sha === SHA_A
          ? cleanReviewState({
              actionable: true,
              blocking: true,
              worstSeverity: "blocker",
              findingsCount: 1,
            })
          : cleanReviewState(),
    });

    assert.equal(result.summary.shas, 2);
    assert.equal(result.summary.blocked, 1);
    assert.equal(result.blocked[0].sha, SHA_A);
  } finally {
    await rm(repoRoot, { recursive: true, force: true });
  }
});

test("push gate skips incomplete non-tip reviews without findings", async () => {
  const repoRoot = await mkdtemp(path.join(tmpdir(), "push-gate-repo-"));
  const reviewsDir = path.join(repoRoot, ".code-reviews");
  try {
    const result = await executePushGate({
      repoRoot,
      reviewsDir,
      stdinText: "",
      pushContext: {
        outgoingShas: [SHA_A, SHA_B],
        pushTipShas: [SHA_B],
        headBranchName: "feature",
        pushedBranchNames: ["feature"],
        shaBranches: {
          [SHA_A]: ["feature"],
          [SHA_B]: ["feature"],
        },
      },
      waitForReview: async () => ({ waited: false, timedOut: false }),
      readReviewState: async ({ sha }) =>
        sha === SHA_A
          ? cleanReviewState({
              actionable: true,
              incomplete: true,
              blocking: true,
              reviewStatus: "partial_success",
              failureReason: "exec_failed",
            })
          : cleanReviewState(),
    });

    assert.equal(result.summary.shas, 2);
    assert.equal(result.summary.blocked, 0);
    assert.deepEqual(result.blocked, []);
  } finally {
    await rm(repoRoot, { recursive: true, force: true });
  }
});

test("pending review queue jobs are treated as unsettled", async () => {
  const tempRoot = await mkdtemp(path.join(tmpdir(), "push-gate-queue-"));
  const reviewsDir = path.join(tempRoot, ".code-reviews");
  try {
    await enqueueReviewJob({
      reviewsDir,
      sha: SHA_A,
      trigger: "post-push",
      source: "test",
      reason: "push-gate",
    });

    assert.equal(
      await defaultIsReviewInProgress({ reviewsDir, sha: SHA_A }),
      true,
    );
  } finally {
    await rm(tempRoot, { recursive: true, force: true });
  }
});
