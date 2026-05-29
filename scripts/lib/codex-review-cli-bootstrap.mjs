#!/usr/bin/env node

import path from "node:path";
import {
  resolveDurableWorkerReviewsDir,
  resolveRepoRoot,
  resolveReviewsDir,
} from "../codex-review-inbox-lib.mjs";

export async function prepareCodexReviewCliContext({
  invocationCwd = process.cwd(),
  reviewsDirOverride = "",
  durableWorker = false,
  env = process.env,
  warnPrefix = "codex-review",
  logger = console,
} = {}) {
  const repoRoot = env.REPO_ROOT || resolveRepoRoot(invocationCwd);
  const reviewsDir = durableWorker
    ? await resolveDurableWorkerReviewsDir(
        repoRoot,
        reviewsDirOverride,
        invocationCwd,
      )
    : await resolveReviewsDir(repoRoot, reviewsDirOverride, invocationCwd);

  if (
    durableWorker &&
    reviewsDirOverride &&
    path.resolve(reviewsDir) !== path.resolve(reviewsDirOverride)
  ) {
    logger.warn(
      `[${warnPrefix}] Redirecting reviews dir ${reviewsDirOverride} -> ${reviewsDir}`,
    );
  }

  return { repoRoot, reviewsDir };
}

export async function resolveCodexReviewCliContext({
  invocationCwd = process.cwd(),
  reviewsDirOverride = "",
  durableWorker = false,
  env = process.env,
} = {}) {
  const repoRoot = env.REPO_ROOT || resolveRepoRoot(invocationCwd);
  const reviewsDir = durableWorker
    ? await resolveDurableWorkerReviewsDir(
        repoRoot,
        reviewsDirOverride,
        invocationCwd,
      )
    : await resolveReviewsDir(repoRoot, reviewsDirOverride, invocationCwd);
  return { repoRoot, reviewsDir };
}
