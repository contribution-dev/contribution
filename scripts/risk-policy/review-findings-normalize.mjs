#!/usr/bin/env node

import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { REPO_ROOT } from "./lib.mjs";
import { listIssueComments } from "./github-client.mjs";
import { consumeCommonGithubArg } from "./shared/common-github-args.mjs";
import { reviewSeverityRank } from "../lib/review-severity.mjs";

const FINDING_HEADER_REGEX =
  /^### \[(?<findingId>[^\]]+)\] \[(?<severity>[A-Z]+)\] (?<title>.+?) \(confidence (?<confidence>[0-9.]+)\)/gm;
const EVIDENCE_FILE_REGEX = /`(?<file>[^`:\n]+):(?<lines>[^`\n]+)`/;
const DEFAULT_MAX_FINDINGS = 5;
const MAX_FINDINGS_LIMIT = 50;

function parseCommitLine(body) {
  const match = body.match(/- Commit: `(?<sha>[0-9a-f]{7,40})`/i);
  return match?.groups?.sha ?? null;
}

export function isSameCommitSha(commitSha, headSha) {
  if (!commitSha || !headSha) return false;
  const normalizedCommit = String(commitSha).toLowerCase();
  const normalizedHead = String(headSha).toLowerCase();
  return (
    normalizedCommit === normalizedHead ||
    normalizedCommit.startsWith(normalizedHead) ||
    normalizedHead.startsWith(normalizedCommit)
  );
}

function severityRank(severity) {
  return reviewSeverityRank(severity);
}

export function normalizeMaxFindings(value) {
  const parsed =
    typeof value === "number"
      ? value
      : Number.parseInt(String(value ?? ""), 10);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return DEFAULT_MAX_FINDINGS;
  }
  return Math.min(Math.floor(parsed), MAX_FINDINGS_LIMIT);
}

export function parseFindingsFromCommentBody(body) {
  const findings = [];
  for (const match of body.matchAll(FINDING_HEADER_REGEX)) {
    const severityRaw = String(match.groups?.severity ?? "").toLowerCase();
    const confidence = Number.parseFloat(match.groups?.confidence ?? "0");
    const start = match.index ?? 0;
    const remainder = body.slice(start, start + 3000);
    const evidenceMatch = remainder.match(EVIDENCE_FILE_REGEX);
    findings.push({
      findingId: String(match.groups?.findingId ?? "").trim(),
      severity: severityRaw,
      confidence: Number.isFinite(confidence) ? confidence : 0,
      title: String(match.groups?.title ?? "").trim(),
      file: evidenceMatch?.groups?.file ?? null,
      lines: evidenceMatch?.groups?.lines ?? null,
      rationale: remainder.slice(0, 1200).trim(),
    });
  }
  return findings;
}

export function normalizeActionableFindings({
  comments,
  headSha,
  maxFindings,
}) {
  const limit = normalizeMaxFindings(maxFindings);
  const dedup = new Map();
  for (const comment of comments) {
    const body = String(comment?.body ?? "");
    const commitSha = parseCommitLine(body);
    if (!isSameCommitSha(commitSha, headSha)) continue;

    const parsed = parseFindingsFromCommentBody(body);
    for (const finding of parsed) {
      if (!["blocker", "major"].includes(finding.severity)) continue;
      if (!finding.findingId) continue;
      if (!dedup.has(finding.findingId)) {
        dedup.set(finding.findingId, finding);
      }
    }
  }

  return [...dedup.values()]
    .sort((a, b) => {
      const severityDelta = severityRank(b.severity) - severityRank(a.severity);
      if (severityDelta !== 0) return severityDelta;
      return b.confidence - a.confidence;
    })
    .slice(0, limit);
}

async function writeReport(report) {
  const outputDir = path.resolve(REPO_ROOT, "output", "test-metrics");
  await mkdir(outputDir, { recursive: true });
  await writeFile(
    path.join(outputDir, "review-findings-normalized.json"),
    `${JSON.stringify(report, null, 2)}\n`,
    "utf8",
  );
}

function parseArgs(argv) {
  const args = {
    owner: process.env.GITHUB_REPOSITORY_OWNER ?? "",
    repo: process.env.GITHUB_REPOSITORY?.split("/")[1] ?? "",
    pullNumber: Number.parseInt(process.env.PR_NUMBER ?? "0", 10),
    token: process.env.GITHUB_TOKEN ?? "",
    headSha: process.env.PR_HEAD_SHA ?? "",
    maxFindings: normalizeMaxFindings(process.env.MAX_FINDINGS),
  };
  for (let i = 2; i < argv.length; i += 1) {
    const arg = argv[i];
    const next = argv[i + 1];
    const consumed = consumeCommonGithubArg(args, arg, next);
    if (consumed > 0) {
      i += consumed;
      continue;
    }
    switch (arg) {
      case "--max-findings":
        args.maxFindings = normalizeMaxFindings(next);
        i += 1;
        break;
      default:
        break;
    }
  }
  return args;
}

export async function executeFindingsNormalize(args) {
  if (
    !args.owner ||
    !args.repo ||
    !args.token ||
    !args.pullNumber ||
    !args.headSha
  ) {
    const report = {
      status: "skipped",
      reason: "missing-inputs",
      findings: [],
    };
    await writeReport(report);
    return report;
  }
  const comments = await listIssueComments({
    token: args.token,
    owner: args.owner,
    repo: args.repo,
    issueNumber: args.pullNumber,
  });

  const findings = normalizeActionableFindings({
    comments,
    headSha: args.headSha,
    maxFindings: args.maxFindings,
  });
  const report = {
    status: findings.length > 0 ? "ok" : "no-findings",
    pullNumber: args.pullNumber,
    headSha: args.headSha,
    findings,
  };
  await writeReport(report);
  return report;
}

async function main() {
  const args = parseArgs(process.argv);
  const result = await executeFindingsNormalize(args);
  console.log(JSON.stringify(result, null, 2));
}

if (
  import.meta.url === pathToFileURL(path.resolve(process.argv[1] ?? "")).href
) {
  main().catch((error) => {
    console.error(`[review-findings-normalize] Failed: ${error.message}`);
    process.exit(1);
  });
}
