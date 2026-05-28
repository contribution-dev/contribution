#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { readFile, readdir, rm, stat } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import {
  isActionableReport,
  resolveReviewsDir,
  updateAckState,
} from "./codex-review-inbox-lib.mjs";
import {
  reportSatisfiesCanonicalCodexLane,
  reportSatisfiesUiRuntime,
} from "./lib/codex-review-state.mjs";
import {
  loadReviewedShas,
  saveReviewedShas,
} from "./codex-review-push-gate-lib.mjs";

const SHA_FILE_PATTERN = /^([0-9a-f]{40})\.(json|md)$/i;
const UI_RUNTIME_FILE_PATTERN = /^([0-9a-f]{40})\.ui-runtime\.(json|md)$/i;
const DEFAULT_REPORT_RETENTION_DAYS = 1;
const DEFAULT_LOG_RETENTION_DAYS = 1;
const DEFAULT_QUEUE_STALE_MS = 10 * 60 * 1000;

function usage() {
  console.log(`Usage: node scripts/codex-review-prune-artifacts.mjs [options]

Options:
  --repo-root <path>             Repository root (default: cwd)
  --reviews-dir <path>           Override reviews directory
  --sha-file <path>              Immediately prune successful artifacts for SHAs listed in the file
  --remote-visible-successes     Immediately prune successful artifacts for SHAs reachable from remotes
  --report-retention-days <n>    Retain non-actionable canonical reports for n days (default: ${DEFAULT_REPORT_RETENTION_DAYS})
  --log-retention-days <n>       Retain review logs for n days (default: ${DEFAULT_LOG_RETENTION_DAYS})
  --dry-run                      Show what would be deleted without removing files
  -h, --help                     Show help
`);
}

function parseNonNegativeInt(value, flagName) {
  const parsed = Number.parseInt(String(value ?? "").trim(), 10);
  if (!Number.isInteger(parsed) || parsed < 0) {
    throw new Error(`Invalid ${flagName}: ${value}`);
  }
  return parsed;
}

function parseArgs(argv) {
  const args = {
    repoRoot: process.cwd(),
    reviewsDir: "",
    shaFile: "",
    remoteVisibleSuccesses: false,
    reportRetentionDays: parseNonNegativeInt(
      process.env.CODEX_REVIEW_REPORT_RETENTION_DAYS ??
        process.env.CODEX_REVIEW_RETENTION_DAYS ??
        DEFAULT_REPORT_RETENTION_DAYS,
      "CODEX_REVIEW_REPORT_RETENTION_DAYS",
    ),
    logRetentionDays: parseNonNegativeInt(
      process.env.CODEX_REVIEW_LOG_RETENTION_DAYS ?? DEFAULT_LOG_RETENTION_DAYS,
      "CODEX_REVIEW_LOG_RETENTION_DAYS",
    ),
    dryRun: false,
  };

  for (let i = 2; i < argv.length; i += 1) {
    const arg = argv[i];
    const next = argv[i + 1];
    switch (arg) {
      case "--repo-root":
        args.repoRoot = String(next ?? "").trim() || process.cwd();
        i += 1;
        break;
      case "--reviews-dir":
        args.reviewsDir = String(next ?? "").trim();
        i += 1;
        break;
      case "--sha-file":
        args.shaFile = String(next ?? "").trim();
        i += 1;
        break;
      case "--remote-visible-successes":
        args.remoteVisibleSuccesses = true;
        break;
      case "--report-retention-days":
        args.reportRetentionDays = parseNonNegativeInt(
          next,
          "--report-retention-days",
        );
        i += 1;
        break;
      case "--log-retention-days":
        args.logRetentionDays = parseNonNegativeInt(
          next,
          "--log-retention-days",
        );
        i += 1;
        break;
      case "--dry-run":
        args.dryRun = true;
        break;
      case "-h":
      case "--help":
        usage();
        process.exit(0);
        break;
      default:
        throw new Error(`Unknown argument: ${arg}`);
    }
  }

  return args;
}

function isSha(value) {
  return /^[0-9a-f]{40}$/i.test(String(value ?? "").trim());
}

function shaFromArtifactName(name) {
  const match = SHA_FILE_PATTERN.exec(String(name ?? ""));
  return match ? match[1].toLowerCase() : "";
}

function uiRuntimeShaFromArtifactName(name) {
  const match = UI_RUNTIME_FILE_PATTERN.exec(String(name ?? ""));
  return match ? match[1].toLowerCase() : "";
}

function toAgeMs(info, nowMs) {
  if (!info || !Number.isFinite(info.mtimeMs)) return 0;
  return Math.max(0, nowMs - info.mtimeMs);
}

function toRecordedAgeMs(timestamp, info, nowMs) {
  const recordedMs = Date.parse(String(timestamp ?? "").trim());
  if (Number.isFinite(recordedMs)) {
    return Math.max(0, nowMs - recordedMs);
  }
  return toAgeMs(info, nowMs);
}

function safeGitLines(repoRoot, args) {
  try {
    return String(
      execFileSync("git", args, {
        cwd: repoRoot,
        encoding: "utf8",
        stdio: ["ignore", "pipe", "ignore"],
      }) ?? "",
    )
      .split(/\r?\n/)
      .map((line) => line.trim())
      .filter(Boolean);
  } catch {
    return [];
  }
}

function collectProtectedTipShas(repoRoot) {
  const protectedShas = new Set();
  for (const sha of safeGitLines(repoRoot, [
    "for-each-ref",
    "--format=%(objectname)",
    "refs/heads",
    "refs/remotes",
  ])) {
    if (isSha(sha)) {
      protectedShas.add(sha.toLowerCase());
    }
  }

  const [headSha] = safeGitLines(repoRoot, ["rev-parse", "HEAD"]);
  if (isSha(headSha)) {
    protectedShas.add(headSha.toLowerCase());
  }

  return protectedShas;
}

async function collectQueuedShas(reviewsDir) {
  const queued = new Set();
  const dirPaths = [
    path.join(reviewsDir, "queue", "codex", "pending"),
    path.join(reviewsDir, "queue", "codex", "active"),
    path.join(reviewsDir, "queue", "pending"),
    path.join(reviewsDir, "queue", "active"),
  ];
  for (const dirPath of dirPaths) {
    const entries = await readdir(dirPath, { withFileTypes: true }).catch(
      () => [],
    );
    for (const entry of entries) {
      if (!entry.isFile()) continue;
      const sha = shaFromArtifactName(entry.name);
      if (sha) {
        queued.add(sha);
      }
    }
  }
  return queued;
}

async function readShaFile(shaFile) {
  if (!shaFile) return new Set();
  const raw = await readFile(shaFile, "utf8");
  return new Set(
    raw
      .split(/\r?\n/)
      .map((line) => line.trim().toLowerCase())
      .filter((line) => isSha(line)),
  );
}

function collectRemoteVisibleShas(repoRoot) {
  return new Set(
    safeGitLines(repoRoot, ["rev-list", "--remotes"])
      .filter((sha) => isSha(sha))
      .map((sha) => sha.toLowerCase()),
  );
}

async function readJsonIfPresent(filePath) {
  try {
    return JSON.parse(await readFile(filePath, "utf8"));
  } catch {
    return null;
  }
}

function reviewedAtForReport(report) {
  const reviewedAt = String(report?.last_reviewed ?? "").trim();
  return {
    reviewedAt,
    reviewedAtMs: Number.isFinite(Date.parse(reviewedAt))
      ? Date.parse(reviewedAt)
      : 0,
  };
}

async function collectSuccessfulTargetShas({
  repoRoot,
  reviewsDir,
  candidateShas,
}) {
  const reviewed = await loadReviewedShas({ repoRoot });
  const queued = await collectQueuedShas(reviewsDir);
  const successful = {
    codex: new Set(),
    uiRuntime: new Set(),
  };
  const reviewedLaneStatusOverrides = {
    codex: new Map(),
  };

  for (const sha of candidateShas) {
    const [codexReport, uiRuntimeReport] = await Promise.all([
      readJsonIfPresent(path.join(reviewsDir, `${sha}.json`)),
      readJsonIfPresent(path.join(reviewsDir, `${sha}.ui-runtime.json`)),
    ]);

    const hasCurrentCodexArtifact = Boolean(codexReport);
    const hasCurrentUiRuntimeArtifact = Boolean(uiRuntimeReport);
    const hasQueuedCodexReview = queued.has(sha);
    const codexSatisfied = reportSatisfiesCanonicalCodexLane(codexReport);

    if (codexSatisfied === true) {
      successful.codex.add(sha);
    } else if (
      !hasCurrentCodexArtifact &&
      !hasQueuedCodexReview &&
      (reviewed.clean.has(sha) || reviewed.lanes?.codex?.clean?.has(sha))
    ) {
      successful.codex.add(sha);
    } else if (
      hasCurrentCodexArtifact &&
      reviewed.lanes?.codex?.clean?.has(sha)
    ) {
      reviewedLaneStatusOverrides.codex.set(sha, "reviewed");
    }

    if (reportSatisfiesUiRuntime(uiRuntimeReport)) {
      successful.uiRuntime.add(sha);
    } else if (
      !hasCurrentUiRuntimeArtifact &&
      reviewed.lanes?.["ui-runtime"]?.clean?.has(sha)
    ) {
      successful.uiRuntime.add(sha);
    }
  }

  return {
    successful,
    reviewedLaneStatusOverrides,
  };
}

async function removeAckEntries(ackPath, deletedShas, dryRun) {
  if (deletedShas.size === 0) return 0;
  if (dryRun) {
    let removed = 0;
    await updateAckState({
      ackPath,
      logger: console,
      mutate: async (ackState) => {
        for (const sha of deletedShas) {
          if (!Object.prototype.hasOwnProperty.call(ackState.acks, sha))
            continue;
          removed += 1;
        }
        return false;
      },
    });
    return removed;
  }

  let removed = 0;
  await updateAckState({
    ackPath,
    logger: console,
    mutate: async (ackState) => {
      let changed = false;
      for (const sha of deletedShas) {
        if (!Object.prototype.hasOwnProperty.call(ackState.acks, sha)) continue;
        delete ackState.acks[sha];
        removed += 1;
        changed = true;
      }
      return changed;
    },
  });
  return removed;
}

async function collectLaneReportPrunePlan({
  reportsDir,
  retentionMs,
  nowMs,
  protectedShas,
  targetShas = null,
  lane = "codex",
  artifactType = "lane",
}) {
  const entries = await readdir(reportsDir, { withFileTypes: true }).catch(
    () => [],
  );
  const plan = [];
  let keptActionable = 0;

  for (const entry of entries) {
    if (
      !entry.isFile() ||
      !entry.name.endsWith(".json") ||
      entry.name === ".ack.json"
    ) {
      continue;
    }
    const sha =
      artifactType === "ui-runtime"
        ? uiRuntimeShaFromArtifactName(entry.name)
        : shaFromArtifactName(entry.name);
    if (!sha) continue;

    const jsonPath = path.join(reportsDir, entry.name);
    if (targetShas) {
      if (!targetShas.has(sha)) continue;
    }

    let parsed;
    try {
      parsed = JSON.parse(await readFile(jsonPath, "utf8"));
    } catch {
      continue;
    }

    if (!targetShas) {
      const info = await stat(jsonPath).catch(() => null);
      if (toRecordedAgeMs(parsed?.last_reviewed, info, nowMs) < retentionMs)
        continue;
      if (protectedShas.has(sha)) continue;
    }

    if (!targetShas && isActionableReport(parsed, true)) {
      keptActionable += 1;
      continue;
    }
    const mdPath =
      artifactType === "ui-runtime"
        ? path.join(reportsDir, `${sha}.ui-runtime.md`)
        : path.join(reportsDir, `${sha}.md`);
    const mdInfo = await stat(mdPath).catch(() => null);
    plan.push({
      sha: artifactType === "ui-runtime" ? `${sha}.ui-runtime` : sha,
      targetSha: sha,
      jsonPath,
      mdPath,
      mdExists: mdInfo?.isFile() === true,
      branchName:
        artifactType === "ui-runtime"
          ? String(parsed?.review_target?.branch ?? "").trim()
          : "",
      ...reviewedAtForReport(parsed),
      successful:
        artifactType === "ui-runtime"
          ? reportSatisfiesUiRuntime(parsed)
          : reportSatisfiesCanonicalCodexLane(parsed),
    });
  }

  return {
    plan,
    keptActionable,
  };
}

async function applyLaneReportPrunePlan({ ackPath, dryRun, plan }) {
  const deletedShas = new Set();
  const successfulDeletedShas = new Set();
  let deletedJson = 0;
  let deletedMd = 0;

  for (const entry of plan) {
    if (!dryRun) {
      await rm(entry.jsonPath, { force: true });
      await rm(entry.mdPath, { force: true });
    }
    deletedShas.add(entry.sha);
    deletedJson += 1;
    if (entry.mdExists) {
      deletedMd += 1;
    }
    if (entry.successful) {
      successfulDeletedShas.add(entry.sha);
    }
  }

  const ackEntriesRemoved = await removeAckEntries(
    ackPath,
    deletedShas,
    dryRun,
  );
  return {
    deletedShas,
    successfulDeletedShas,
    deletedJson,
    deletedMd,
    ackEntriesRemoved,
  };
}

function mergeReportPlans(...plans) {
  const merged = [];
  const seenJsonPaths = new Set();
  let keptActionable = 0;
  for (const input of plans) {
    if (!input) continue;
    keptActionable += Number(input.keptActionable ?? 0) || 0;
    for (const entry of input.plan ?? []) {
      if (seenJsonPaths.has(entry.jsonPath)) continue;
      seenJsonPaths.add(entry.jsonPath);
      merged.push(entry);
    }
  }
  return { plan: merged, keptActionable };
}

function mergeFilePlans(...plans) {
  const merged = [];
  const seenPaths = new Set();
  for (const input of plans) {
    if (!input) continue;
    for (const entry of input.plan ?? []) {
      if (seenPaths.has(entry.filePath)) continue;
      seenPaths.add(entry.filePath);
      merged.push(entry);
    }
  }
  return { plan: merged };
}

async function pruneTargetQueueJobs({
  reviewsDir,
  dryRun,
  targetShas = null,
  lane = "codex",
}) {
  if (!targetShas || targetShas.size === 0) {
    return 0;
  }
  let deleted = 0;
  for (const status of ["pending", "active"]) {
    const dirPath = path.join(reviewsDir, "queue", lane, status);
    const entries = await readdir(dirPath, { withFileTypes: true }).catch(
      () => [],
    );
    for (const entry of entries) {
      if (!entry.isFile()) continue;
      const sha = shaFromArtifactName(entry.name);
      if (!sha || !targetShas.has(sha)) continue;
      const filePath = path.join(dirPath, entry.name);
      if (!dryRun) {
        await rm(filePath, { force: true });
      }
      deleted += 1;
    }
  }
  return deleted;
}

async function pruneReviewLogs({
  reviewsDir,
  retentionMs,
  nowMs,
  dryRun,
  targetShas = null,
  liveQueueShas = new Set(),
}) {
  const logsDir = path.join(reviewsDir, "logs");
  let deletedLogFiles = 0;

  async function pruneDir(dirPath) {
    const entries = await readdir(dirPath, { withFileTypes: true }).catch(
      () => [],
    );
    for (const entry of entries) {
      const fullPath = path.join(dirPath, entry.name);
      if (entry.isDirectory()) {
        await pruneDir(fullPath);
        continue;
      }
      if (!entry.isFile()) continue;

      const isLogFile = entry.name.endsWith(".log");
      if (!isLogFile) continue;

      const info = await stat(fullPath).catch(() => null);
      if (toAgeMs(info, nowMs) < retentionMs) {
        continue;
      }

      if (!dryRun) {
        await rm(fullPath, { force: true });
      }
      deletedLogFiles += 1;
    }
  }

  await pruneDir(logsDir);

  async function removeEmptyDirs(dirPath, stopPath) {
    const entries = await readdir(dirPath, { withFileTypes: true }).catch(
      () => [],
    );
    for (const entry of entries) {
      if (!entry.isDirectory()) continue;
      await removeEmptyDirs(path.join(dirPath, entry.name), stopPath);
    }

    if (path.resolve(dirPath) === path.resolve(stopPath)) {
      return;
    }

    const remaining = await readdir(dirPath).catch(() => []);
    if (remaining.length === 0 && !dryRun) {
      await rm(dirPath, { recursive: true, force: true });
    }
  }

  await removeEmptyDirs(logsDir, logsDir);

  return { deletedLogFiles, deletedShaArtifacts: 0 };
}

async function pruneLegacyLocalArtifacts({
  reviewsDir,
  dryRun,
  targetShas = null,
  nowMs = Date.now(),
}) {
  let deletedFiles = 0;
  const protectedLocalShas = new Set();
  const queueRoot = path.join(reviewsDir, "queue", "local");
  const localReportsDir = path.join(reviewsDir, "local");

  const collectProtectedLocalReports = async () => {
    const entries = await readdir(localReportsDir, {
      withFileTypes: true,
    }).catch(() => []);
    for (const entry of entries) {
      if (!entry.isFile() || !entry.name.endsWith(".json")) continue;
      const sha = shaFromArtifactName(entry.name);
      if (!sha) continue;
      const report = await readJsonIfPresent(
        path.join(localReportsDir, entry.name),
      );
      if (isActionableReport(report, true)) {
        protectedLocalShas.add(sha);
      }
    }
  };
  const collectProtectedLocalQueue = async () => {
    for (const status of ["pending", "active"]) {
      const dirPath = path.join(queueRoot, status);
      const entries = await readdir(dirPath, { withFileTypes: true }).catch(
        () => [],
      );
      for (const entry of entries) {
        if (!entry.isFile()) continue;
        const sha = shaFromArtifactName(entry.name);
        if (!sha) {
          continue;
        }
        const job = await readJsonIfPresent(path.join(dirPath, entry.name));
        const heartbeatRaw =
          job?.worker?.heartbeat_at ||
          job?.updated_at ||
          job?.started_at ||
          job?.enqueued_at ||
          job?.created_at;
        const heartbeatMs = Date.parse(String(heartbeatRaw ?? ""));
        const isFresh =
          status === "active"
            ? Number.isFinite(heartbeatMs) &&
              nowMs - heartbeatMs <= DEFAULT_QUEUE_STALE_MS
            : Number.isFinite(heartbeatMs) &&
              nowMs - heartbeatMs <= DEFAULT_QUEUE_STALE_MS;
        if (isFresh) {
          protectedLocalShas.add(sha);
        }
      }
    }
  };
  const localLaneDrained = async () => {
    if (protectedLocalShas.size > 0) {
      return false;
    }
    for (const dirPath of [
      localReportsDir,
      path.join(queueRoot, "pending"),
      path.join(queueRoot, "active"),
    ]) {
      const entries = await readdir(dirPath, { withFileTypes: true }).catch(
        () => [],
      );
      if (entries.some((entry) => entry.isFile())) {
        return false;
      }
    }
    return true;
  };

  await collectProtectedLocalReports();
  await collectProtectedLocalQueue();

  const deleteIfMatch = async (dirPath, fileNameToSha) => {
    const entries = await readdir(dirPath, { withFileTypes: true }).catch(
      () => [],
    );
    for (const entry of entries) {
      if (!entry.isFile()) continue;
      const fullPath = path.join(dirPath, entry.name);
      const sha = fileNameToSha(entry.name);
      if (targetShas && (!sha || !targetShas.has(sha))) {
        continue;
      }
      if (sha && protectedLocalShas.has(sha)) {
        continue;
      }
      if (!dryRun) {
        await rm(fullPath, { force: true });
      }
      deletedFiles += 1;
    }
  };

  await deleteIfMatch(localReportsDir, (name) => shaFromArtifactName(name));
  await deleteIfMatch(path.join(reviewsDir, "local-queue", "entries"), (name) =>
    shaFromArtifactName(name),
  );
  for (const status of ["pending", "active", "completed", "failed"]) {
    await deleteIfMatch(path.join(queueRoot, status), (name) =>
      shaFromArtifactName(name),
    );
  }

  const laneDrained = await localLaneDrained();
  if (laneDrained) {
    for (const legacyFile of [
      path.join(reviewsDir, "local-model-runtime.json"),
      path.join(reviewsDir, "local-runtime-events.jsonl"),
    ]) {
      const info = await stat(legacyFile).catch(() => null);
      if (!info?.isFile()) continue;
      if (!dryRun) {
        await rm(legacyFile, { force: true });
      }
      deletedFiles += 1;
    }
  }

  if (!dryRun && laneDrained) {
    await rm(path.join(reviewsDir, "logs", "local"), {
      recursive: true,
      force: true,
    }).catch(() => {});
    await rm(path.join(reviewsDir, "local-queue"), {
      recursive: true,
      force: true,
    }).catch(() => {});
    await rm(path.join(reviewsDir, "queue", "local"), {
      recursive: true,
      force: true,
    }).catch(() => {});
    const localEntries = await readdir(path.join(reviewsDir, "local")).catch(
      () => [],
    );
    if (localEntries.length === 0) {
      await rm(path.join(reviewsDir, "local"), {
        recursive: true,
        force: true,
      }).catch(() => {});
    }
  }

  return deletedFiles;
}

export async function pruneReviewArtifacts({
  repoRoot = process.cwd(),
  reviewsDir = "",
  shaFile = "",
  remoteVisibleSuccesses = false,
  reportRetentionDays = DEFAULT_REPORT_RETENTION_DAYS,
  logRetentionDays = DEFAULT_LOG_RETENTION_DAYS,
  nowMs = Date.now(),
  dryRun = false,
  saveReviewedShasFn = saveReviewedShas,
} = {}) {
  const resolvedReviewsDir = await resolveReviewsDir(repoRoot, reviewsDir);
  const reportRetentionMs = reportRetentionDays * 24 * 60 * 60 * 1000;
  const logRetentionMs = logRetentionDays * 24 * 60 * 60 * 1000;

  const protectedTipShas = collectProtectedTipShas(repoRoot);
  const liveQueueShas = await collectQueuedShas(resolvedReviewsDir);
  const protectedShas = new Set(protectedTipShas);
  for (const sha of liveQueueShas) {
    protectedShas.add(sha);
  }
  const targetShas = new Set(await readShaFile(shaFile));
  if (remoteVisibleSuccesses) {
    for (const sha of collectRemoteVisibleShas(repoRoot)) {
      targetShas.add(sha);
    }
  }
  const { successful: successfulTargetShas, reviewedLaneStatusOverrides } =
    targetShas.size > 0
      ? await collectSuccessfulTargetShas({
          repoRoot,
          reviewsDir: resolvedReviewsDir,
          candidateShas: targetShas,
        })
      : {
          successful: { codex: new Set(), uiRuntime: new Set() },
          reviewedLaneStatusOverrides: {
            codex: new Map(),
          },
        };
  const effectiveCodexTargetShas =
    successfulTargetShas.codex.size > 0 ? successfulTargetShas.codex : null;
  const effectiveUiRuntimeTargetShas =
    successfulTargetShas.uiRuntime.size > 0
      ? successfulTargetShas.uiRuntime
      : null;

  const codexRetentionReportPlan = await collectLaneReportPrunePlan({
    reportsDir: resolvedReviewsDir,
    retentionMs: reportRetentionMs,
    nowMs,
    protectedShas,
    lane: "codex",
  });
  const codexTargetedReportPlan = effectiveCodexTargetShas
    ? await collectLaneReportPrunePlan({
        reportsDir: resolvedReviewsDir,
        retentionMs: reportRetentionMs,
        nowMs,
        protectedShas,
        targetShas: effectiveCodexTargetShas,
        lane: "codex",
      })
    : null;
  const codexReportPlan = mergeReportPlans(
    codexRetentionReportPlan,
    codexTargetedReportPlan,
  );
  const uiRuntimeRetentionReportPlan = await collectLaneReportPrunePlan({
    reportsDir: resolvedReviewsDir,
    retentionMs: reportRetentionMs,
    nowMs,
    protectedShas,
    artifactType: "ui-runtime",
  });
  const uiRuntimeTargetedReportPlan = effectiveUiRuntimeTargetShas
    ? await collectLaneReportPrunePlan({
        reportsDir: resolvedReviewsDir,
        retentionMs: reportRetentionMs,
        nowMs,
        protectedShas,
        targetShas: effectiveUiRuntimeTargetShas,
        artifactType: "ui-runtime",
      })
    : null;
  const uiRuntimeReportPlan = mergeReportPlans(
    uiRuntimeRetentionReportPlan,
    uiRuntimeTargetedReportPlan,
  );

  const reviewedCodexCleanShas = new Set([
    ...codexReportPlan.plan
      .filter((entry) => entry.successful)
      .map((entry) => entry.sha),
  ]);
  const reviewedCodexReviewedAt = new Map(
    codexReportPlan.plan
      .filter((entry) => entry.successful && entry.reviewedAt)
      .map((entry) => [entry.sha, entry.reviewedAt]),
  );
  const reviewedUiRuntimeCleanShas = new Set(
    uiRuntimeReportPlan.plan
      .filter((entry) => entry.successful)
      .map((entry) => entry.targetSha),
  );
  const reviewedUiRuntimeBranchCheckpoints = new Map();
  for (const entry of uiRuntimeReportPlan.plan) {
    if (!entry.successful || !entry.branchName) continue;
    const existing = reviewedUiRuntimeBranchCheckpoints.get(entry.branchName);
    if (
      existing &&
      (existing.reviewedAtMs > entry.reviewedAtMs ||
        (existing.reviewedAtMs === entry.reviewedAtMs &&
          String(existing.sha ?? "").localeCompare(entry.targetSha) >= 0))
    ) {
      continue;
    }
    reviewedUiRuntimeBranchCheckpoints.set(entry.branchName, {
      status: "clean",
      sha: entry.targetSha,
      reviewedAt: entry.reviewedAt,
      reviewedAtMs: entry.reviewedAtMs,
    });
  }
  if (
    !dryRun &&
    (reviewedCodexCleanShas.size > 0 ||
      reviewedUiRuntimeCleanShas.size > 0 ||
      reviewedLaneStatusOverrides.codex.size > 0 ||
      reviewedUiRuntimeBranchCheckpoints.size > 0)
  ) {
    await saveReviewedShasFn({
      repoRoot,
      reviewedCleanShas: reviewedCodexCleanShas,
      reviewedLaneCleanShas: {
        codex: reviewedCodexCleanShas,
        "ui-runtime": reviewedUiRuntimeCleanShas,
      },
      reviewedLaneStatusOverrides,
      reviewedLaneReviewedAt: {
        codex: reviewedCodexReviewedAt,
      },
      reviewedBranchLaneCheckpoints: {
        "ui-runtime": reviewedUiRuntimeBranchCheckpoints,
      },
    });
  }
  const codexResult = await applyLaneReportPrunePlan({
    ackPath: path.join(resolvedReviewsDir, ".ack.json"),
    dryRun,
    plan: codexReportPlan.plan,
  });
  const uiRuntimeResult = await applyLaneReportPrunePlan({
    ackPath: path.join(resolvedReviewsDir, ".ack.json"),
    dryRun,
    plan: uiRuntimeReportPlan.plan,
  });
  const targetedQueueDeleted = await pruneTargetQueueJobs({
    reviewsDir: resolvedReviewsDir,
    dryRun,
    targetShas: effectiveCodexTargetShas,
    lane: "codex",
  });
  const logResult = await pruneReviewLogs({
    reviewsDir: resolvedReviewsDir,
    retentionMs: logRetentionMs,
    nowMs,
    dryRun,
    targetShas: effectiveCodexTargetShas,
    liveQueueShas,
  });
  const deletedLegacyLocalArtifacts = await pruneLegacyLocalArtifacts({
    reviewsDir: resolvedReviewsDir,
    dryRun,
    targetShas: targetShas.size > 0 ? targetShas : null,
    nowMs,
  });

  return {
    reviewsDir: resolvedReviewsDir,
    deletedReports:
      codexResult.deletedJson +
      codexResult.deletedMd +
      uiRuntimeResult.deletedJson +
      uiRuntimeResult.deletedMd,
    deletedReportShas:
      codexResult.deletedShas.size + uiRuntimeResult.deletedShas.size,
    deletedAckEntries:
      codexResult.ackEntriesRemoved + uiRuntimeResult.ackEntriesRemoved,
    keptActionableReports:
      codexReportPlan.keptActionable + uiRuntimeReportPlan.keptActionable,
    deletedCompletedQueueJobs: 0,
    deletedTargetQueueJobs: targetedQueueDeleted,
    deletedLogFiles: logResult.deletedLogFiles,
    deletedLogShaArtifacts: logResult.deletedShaArtifacts,
    deletedLegacyLocalArtifacts,
    protectedTipShas: protectedTipShas.size,
    protectedQueuedShas: liveQueueShas.size,
    targetedShas: targetShas.size,
    targetedSuccessfulCodexShas: successfulTargetShas.codex.size,
    targetedSuccessfulUiRuntimeShas: successfulTargetShas.uiRuntime.size,
    dryRun,
    reportRetentionDays,
    logRetentionDays,
  };
}

async function main() {
  const args = parseArgs(process.argv);
  const result = await pruneReviewArtifacts({
    repoRoot: args.repoRoot,
    reviewsDir: args.reviewsDir,
    shaFile: args.shaFile,
    remoteVisibleSuccesses: args.remoteVisibleSuccesses,
    reportRetentionDays: args.reportRetentionDays,
    logRetentionDays: args.logRetentionDays,
    dryRun: args.dryRun,
  });

  console.log(
    `[codex-review-prune-artifacts] ${result.dryRun ? "would_prune" : "pruned"} targeted_shas=${result.targetedShas} report_shas=${result.deletedReportShas} report_files=${result.deletedReports} ack_entries=${result.deletedAckEntries} queue_completed=${result.deletedCompletedQueueJobs} queue_other=${result.deletedTargetQueueJobs} log_files=${result.deletedLogFiles} log_sha_artifacts=${result.deletedLogShaArtifacts} legacy_local=${result.deletedLegacyLocalArtifacts} kept_actionable=${result.keptActionableReports} protected_tips=${result.protectedTipShas} protected_queued=${result.protectedQueuedShas} report_retention_days=${result.reportRetentionDays} log_retention_days=${result.logRetentionDays}`,
  );
}

if (
  import.meta.url === pathToFileURL(path.resolve(process.argv[1] ?? "")).href
) {
  main().catch((error) => {
    console.error(`[codex-review-prune-artifacts] Failed: ${error.message}`);
    process.exit(1);
  });
}
