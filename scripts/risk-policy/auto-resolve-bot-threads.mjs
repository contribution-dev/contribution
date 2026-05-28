#!/usr/bin/env node

import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { DEFAULT_CONTRACT_PATH, REPO_ROOT, loadRiskContract } from "./lib.mjs";
import { consumeCommonGithubArg } from "./shared/common-github-args.mjs";
import {
  createIssueComment,
  getPullRequest,
  graphqlRequest,
  listCheckRunsForHead,
} from "./github-client.mjs";
import {
  REVIEW_STATUS,
  isStaleWorkflowHead,
  resolveReviewStatus,
} from "./review-automation-lib.mjs";

const THREADS_QUERY = `
query PullRequestThreads($owner: String!, $repo: String!, $number: Int!, $cursor: String) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      reviewThreads(first: 100, after: $cursor) {
        nodes {
          id
          isResolved
          comments(first: 100) {
            nodes {
              author {
                login
              }
            }
          }
        }
        pageInfo {
          hasNextPage
          endCursor
        }
      }
    }
  }
}
`;

const RESOLVE_MUTATION = `
mutation ResolveReviewThread($threadId: ID!) {
  resolveReviewThread(input: {threadId: $threadId}) {
    thread {
      id
      isResolved
    }
  }
}
`;

function parseArgs(argv) {
  const args = {
    owner: process.env.GITHUB_REPOSITORY_OWNER ?? "",
    repo: process.env.GITHUB_REPOSITORY?.split("/")[1] ?? "",
    pullNumber: Number.parseInt(process.env.PR_NUMBER ?? "0", 10),
    token: process.env.GITHUB_TOKEN ?? "",
    contractPath: DEFAULT_CONTRACT_PATH,
    headSha: process.env.PR_HEAD_SHA ?? "",
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

function requireArg(value, message) {
  if (!value) throw new Error(message);
}

async function writeReport(report) {
  const outputDir = path.resolve(REPO_ROOT, "output", "test-metrics");
  await mkdir(outputDir, { recursive: true });
  await writeFile(
    path.join(outputDir, "auto-resolve-threads.json"),
    `${JSON.stringify(report, null, 2)}\n`,
    "utf8",
  );
}

export function isBotOnlyThread(thread, allowedBotAuthors) {
  if (!thread || thread.isResolved) return false;
  const comments = Array.isArray(thread.comments?.nodes)
    ? thread.comments.nodes
    : [];
  if (comments.length === 0) return false;
  return comments.every((comment) => {
    const login = String(comment?.author?.login ?? "").toLowerCase();
    return allowedBotAuthors.has(login);
  });
}

async function loadAllThreads({ token, owner, repo, pullNumber }) {
  const threads = [];
  let cursor = null;
  while (true) {
    const response = await graphqlRequest({
      token,
      query: THREADS_QUERY,
      variables: {
        owner,
        repo,
        number: pullNumber,
        cursor,
      },
    });
    const payload = response?.data?.repository?.pullRequest?.reviewThreads;
    const nodes = Array.isArray(payload?.nodes) ? payload.nodes : [];
    threads.push(...nodes);
    if (!payload?.pageInfo?.hasNextPage) break;
    cursor = payload.pageInfo.endCursor;
  }
  return threads;
}

async function resolveThread({ token, threadId }) {
  return graphqlRequest({
    token,
    query: RESOLVE_MUTATION,
    variables: { threadId },
  });
}

export async function executeAutoResolve(args) {
  requireArg(args.owner, "Missing owner.");
  requireArg(args.repo, "Missing repo.");
  requireArg(args.token, "Missing token.");

  if (!Number.isInteger(args.pullNumber) || args.pullNumber <= 0) {
    const report = { status: "skipped", reason: "no-pull-request" };
    await writeReport(report);
    return report;
  }

  const contract = await loadRiskContract(args.contractPath);
  const automation = contract.reviewAutomation?.autoResolve ?? {};
  const enabled = automation.enabled ?? true;
  if (!enabled) {
    const report = { status: "skipped", reason: "auto-resolve-disabled" };
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
  requireArg(headSha, "Missing head SHA.");

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
  const requireSuccess = automation.requireCurrentHeadReviewSuccess ?? true;
  if (requireSuccess && reviewState.status !== REVIEW_STATUS.SUCCESS) {
    const report = {
      status: "skipped",
      reason: "review-not-success",
      reviewStatus: reviewState.status,
    };
    await writeReport(report);
    return report;
  }

  const allowedBotAuthors = new Set(
    (
      automation.allowedBotAuthors ?? ["coderabbitai[bot]", "greptile-app[bot]"]
    ).map((author) => String(author).toLowerCase()),
  );
  const threads = await loadAllThreads({
    token: args.token,
    owner: args.owner,
    repo: args.repo,
    pullNumber: args.pullNumber,
  });
  const botOnlyThreads = threads.filter((thread) =>
    isBotOnlyThread(thread, allowedBotAuthors),
  );

  const resolvedThreadIds = [];
  for (const thread of botOnlyThreads) {
    await resolveThread({ token: args.token, threadId: thread.id });
    resolvedThreadIds.push(thread.id);
  }

  if (resolvedThreadIds.length > 0) {
    await createIssueComment({
      token: args.token,
      owner: args.owner,
      repo: args.repo,
      issueNumber: args.pullNumber,
      body: `Resolved ${resolvedThreadIds.length} bot-only review thread(s) after clean current-head review.`,
    });
  }

  const report = {
    status: resolvedThreadIds.length > 0 ? "resolved" : "no-op",
    pullNumber: args.pullNumber,
    headSha,
    reviewStatus: reviewState.status,
    resolvedThreadCount: resolvedThreadIds.length,
    resolvedThreadIds,
  };
  await writeReport(report);
  return report;
}

async function main() {
  const args = parseArgs(process.argv);
  const result = await executeAutoResolve(args);
  console.log(
    `[auto-resolve] status=${result.status} resolved=${result.resolvedThreadCount ?? 0}`,
  );
}

if (
  import.meta.url === pathToFileURL(path.resolve(process.argv[1] ?? "")).href
) {
  main().catch((error) => {
    console.error(`[auto-resolve] Failed: ${error.message}`);
    process.exit(1);
  });
}
