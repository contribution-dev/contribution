#!/usr/bin/env node

import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import {
  DEFAULT_CONTRACT_PATH,
  REPO_ROOT,
  loadRiskContract,
  normalizePath,
} from "./lib.mjs";
import { consumeCommonGithubArg } from "./shared/common-github-args.mjs";
import {
  createIssueComment,
  getPullRequest,
  listCheckRunsForHead,
  listIssueComments,
  listPullRequestFiles,
} from "./github-client.mjs";
import {
  REVIEW_STATUS,
  computeRiskTier,
  hasShaDedupComment,
  isStaleWorkflowHead,
  isReviewRequired,
  resolveReviewStatus,
} from "./review-automation-lib.mjs";

function parseArgs(argv) {
  const args = {
    owner: process.env.GITHUB_REPOSITORY_OWNER ?? "",
    repo: process.env.GITHUB_REPOSITORY?.split("/")[1] ?? "",
    pullNumber: Number.parseInt(process.env.PR_NUMBER ?? "0", 10),
    headSha: process.env.PR_HEAD_SHA ?? "",
    token: process.env.GITHUB_TOKEN ?? "",
    contractPath: DEFAULT_CONTRACT_PATH,
  };

  for (let i = 2; i < argv.length; i += 1) {
    const arg = argv[i];
    const next = argv[i + 1];
    const consumed = consumeCommonGithubArg(
      args,
      arg,
      next,
      DEFAULT_CONTRACT_PATH,
    );
    if (consumed > 0) {
      i += consumed;
      continue;
    }
    switch (arg) {
      default:
        break;
    }
  }

  return args;
}

async function writeReport(report) {
  const outputDir = path.resolve(REPO_ROOT, "output", "test-metrics");
  await mkdir(outputDir, { recursive: true });
  await writeFile(
    path.join(outputDir, "review-rerun.json"),
    `${JSON.stringify(report, null, 2)}\n`,
    "utf8",
  );
}

function requireArg(value, message) {
  if (!value) throw new Error(message);
}

function buildDefaults(contract) {
  const rerun = contract.reviewAutomation?.rerun ?? {};
  return {
    enabled: rerun.enabled ?? true,
    triggerOnStatuses: rerun.triggerOnStatuses ?? [
      "no-match",
      "pending",
      "non-success",
      "timeout",
    ],
    marker: rerun.marker ?? "<!-- review-agent-auto-rerun -->",
    shaTokenPrefix: rerun.shaTokenPrefix ?? "sha:",
    commentTemplate: rerun.commentTemplate ?? "@review-agent please re-review",
  };
}

export function shouldPostRerunComment({
  reviewStatus,
  triggerOnStatuses,
  alreadyRequestedForSha,
}) {
  if (alreadyRequestedForSha) return false;
  if (!Array.isArray(triggerOnStatuses)) return false;
  return triggerOnStatuses.includes(reviewStatus);
}

export async function executeReviewRerun(args) {
  requireArg(args.owner, "Missing owner.");
  requireArg(args.repo, "Missing repo.");
  requireArg(args.token, "Missing token.");

  if (!Number.isInteger(args.pullNumber) || args.pullNumber <= 0) {
    const report = {
      status: "skipped",
      reason: "no-pull-request",
    };
    await writeReport(report);
    return report;
  }

  const contract = await loadRiskContract(args.contractPath);
  const defaults = buildDefaults(contract);
  if (!defaults.enabled) {
    const report = {
      status: "skipped",
      reason: "review-automation-disabled",
    };
    await writeReport(report);
    return report;
  }

  const pr = await getPullRequest({
    token: args.token,
    owner: args.owner,
    repo: args.repo,
    pullNumber: args.pullNumber,
  });
  if (pr.state !== "open" || pr.draft) {
    const report = {
      status: "skipped",
      reason: pr.state !== "open" ? "pr-not-open" : "pr-draft",
      pullNumber: args.pullNumber,
    };
    await writeReport(report);
    return report;
  }

  const currentHeadSha = String(pr.head?.sha ?? "");
  if (
    args.headSha &&
    currentHeadSha &&
    isStaleWorkflowHead({
      eventHeadSha: args.headSha,
      currentHeadSha,
    })
  ) {
    const report = {
      status: "skipped",
      reason: "stale-workflow-run",
      pullNumber: args.pullNumber,
      eventHeadSha: args.headSha,
      currentHeadSha,
    };
    await writeReport(report);
    return report;
  }

  const headSha = args.headSha || currentHeadSha;
  requireArg(headSha, "Missing PR head SHA.");

  const prFiles = await listPullRequestFiles({
    token: args.token,
    owner: args.owner,
    repo: args.repo,
    pullNumber: args.pullNumber,
  });
  const changedFiles = prFiles.map((file) =>
    normalizePath(String(file.filename ?? "")),
  );
  const riskTier = computeRiskTier(contract, changedFiles);
  const reviewRequired = isReviewRequired(contract, riskTier);
  if (!reviewRequired) {
    const report = {
      status: "skipped",
      reason: "review-not-required-for-tier",
      riskTier,
      pullNumber: args.pullNumber,
      headSha,
    };
    await writeReport(report);
    return report;
  }

  const checkRuns = await listCheckRunsForHead({
    token: args.token,
    owner: args.owner,
    repo: args.repo,
    headSha,
  });
  const reviewState = resolveReviewStatus(
    checkRuns,
    contract.reviewAgent.checkRunNamePatterns,
  );
  const comments = await listIssueComments({
    token: args.token,
    owner: args.owner,
    repo: args.repo,
    issueNumber: args.pullNumber,
  });
  const alreadyRequestedForSha = hasShaDedupComment({
    comments,
    marker: defaults.marker,
    shaTokenPrefix: defaults.shaTokenPrefix,
    headSha,
  });

  const postComment = shouldPostRerunComment({
    reviewStatus: reviewState.status,
    triggerOnStatuses: defaults.triggerOnStatuses,
    alreadyRequestedForSha,
  });

  let commentId = null;
  if (postComment) {
    const body = `${defaults.marker}\n${defaults.commentTemplate}\n${defaults.shaTokenPrefix}${headSha}`;
    const comment = await createIssueComment({
      token: args.token,
      owner: args.owner,
      repo: args.repo,
      issueNumber: args.pullNumber,
      body,
    });
    commentId = comment?.id ?? null;
  }

  const report = {
    status: postComment ? "comment-posted" : "no-comment",
    pullNumber: args.pullNumber,
    riskTier,
    headSha,
    reviewStatus: reviewState.status,
    latestRunId: reviewState.latestRun?.id ?? null,
    alreadyRequestedForSha,
    postedCommentId: commentId,
  };
  await writeReport(report);
  return report;
}

async function main() {
  const args = parseArgs(process.argv);
  const result = await executeReviewRerun(args);
  console.log(
    `[review-rerun] status=${result.status} pr=${result.pullNumber ?? "n/a"} review=${result.reviewStatus ?? "n/a"}`,
  );
}

if (
  import.meta.url === pathToFileURL(path.resolve(process.argv[1] ?? "")).href
) {
  main().catch((error) => {
    console.error(`[review-rerun] Failed: ${error.message}`);
    process.exit(1);
  });
}
