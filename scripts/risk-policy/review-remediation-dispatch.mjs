#!/usr/bin/env node

import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import {
  DEFAULT_CONTRACT_PATH,
  REPO_ROOT,
  loadRiskContract,
  normalizePath,
  pathMatchesAnyGlob,
} from "./lib.mjs";
import { consumeCommonGithubArg } from "./shared/common-github-args.mjs";
import {
  createIssueComment,
  getPullRequest,
  listPullRequestFiles,
} from "./github-client.mjs";
import {
  computeRiskTier,
  isStaleWorkflowHead,
} from "./review-automation-lib.mjs";
import { executeFindingsNormalize } from "./review-findings-normalize.mjs";

function parseArgs(argv) {
  const args = {
    owner: process.env.GITHUB_REPOSITORY_OWNER ?? "",
    repo: process.env.GITHUB_REPOSITORY?.split("/")[1] ?? "",
    pullNumber: Number.parseInt(process.env.PR_NUMBER ?? "0", 10),
    token: process.env.GITHUB_TOKEN ?? "",
    headSha: process.env.PR_HEAD_SHA ?? "",
    runId: process.env.GITHUB_RUN_ID ?? "",
    contractPath: DEFAULT_CONTRACT_PATH,
    webhookUrl: process.env.REMEDIATION_WEBHOOK_URL ?? "",
    webhookToken: process.env.REMEDIATION_WEBHOOK_TOKEN ?? "",
    timeoutMinutes: Number.parseInt(
      process.env.REMEDIATION_TIMEOUT_MINUTES ?? "25",
      10,
    ),
    dryRun: process.env.REMEDIATION_DRY_RUN === "1",
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
      case "--run-id":
        args.runId = next ?? "";
        i += 1;
        break;
      case "--webhook-url":
        args.webhookUrl = next ?? "";
        i += 1;
        break;
      case "--webhook-token":
        args.webhookToken = next ?? "";
        i += 1;
        break;
      case "--timeout-minutes":
        args.timeoutMinutes = Number.parseInt(next ?? "25", 10);
        i += 1;
        break;
      case "--dry-run":
        args.dryRun = true;
        break;
      default:
        break;
    }
  }

  return args;
}

export function normalizeTimeoutMinutes(value, fallback = 25) {
  if (!Number.isInteger(value) || value <= 0 || value > 120) return fallback;
  return value;
}

function assertArg(value, message) {
  if (!value) throw new Error(message);
}

async function writeReport(report) {
  const outputDir = path.resolve(REPO_ROOT, "output", "test-metrics");
  await mkdir(outputDir, { recursive: true });
  await writeFile(
    path.join(outputDir, "review-remediation-dispatch.json"),
    `${JSON.stringify(report, null, 2)}\n`,
    "utf8",
  );
}

function shouldBlockByPath(changedFiles, blockedPaths) {
  return changedFiles.some((file) => pathMatchesAnyGlob(file, blockedPaths));
}

async function postWebhook({ url, token, payload, timeoutMinutes }) {
  const controller = new AbortController();
  const timeoutHandle = setTimeout(
    () => controller.abort(),
    timeoutMinutes * 60_000,
  );

  try {
    const response = await fetch(url, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify(payload),
      signal: controller.signal,
    });
    const text = await response.text();
    if (!response.ok) {
      throw new Error(
        `Remediation webhook failed (${response.status}): ${text}`,
      );
    }
    let parsed = null;
    try {
      parsed = JSON.parse(text);
    } catch {
      parsed = { raw: text };
    }
    return parsed;
  } finally {
    clearTimeout(timeoutHandle);
  }
}

export async function executeRemediationDispatch(args) {
  assertArg(args.owner, "Missing owner.");
  assertArg(args.repo, "Missing repo.");
  assertArg(args.token, "Missing token.");
  if (!Number.isInteger(args.pullNumber) || args.pullNumber <= 0) {
    const report = { status: "skipped", reason: "no-pull-request" };
    await writeReport(report);
    return report;
  }

  const contract = await loadRiskContract(args.contractPath);
  const policy = contract.remediationAgent ?? {};
  if ((policy.enabled ?? true) !== true) {
    const report = { status: "skipped", reason: "disabled" };
    await writeReport(report);
    return report;
  }

  const pr = await getPullRequest({
    token: args.token,
    owner: args.owner,
    repo: args.repo,
    pullNumber: args.pullNumber,
  });
  const sameRepo =
    String(pr.head?.repo?.full_name ?? "").toLowerCase() ===
    String(pr.base?.repo?.full_name ?? "").toLowerCase();
  if (!sameRepo) {
    const report = { status: "skipped", reason: "fork-pr" };
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
  assertArg(headSha, "Missing head SHA.");

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

  const requiredTiers = policy.requiredTiers ?? ["high"];
  if (!requiredTiers.includes(riskTier)) {
    const report = { status: "skipped", reason: "tier-not-required", riskTier };
    await writeReport(report);
    return report;
  }

  const blockedPaths = policy.blockedPaths ?? [];
  if (
    blockedPaths.length > 0 &&
    shouldBlockByPath(changedFiles, blockedPaths)
  ) {
    const report = {
      status: "skipped",
      reason: "blocked-path-touched",
      riskTier,
    };
    await writeReport(report);
    return report;
  }

  const findingsResult = await executeFindingsNormalize({
    owner: args.owner,
    repo: args.repo,
    pullNumber: args.pullNumber,
    token: args.token,
    headSha,
    maxFindings: Number.isInteger(policy.maxFindingsPerRun)
      ? policy.maxFindingsPerRun
      : 5,
  });
  const findings = findingsResult.findings ?? [];
  if (findings.length === 0) {
    const report = {
      status: "no-op",
      reason: "no-actionable-findings",
      riskTier,
      headSha,
    };
    await writeReport(report);
    return report;
  }

  const webhookUrl = args.webhookUrl;
  const webhookToken = args.webhookToken;
  if (!webhookUrl || !webhookToken) {
    const report = {
      status: "skipped",
      reason: "missing-webhook-secrets",
      findingsCount: findings.length,
      riskTier,
    };
    await writeReport(report);
    return report;
  }

  const payload = {
    owner: args.owner,
    repo: args.repo,
    pullNumber: args.pullNumber,
    headSha,
    runId: args.runId,
    findings,
    changedFiles,
    riskTier,
  };

  const timeoutMinutes = normalizeTimeoutMinutes(args.timeoutMinutes, 25);

  let responsePayload = null;
  if (!args.dryRun) {
    responsePayload = await postWebhook({
      url: webhookUrl,
      token: webhookToken,
      payload,
      timeoutMinutes,
    });
  }

  const result = {
    status: args.dryRun ? "dry-run-dispatched" : "dispatched",
    pullNumber: args.pullNumber,
    headSha,
    findingsCount: findings.length,
    riskTier,
    response: responsePayload,
  };
  await writeReport(result);

  const summaryBody = args.dryRun
    ? `Remediation dry-run: ${findings.length} actionable finding(s) detected for \`${headSha}\`.`
    : `Remediation dispatched for ${findings.length} actionable finding(s) on \`${headSha}\`.`;
  await createIssueComment({
    token: args.token,
    owner: args.owner,
    repo: args.repo,
    issueNumber: args.pullNumber,
    body: summaryBody,
  });
  return result;
}

async function main() {
  const args = parseArgs(process.argv);
  const result = await executeRemediationDispatch(args);
  console.log(
    `[review-remediation-dispatch] status=${result.status} findings=${result.findingsCount ?? 0}`,
  );
}

if (
  import.meta.url === pathToFileURL(path.resolve(process.argv[1] ?? "")).href
) {
  main().catch((error) => {
    console.error(`[review-remediation-dispatch] Failed: ${error.message}`);
    process.exit(1);
  });
}
