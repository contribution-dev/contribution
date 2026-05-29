#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import crypto from "node:crypto";
import {
  mkdir,
  readdir,
  readFile,
  rename,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import path from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { writeReviewArtifacts } from "./lib/codex-review-findings.mjs";
import { reviewSeverityRank } from "./lib/review-severity.mjs";
import {
  buildStaleSupersessionArchiver,
  collectLiveBacklogArtifacts,
} from "./lib/codex-review-backlog-artifacts.mjs";
import {
  defaultReviewsDir,
  isActionableCodexReviewReport,
  isNonFindingCodexReviewStatusActionable,
  isQueueRecoveryReport,
  legacyReviewsDir,
  normalizeReviewRootOverride,
  resolveRepoRoot,
  resolveReviewRootOverride,
} from "./lib/codex-review-state.mjs";
import {
  queueStateForSha,
  readReviewQueueBacklog,
} from "./codex-review-queue-lib.mjs";
import {
  applyOperatorClosuresToReport,
  readOperatorState,
} from "./codex-review-push-gate-lib.mjs";

export { resolveRepoRoot } from "./lib/codex-review-state.mjs";

const ACK_SCHEMA_VERSION = 1;
const LOCK_OWNER_FILE = "owner.json";
const TOOL_REPO_ROOT = resolveRepoRoot(
  path.dirname(fileURLToPath(import.meta.url)),
);

export function isoNow() {
  return new Date().toISOString();
}

export function laneDirName(lane = "codex") {
  const normalized = String(lane ?? "")
    .trim()
    .toLowerCase();
  if (normalized === "local") return "local";
  if (normalized === "incidents") return "incidents";
  return "";
}

export function reportsDirForLane(reviewsDir, lane = "codex") {
  const suffix = laneDirName(lane);
  return suffix ? path.join(reviewsDir, suffix) : reviewsDir;
}

export function ackPathForLane(reviewsDir, lane = "codex") {
  return path.join(reportsDirForLane(reviewsDir, lane), ".ack.json");
}

export function backlogRemediationAutomationConfigPath(reviewsDir) {
  return path.join(reviewsDir, "backlog-remediation", "automation.json");
}

function normalizeBacklogRemediationAutomationConfig(value) {
  const maxFollowOnChain = Number.parseInt(
    String(value?.max_follow_on_chain ?? value?.maxFollowOnChain ?? "3"),
    10,
  );
  return {
    version: 1,
    enabled: value?.enabled === true,
    paused: value?.paused === true,
    pause_reason: String(
      value?.pause_reason ?? value?.pauseReason ?? "",
    ).trim(),
    last_opened_sha: String(
      value?.last_opened_sha ?? value?.lastOpenedSha ?? "",
    ).trim(),
    updated_at: String(value?.updated_at ?? value?.updatedAt ?? "").trim(),
    max_follow_on_chain:
      Number.isInteger(maxFollowOnChain) && maxFollowOnChain > 0
        ? maxFollowOnChain
        : 3,
  };
}

export async function readBacklogRemediationAutomationConfig(reviewsDir) {
  const lane = laneForReportsDir(reviewsDir);
  const rootReviewsDir = rootReviewsDirForLane(reviewsDir, lane);
  const parsed = await readJsonFile(
    backlogRemediationAutomationConfigPath(rootReviewsDir),
  ).catch(() => null);
  return normalizeBacklogRemediationAutomationConfig(parsed);
}

export async function writeBacklogRemediationAutomationConfig(
  reviewsDir,
  value,
) {
  const lane = laneForReportsDir(reviewsDir);
  const rootReviewsDir = rootReviewsDirForLane(reviewsDir, lane);
  const filePath = backlogRemediationAutomationConfigPath(rootReviewsDir);
  const payload = {
    ...normalizeBacklogRemediationAutomationConfig(value),
    updated_at: isoNow(),
  };
  await mkdir(path.dirname(filePath), { recursive: true, mode: 0o700 });
  const tempPath = `${filePath}.tmp.${process.pid}.${Date.now()}`;
  await writeFile(tempPath, `${JSON.stringify(payload, null, 2)}\n`, {
    encoding: "utf8",
    mode: 0o600,
  });
  await rename(tempPath, filePath);
  return payload;
}

function compareAckEntries(left, right) {
  const leftAckedAt = toEpochMs(left?.acked_at);
  const rightAckedAt = toEpochMs(right?.acked_at);
  if (leftAckedAt !== rightAckedAt) {
    return leftAckedAt - rightAckedAt;
  }

  const leftToken = String(left?.version_token ?? "");
  const rightToken = String(right?.version_token ?? "");
  const tokenOrder = leftToken.localeCompare(rightToken);
  if (tokenOrder !== 0) return tokenOrder;

  const leftSource = String(left?.source ?? "");
  const rightSource = String(right?.source ?? "");
  const sourceOrder = leftSource.localeCompare(rightSource);
  if (sourceOrder !== 0) return sourceOrder;

  return String(left?.reason ?? "").localeCompare(String(right?.reason ?? ""));
}

async function readAckStateForMerge(ackPath) {
  try {
    const parsed = await readJsonFile(ackPath);
    return normalizeAckState(parsed);
  } catch {
    return normalizeAckState({});
  }
}

async function withAckLocks(ackPaths, fn, lockOptions = {}, logger = console) {
  const uniquePaths = [
    ...new Set(ackPaths.map((ackPath) => path.resolve(ackPath))),
  ].sort();
  const locks = [];
  try {
    for (const ackPath of uniquePaths) {
      locks.push(await acquireAckLock({ ackPath, ...lockOptions }));
    }
    return await fn();
  } finally {
    for (const lock of locks.reverse()) {
      await releaseAckLock(lock, logger);
    }
  }
}

async function mergeAckFiles(sourcePath, targetPath) {
  await withAckLocks([sourcePath, targetPath], async () => {
    const [sourceState, targetState] = await Promise.all([
      readAckStateForMerge(sourcePath),
      readAckStateForMerge(targetPath),
    ]);

    const mergedAcks = { ...targetState.acks };
    for (const [sha, sourceAck] of Object.entries(sourceState.acks)) {
      const current = mergedAcks[sha];
      if (!current || compareAckEntries(sourceAck, current) > 0) {
        mergedAcks[sha] = sourceAck;
      }
    }

    const sourceUpdatedAt = String(sourceState.updated_at ?? "");
    const targetUpdatedAt = String(targetState.updated_at ?? "");
    const mergedUpdatedAt =
      compareAckEntries(
        { acked_at: sourceUpdatedAt },
        { acked_at: targetUpdatedAt },
      ) > 0
        ? sourceUpdatedAt
        : targetUpdatedAt;

    await writeAckStateAtomic(
      targetPath,
      {
        schema_version: ACK_SCHEMA_VERSION,
        updated_at: mergedUpdatedAt,
        acks: mergedAcks,
      },
      mergedUpdatedAt || isoNow(),
    );
    await rm(sourcePath, { force: true }).catch(() => {});
  });
}

async function mergeLegacyReviewTree(sourceDir, targetDir) {
  const sourceInfo = await stat(sourceDir).catch(() => null);
  if (!sourceInfo?.isDirectory()) {
    return;
  }
  await mkdir(targetDir, { recursive: true, mode: 0o700 }).catch(() => {});
  const entries = await readdir(sourceDir, { withFileTypes: true }).catch(
    () => [],
  );
  for (const entry of entries) {
    const sourcePath = path.join(sourceDir, entry.name);
    const targetPath = path.join(targetDir, entry.name);
    const targetInfo = await stat(targetPath).catch(() => null);
    if (!targetInfo) {
      await rename(sourcePath, targetPath).catch(() => {});
      continue;
    }
    if (entry.isFile() && targetInfo.isFile() && entry.name === ".ack.json") {
      await mergeAckFiles(sourcePath, targetPath);
      continue;
    }
    if (entry.isDirectory() && targetInfo.isDirectory()) {
      await mergeLegacyReviewTree(sourcePath, targetPath);
    }
  }
  const remaining = await readdir(sourceDir).catch(() => []);
  if (remaining.length === 0) {
    await rm(sourceDir, { recursive: true, force: true }).catch(() => {});
  }
}

async function migrateLegacyReviewsDir(repoRoot, targetDir) {
  const legacyDir = legacyReviewsDir(repoRoot);
  if (path.resolve(targetDir) === path.resolve(legacyDir)) {
    return;
  }
  const [targetInfo, legacyInfo] = await Promise.all([
    stat(targetDir).catch(() => null),
    stat(legacyDir).catch(() => null),
  ]);
  if (!legacyInfo?.isDirectory()) {
    return;
  }
  if (targetInfo?.isDirectory()) {
    await mergeLegacyReviewTree(legacyDir, targetDir);
    return;
  }
  await mkdir(path.dirname(targetDir), { recursive: true, mode: 0o700 }).catch(
    () => {},
  );
  await rename(legacyDir, targetDir).catch(() => {});
}

function stableEvidence(item) {
  if (!item || typeof item !== "object" || Array.isArray(item)) return null;
  return {
    file: String(item.file ?? ""),
    lines: String(item.lines ?? ""),
    reason: String(item.reason ?? ""),
  };
}

function stableFinding(item) {
  if (!item || typeof item !== "object" || Array.isArray(item)) return null;
  const evidence = Array.isArray(item.evidence)
    ? item.evidence
        .map((entry) => stableEvidence(entry))
        .filter(Boolean)
        .sort((a, b) =>
          `${a.file}:${a.lines}:${a.reason}`.localeCompare(
            `${b.file}:${b.lines}:${b.reason}`,
          ),
        )
    : [];
  return {
    severity: String(item.severity ?? ""),
    confidence:
      typeof item.confidence === "number" && Number.isFinite(item.confidence)
        ? item.confidence
        : 0,
    title: String(item.title ?? ""),
    finding_id: String(item.finding_id ?? ""),
    hypothesis: String(item.hypothesis ?? ""),
    impact: String(item.impact ?? ""),
    recommended_direction: String(item.recommended_direction ?? ""),
    evidence,
  };
}

function findingsHash(findings) {
  const normalized = Array.isArray(findings)
    ? findings
        .map((finding) => stableFinding(finding))
        .filter(Boolean)
        .sort((a, b) =>
          `${a.finding_id}:${a.title}:${a.severity}`.localeCompare(
            `${b.finding_id}:${b.title}:${b.severity}`,
          ),
        )
    : [];
  const raw = JSON.stringify(normalized);
  return crypto.createHash("sha1").update(raw, "utf8").digest("hex");
}

function defaultOpenReports(mdPaths) {
  const normalizedPaths = Array.isArray(mdPaths)
    ? mdPaths.map((mdPath) => String(mdPath ?? "").trim()).filter(Boolean)
    : [];
  const eligiblePaths = normalizedPaths.filter((mdPath) =>
    shouldAutoOpenReportPath(mdPath),
  );
  if (eligiblePaths.length === 0) {
    return false;
  }

  const sublimeCliCommands = [
    "subl",
    "/Applications/Sublime Text.app/Contents/SharedSupport/bin/subl",
    path.join(
      process.env.HOME ?? "",
      "Applications",
      "Sublime Text.app",
      "Contents",
      "SharedSupport",
      "bin",
      "subl",
    ),
  ].filter(Boolean);
  const attempts = [
    ...sublimeCliCommands.map((command) => [command, eligiblePaths]),
    ...(process.platform === "darwin"
      ? [["open", ["-a", "Sublime Text", ...eligiblePaths]]]
      : []),
    ["cursor", eligiblePaths],
    ["code", ["-r", ...eligiblePaths]],
    ["open", eligiblePaths],
  ];

  for (const [command, args] of attempts) {
    const result = spawnSync(command, args, { stdio: "ignore" });
    if (result.status === 0) {
      return true;
    }
  }

  return false;
}

export function defaultOpenReport(mdPath) {
  return defaultOpenReports([mdPath]);
}

export function shouldAutoOpenReportPath(mdPath, repoRoot = TOOL_REPO_ROOT) {
  const rawPath = String(mdPath ?? "").trim();
  const rawRepoRoot = String(repoRoot ?? "").trim();
  if (!rawPath || !rawRepoRoot) {
    return false;
  }
  const normalizedPath = path.resolve(rawPath);
  const normalizedRepoRoot = path.resolve(rawRepoRoot);
  if (normalizedRepoRoot !== path.resolve(TOOL_REPO_ROOT)) {
    return false;
  }
  const relative = path.relative(normalizedRepoRoot, normalizedPath);
  return (
    relative !== "" && !relative.startsWith("..") && !path.isAbsolute(relative)
  );
}

function hasNonWhitespaceText(value) {
  return /[^\s]/.test(String(value ?? ""));
}

async function repairActionableMarkdown({ sha, jsonPath, mdPath, parsed }) {
  const findings = Array.isArray(parsed?.findings) ? parsed.findings : [];
  writeReviewArtifacts({
    newData: {
      summary: String(parsed?.summary ?? ""),
      findings,
    },
    targetJson: jsonPath,
    targetMd: mdPath,
    commitSha: String(parsed?.sha ?? sha),
    triggerName: String(parsed?.trigger_last ?? "manual"),
    reviewStatus: String(parsed?.review_status ?? "ok"),
    findingModelLabel: Array.isArray(parsed?.finding_models)
      ? String(parsed.finding_models[0] ?? "")
      : "",
    repoName: String(parsed?.repository?.name ?? ""),
    repoRoot: String(parsed?.repository?.root ?? ""),
    reviewLane: String(parsed?.lane ?? "codex"),
    preserveExistingQueueEnqueuedAt: true,
  });
}

async function deleteReportArtifacts(jsonPath, mdPath) {
  await Promise.allSettled([
    rm(jsonPath, { force: true }),
    rm(mdPath, { force: true }),
  ]);
}

function getProcessStartTime(pid) {
  if (!Number.isInteger(pid) || pid <= 0) return null;
  const result = spawnSync("ps", ["-o", "lstart=", "-p", String(pid)], {
    encoding: "utf8",
    stdio: ["ignore", "pipe", "ignore"],
  });
  if (result.status !== 0) return null;
  const value = String(result.stdout ?? "").trim();
  return value || null;
}

async function readJsonFile(filePath) {
  const raw = await readFile(filePath, "utf8");
  return JSON.parse(raw);
}

function shaFromJsonFile(fileName) {
  if (!fileName.endsWith(".json")) return "";
  return fileName.slice(0, -5);
}

function toEpochMs(iso) {
  const parsed = Date.parse(String(iso ?? ""));
  return Number.isNaN(parsed) ? 0 : parsed;
}

function severityRank(severity) {
  return reviewSeverityRank(severity);
}

function reportSortKey(report) {
  const findings = Array.isArray(report?.findings) ? report.findings : [];
  const reviewStatus = String(report?.review_status ?? "")
    .trim()
    .toLowerCase();
  const hasFindings = findings.length > 0;
  const actionableClass = hasFindings
    ? isQueueRecoveryReport(report)
      ? 1
      : 3
    : isNonFindingCodexReviewStatusActionable(reviewStatus)
      ? 2
      : 0;
  const maxSeverity = findings.reduce(
    (highest, finding) =>
      Math.max(
        highest,
        severityRank(
          String(finding?.severity ?? "")
            .trim()
            .toLowerCase(),
        ),
      ),
    0,
  );
  const maxConfidence = findings.reduce((highest, finding) => {
    const value = Number(finding?.confidence ?? 0);
    return Number.isFinite(value) ? Math.max(highest, value) : highest;
  }, 0);

  return {
    actionableClass,
    maxSeverity,
    maxConfidence,
    lastReviewedMs: toEpochMs(report?.last_reviewed),
  };
}

function laneForReportsDir(reviewsDir) {
  const base = path.basename(path.resolve(reviewsDir));
  if (base === "local") return "local";
  if (base === "incidents") return "incidents";
  return "codex";
}

function rootReviewsDirForLane(reviewsDir, lane) {
  return lane === "local" ? path.dirname(reviewsDir) : reviewsDir;
}

export function isActionableReport(report, includeFailed) {
  return isActionableCodexReviewReport(report, includeFailed);
}

export function reportFindingsCount(report) {
  return Array.isArray(report?.parsed?.findings)
    ? report.parsed.findings.length
    : 0;
}

export function shouldAutoOpenReport({
  lane = "codex",
  report,
  hasLiteralShaArgs = false,
}) {
  if (hasLiteralShaArgs) return true;
  if (String(lane) === "local") return reportFindingsCount(report) > 0;
  if (String(lane) === "codex") return reportFindingsCount(report) > 0;
  return true;
}

export async function isBacklogRemediationAutomationEnabled(reviewsDir) {
  const config = await readBacklogRemediationAutomationConfig(reviewsDir);
  return config.enabled && !config.paused;
}

export async function pauseBacklogRemediationForManualFallback(
  reviewsDir,
  sha = "",
) {
  const current = await readBacklogRemediationAutomationConfig(reviewsDir);
  if (!(current.enabled && !current.paused)) {
    return current;
  }
  return writeBacklogRemediationAutomationConfig(reviewsDir, {
    ...current,
    enabled: true,
    paused: true,
    pause_reason: "manual_fallback",
    last_opened_sha: String(sha ?? "").trim(),
  });
}

async function reportQueuedBeforeCutoff(
  reviewsDir,
  sha,
  parsed,
  cutoffMs,
  cache,
) {
  if (!Number.isFinite(cutoffMs) || !sha) return false;
  if (cache.has(sha)) {
    const cached = cache.get(sha);
    return cached === true;
  }

  const state = await queueStateForSha(reviewsDir, sha, { lane: "codex" });
  const enqueuedAtMs = Math.max(
    toEpochMs(state?.job?.enqueued_at),
    toEpochMs(parsed?.queue_enqueued_at),
  );
  if (!enqueuedAtMs) {
    cache.set(sha, false);
    return false;
  }
  const queuedBeforeCutoff = enqueuedAtMs <= cutoffMs;
  cache.set(sha, queuedBeforeCutoff);
  return queuedBeforeCutoff;
}

export async function buildBacklogSuppressor(reviewsDir) {
  const lane = laneForReportsDir(reviewsDir);
  if (lane !== "codex") {
    return () => false;
  }
  const rootReviewsDir = rootReviewsDirForLane(reviewsDir, lane);
  const backlogState = await readReviewQueueBacklog(rootReviewsDir, lane);
  if (!backlogState.active) {
    return () => false;
  }
  const cutoffMs = toEpochMs(backlogState.enqueue_after);
  if (!cutoffMs) {
    return () => false;
  }
  const queueStateCache = new Map();
  return ({ sha, parsed }) =>
    reportQueuedBeforeCutoff(
      rootReviewsDir,
      sha,
      parsed,
      cutoffMs,
      queueStateCache,
    );
}

export async function buildStaleSupersessionSuppressor(reviewsDir) {
  const lane = laneForReportsDir(reviewsDir);
  if (lane !== "codex") {
    return () => false;
  }
  const rootReviewsDir = rootReviewsDirForLane(reviewsDir, lane);
  const archiveIfSuperseded =
    await buildStaleSupersessionArchiver(rootReviewsDir);
  return async ({ sha, parsed }) => archiveIfSuperseded({ sha, parsed });
}

export async function buildReportSuppressor(reviewsDir) {
  const lane = laneForReportsDir(reviewsDir);
  if (lane === "codex") {
    const rootReviewsDir = rootReviewsDirForLane(reviewsDir, lane);
    await collectLiveBacklogArtifacts(rootReviewsDir);
  }
  const suppressors = [
    await buildBacklogSuppressor(reviewsDir),
    await buildStaleSupersessionSuppressor(reviewsDir),
  ].filter((value) => typeof value === "function");

  return async (report) => {
    for (const suppressor of suppressors) {
      const result = await suppressor(report);
      if (!result) continue;
      return result;
    }
    return false;
  };
}

function reportContentHash(report) {
  const normalized = {
    review_status: String(report?.review_status ?? "")
      .trim()
      .toLowerCase(),
    failure_reason: String(report?.failure_reason ?? "")
      .trim()
      .toLowerCase(),
    summary: String(report?.summary ?? ""),
    findings: Array.isArray(report?.findings)
      ? report.findings
          .map((finding) => stableFinding(finding))
          .filter(Boolean)
          .sort((a, b) =>
            `${a.finding_id}:${a.title}:${a.severity}`.localeCompare(
              `${b.finding_id}:${b.title}:${b.severity}`,
            ),
          )
      : [],
  };
  const raw = JSON.stringify(normalized);
  return crypto.createHash("sha1").update(raw, "utf8").digest("hex");
}

export function reportContentHashLegacyV2(report) {
  const normalized = {
    review_status: String(report?.review_status ?? "")
      .trim()
      .toLowerCase(),
    failure_reason: String(report?.failure_reason ?? "")
      .trim()
      .toLowerCase(),
    summary: String(report?.summary ?? ""),
    findings: Array.isArray(report?.findings)
      ? report.findings
          .map((finding) => {
            if (
              !finding ||
              typeof finding !== "object" ||
              Array.isArray(finding)
            ) {
              return null;
            }
            return {
              finding_id: String(finding?.finding_id ?? ""),
              title: String(finding?.title ?? ""),
              severity: String(finding?.severity ?? "")
                .trim()
                .toLowerCase(),
              confidence: Number(finding?.confidence ?? 0),
              hypothesis: String(finding?.hypothesis ?? ""),
              impact: String(finding?.impact ?? ""),
              recommended_direction: String(
                finding?.recommended_direction ?? "",
              ),
              evidence: Array.isArray(finding?.evidence)
                ? finding.evidence.map((item) => ({
                    file: String(item?.file ?? ""),
                    lines: String(item?.lines ?? ""),
                    reason: String(item?.reason ?? ""),
                  }))
                : [],
            };
          })
          .filter(Boolean)
      : [],
  };
  const raw = JSON.stringify(normalized);
  return crypto.createHash("sha1").update(raw, "utf8").digest("hex");
}

export function versionTokenForReport(sha, report) {
  return `v2:${sha}:${reportContentHash(report)}`;
}

export function ackMatchesReportVersion(sha, report, ackVersionToken) {
  const normalizedAck = String(ackVersionToken ?? "").trim();
  if (!normalizedAck) return false;

  const nextToken = versionTokenForReport(sha, report);
  if (normalizedAck === nextToken) return true;

  const legacyV2Prefix = `v2:${sha}:`;
  if (normalizedAck.startsWith(legacyV2Prefix)) {
    const legacyHash = reportContentHashLegacyV2(report);
    return normalizedAck === `${legacyV2Prefix}${legacyHash}`;
  }

  return false;
}

export function normalizeAckState(parsed) {
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    return {
      schema_version: ACK_SCHEMA_VERSION,
      updated_at: "",
      acks: {},
    };
  }

  const acks = {};
  const rawAcks = parsed.acks;
  if (rawAcks && typeof rawAcks === "object" && !Array.isArray(rawAcks)) {
    for (const [sha, value] of Object.entries(rawAcks)) {
      if (!value || typeof value !== "object" || Array.isArray(value)) continue;
      const versionToken = String(value.version_token ?? "");
      if (!versionToken) continue;
      acks[String(sha)] = {
        version_token: versionToken,
        acked_at: String(value.acked_at ?? ""),
        reason: String(value.reason ?? ""),
        source: String(value.source ?? ""),
      };
    }
  }

  return {
    schema_version: ACK_SCHEMA_VERSION,
    updated_at: String(parsed.updated_at ?? ""),
    acks,
  };
}

export async function readAckState(ackPath, logger = console) {
  try {
    const parsed = await readJsonFile(ackPath);
    return normalizeAckState(parsed);
  } catch (error) {
    if (error?.code !== "ENOENT") {
      logger.warn(
        `[codex-review-inbox] Warning: failed to parse ack store at ${ackPath}; resetting.`,
      );
    }
    return normalizeAckState({});
  }
}

export async function writeAckStateAtomic(ackPath, state, now = isoNow()) {
  const dir = path.dirname(ackPath);
  await mkdir(dir, { recursive: true });
  const payload = {
    schema_version: ACK_SCHEMA_VERSION,
    updated_at: now,
    acks: state.acks,
  };
  const tempPath = `${ackPath}.tmp.${process.pid}.${Date.now()}`;
  await writeFile(tempPath, `${JSON.stringify(payload, null, 2)}\n`, {
    encoding: "utf8",
    mode: 0o600,
  });
  await rename(tempPath, ackPath);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function acquireAckLock({
  ackPath,
  timeoutMs = 5000,
  pollIntervalMs = 40,
  staleAfterMs = 30000,
  processStartTimeFn = getProcessStartTime,
  missingStartTimeEvictAfterMs = 900000,
}) {
  const lockPath = `${ackPath}.lock`;
  const start = Date.now();
  while (true) {
    try {
      await mkdir(lockPath, { mode: 0o700 });
      const owner = {
        pid: process.pid,
        token: crypto.randomUUID(),
        created_at: isoNow(),
        start_time: processStartTimeFn(process.pid) ?? "",
      };
      await writeFile(
        path.join(lockPath, LOCK_OWNER_FILE),
        `${JSON.stringify(owner)}\n`,
        {
          encoding: "utf8",
          mode: 0o600,
        },
      );
      return { lockPath, owner };
    } catch (error) {
      if (error?.code !== "EEXIST") {
        throw error;
      }

      try {
        const ownerPath = path.join(lockPath, LOCK_OWNER_FILE);
        const owner = await readJsonFile(ownerPath).catch(() => null);
        const details = await stat(lockPath);
        const ageMs = Date.now() - details.mtimeMs;
        const ownerPid =
          owner && Number.isInteger(owner.pid) ? Number(owner.pid) : null;
        let ownerAlive = false;
        if (ownerPid !== null) {
          try {
            process.kill(ownerPid, 0);
            ownerAlive = true;
          } catch (ownerError) {
            ownerAlive = ownerError?.code !== "ESRCH";
          }
        }
        const ownerStartTime =
          owner && typeof owner.start_time === "string"
            ? owner.start_time.trim()
            : "";
        let ownerIdentityMismatch = false;
        if (ownerAlive && ownerPid !== null && ownerStartTime) {
          const liveStartTime = processStartTimeFn(ownerPid);
          if (liveStartTime) {
            ownerIdentityMismatch = liveStartTime !== ownerStartTime;
          }
        }
        const missingStartTimeStale =
          ownerAlive && !ownerStartTime && ageMs > missingStartTimeEvictAfterMs;
        const canEvict =
          ageMs > staleAfterMs &&
          (!ownerAlive || ownerIdentityMismatch || missingStartTimeStale);
        if (canEvict) {
          await rm(lockPath, { recursive: true, force: true });
          continue;
        }
      } catch {
        // If stat/rm races with another process, continue retry loop.
      }

      if (Date.now() - start > timeoutMs) {
        throw new Error(`Timed out acquiring ack lock: ${lockPath}`);
      }
      await sleep(pollIntervalMs);
    }
  }
}

async function releaseAckLock(lock, logger = console) {
  const ownerPath = path.join(lock.lockPath, LOCK_OWNER_FILE);
  const owner = await readJsonFile(ownerPath).catch(() => null);
  if (!owner || owner.token !== lock.owner.token) {
    logger.warn(
      `[codex-review-inbox] Lock ownership changed; skipping unlock for ${lock.lockPath}`,
    );
    return false;
  }
  await rm(lock.lockPath, { recursive: true, force: true });
  return true;
}

async function withAckLock(ackPath, fn, lockOptions = {}, logger = console) {
  const lock = await acquireAckLock({ ackPath, ...lockOptions });
  try {
    return await fn();
  } finally {
    await releaseAckLock(lock, logger);
  }
}

export async function updateAckState({
  ackPath,
  now = isoNow(),
  logger = console,
  lockOptions = {},
  mutate,
}) {
  return withAckLock(
    ackPath,
    async () => {
      const ackState = await readAckState(ackPath, logger);
      const changed = await mutate(ackState);
      if (changed) {
        await writeAckStateAtomic(ackPath, ackState, now);
      }
      return { changed, ackState };
    },
    lockOptions,
    logger,
  );
}

export async function resolveReviewsDir(
  repoRoot,
  preferredDir,
  cwd = process.cwd(),
) {
  const override = normalizeReviewRootOverride(
    repoRoot,
    resolveReviewRootOverride(preferredDir),
    cwd,
  );
  const canonical = defaultReviewsDir(repoRoot);
  const legacy = legacyReviewsDir(repoRoot);
  let preferred = override || canonical;
  if (path.resolve(preferred) === path.resolve(legacy)) {
    const canonicalInfo = await stat(canonical).catch(() => null);
    if (canonicalInfo?.isDirectory()) {
      preferred = canonical;
    }
  }
  await migrateLegacyReviewsDir(repoRoot, preferred);
  try {
    await mkdir(path.join(preferred, "logs"), { recursive: true, mode: 0o700 });
    return preferred;
  } catch {
    const uid =
      typeof process.getuid === "function"
        ? String(process.getuid())
        : "unknown";
    const repoHash = crypto
      .createHash("sha256")
      .update(String(repoRoot), "utf8")
      .digest("hex");
    const fallback = path.join(
      tmpdir(),
      `contribution-code-reviews-${uid}-${repoHash}`,
    );
    await mkdir(path.join(fallback, "logs"), { recursive: true, mode: 0o700 });
    return fallback;
  }
}

export async function resolveDurableWorkerReviewsDir(
  repoRoot,
  preferredDir,
  cwd = process.cwd(),
) {
  const resolved = await resolveReviewsDir(repoRoot, preferredDir, cwd);
  const legacyDir = legacyReviewsDir(repoRoot);
  const canonicalDir = defaultReviewsDir(repoRoot);
  if (path.resolve(resolved) !== path.resolve(legacyDir)) {
    return resolved;
  }
  const canonicalInfo = await stat(canonicalDir).catch(() => null);
  if (canonicalInfo?.isDirectory()) {
    return canonicalDir;
  }
  return resolved;
}

export async function collectReports({
  reviewsDir,
  includeFailed,
  shouldSuppressReport = () => false,
}) {
  const [entries, operatorState] = await Promise.all([
    readdir(reviewsDir, { withFileTypes: true }).catch((error) => {
      if (error?.code === "ENOENT") {
        return [];
      }
      throw error;
    }),
    readOperatorState({ reviewsDir }),
  ]);
  const reports = [];
  const seenShas = new Set();
  const suppressReport =
    typeof shouldSuppressReport === "function"
      ? shouldSuppressReport
      : () => false;
  for (const entry of entries) {
    if (!entry.isFile()) continue;
    if (!entry.name.endsWith(".json")) continue;
    if (entry.name === ".ack.json" || entry.name === ".operator-state.json")
      continue;
    const sha = shaFromJsonFile(entry.name);
    if (!sha) continue;
    seenShas.add(sha);

    const jsonPath = path.join(reviewsDir, entry.name);
    const mdPath = path.join(reviewsDir, `${sha}.md`);
    let parsed;
    try {
      parsed = await readJsonFile(jsonPath);
    } catch {
      continue;
    }

    let mdText = "";
    let mdMissing = false;
    try {
      mdText = await readFile(mdPath, "utf8");
    } catch (error) {
      if (error?.code === "ENOENT") {
        mdMissing = true;
      } else {
        continue;
      }
    }

    let effectiveParsed = applyOperatorClosuresToReport({
      operatorState,
      sha,
      report: parsed,
    });

    let actionable = isActionableReport(effectiveParsed, includeFailed);
    if (!mdMissing && !hasNonWhitespaceText(mdText)) {
      // Treat whitespace-only markdown as an explicit clear signal.
      await deleteReportArtifacts(jsonPath, mdPath);
      continue;
    }
    if (mdMissing && actionable) {
      try {
        await repairActionableMarkdown({ sha, jsonPath, mdPath, parsed });
        parsed = await readJsonFile(jsonPath);
        effectiveParsed = applyOperatorClosuresToReport({
          operatorState,
          sha,
          report: parsed,
        });
        mdText = await readFile(mdPath, "utf8");
        mdMissing = false;
        actionable = isActionableReport(effectiveParsed, includeFailed);
      } catch {
        // Leave mismatched artifacts untouched if repair fails.
      }
    }
    if (mdMissing) {
      // Skip orphan sidecars non-destructively to avoid deleting reports during
      // transient write races.
      continue;
    }
    if (!hasNonWhitespaceText(mdText)) {
      // Skip empty/whitespace markdown non-destructively to avoid deleting
      // in-progress report writes.
      continue;
    }

    const versionToken = versionTokenForReport(sha, parsed);
    const suppression = actionable
      ? await suppressReport({ sha, parsed, jsonPath, mdPath })
      : false;
    const suppressedByBacklog =
      suppression === true || suppression?.suppress === true;
    if (suppression?.archivedArtifacts) {
      continue;
    }
    reports.push({
      sha,
      jsonPath,
      mdPath,
      parsed: effectiveParsed,
      originalParsed: parsed,
      actionable: actionable && !suppressedByBacklog,
      suppressedByBacklog,
      versionToken,
    });
  }
  return reports.sort((a, b) => {
    const left = reportSortKey(a.parsed);
    const right = reportSortKey(b.parsed);
    if (right.actionableClass !== left.actionableClass) {
      return right.actionableClass - left.actionableClass;
    }
    if (right.maxSeverity !== left.maxSeverity) {
      return right.maxSeverity - left.maxSeverity;
    }
    if (right.maxConfidence !== left.maxConfidence) {
      return right.maxConfidence - left.maxConfidence;
    }
    if (right.lastReviewedMs !== left.lastReviewedMs) {
      return right.lastReviewedMs - left.lastReviewedMs;
    }
    return a.sha.localeCompare(b.sha);
  });
}

export async function runCatchup({
  reviewsDir,
  ackPath = path.join(reviewsDir, ".ack.json"),
  includeFailed = true,
  maxOpens = 1,
  shaFilter = null,
  shouldSuppressReport = null,
  comparePendingReports = null,
  dryRun = false,
  openReport = defaultOpenReport,
  openReports = null,
  shouldOpenReport = () => true,
  now = isoNow(),
  logger = console,
  lockOptions = {},
}) {
  const ackState = await readAckState(ackPath, logger);
  const reports = await collectReports({
    reviewsDir,
    includeFailed,
    shouldSuppressReport,
  });
  const normalizedShaFilter =
    shaFilter instanceof Set
      ? new Set(
          [...shaFilter]
            .map((sha) =>
              String(sha ?? "")
                .trim()
                .toLowerCase(),
            )
            .filter(Boolean),
        )
      : Array.isArray(shaFilter)
        ? new Set(
            shaFilter
              .map((sha) =>
                String(sha ?? "")
                  .trim()
                  .toLowerCase(),
              )
              .filter(Boolean),
          )
        : null;
  const filteredReports =
    normalizedShaFilter && normalizedShaFilter.size > 0
      ? reports.filter((report) => normalizedShaFilter.has(report.sha))
      : reports;
  const actionable = filteredReports.filter((report) => report.actionable);
  let pending = actionable.filter(
    (report) =>
      !ackMatchesReportVersion(
        report.sha,
        report.parsed,
        ackState.acks?.[report.sha]?.version_token,
      ),
  );
  if (typeof comparePendingReports === "function" && pending.length > 1) {
    pending = [...pending].sort(comparePendingReports);
  }

  const openedReports = [];
  const ackUpdates = {};
  const openBudget = Math.max(0, Number(maxOpens) || 0);
  const reportsToOpen = [];

  for (const report of pending) {
    if (reportsToOpen.length >= openBudget) break;
    if (!shouldOpenReport(report)) continue;
    reportsToOpen.push(report);
  }

  const batchOpenReports =
    typeof openReports === "function"
      ? openReports
      : openReport === defaultOpenReport
        ? defaultOpenReports
        : null;

  if (dryRun) {
    openedReports.push(...reportsToOpen);
  } else if (reportsToOpen.length === 1) {
    if (await Promise.resolve(openReport(reportsToOpen[0].mdPath))) {
      openedReports.push(reportsToOpen[0]);
    }
  } else if (reportsToOpen.length > 1) {
    const batchPaths = reportsToOpen.map((report) => report.mdPath);
    if (await Promise.resolve(batchOpenReports?.(batchPaths))) {
      openedReports.push(...reportsToOpen);
    } else {
      for (const report of reportsToOpen) {
        if (!(await Promise.resolve(openReport(report.mdPath)))) continue;
        openedReports.push(report);
      }
    }
  }

  for (const report of openedReports) {
    ackUpdates[report.sha] = {
      version_token: report.versionToken,
      acked_at: now,
      reason: dryRun ? "dry-run-opened" : "opened",
      source: "inbox",
    };
  }
  const opened = openedReports.length;

  let ackChanged = false;
  if (Object.keys(ackUpdates).length > 0 && !dryRun) {
    const updated = await updateAckState({
      ackPath,
      now,
      logger,
      lockOptions,
      mutate: (latest) => {
        let changed = false;
        for (const [sha, ack] of Object.entries(ackUpdates)) {
          const existing = latest.acks[sha];
          if (
            existing &&
            existing.version_token === ack.version_token &&
            existing.acked_at === ack.acked_at &&
            existing.reason === ack.reason &&
            existing.source === ack.source
          ) {
            continue;
          }
          latest.acks[sha] = ack;
          changed = true;
        }
        return changed;
      },
    });
    ackChanged = updated.changed;
  } else if (Object.keys(ackUpdates).length > 0 && dryRun) {
    ackChanged = true;
  }

  return {
    reports: filteredReports,
    actionable,
    pending,
    opened,
    ackChanged,
    ackPath,
  };
}

export async function ackCurrentVersions({
  reports,
  ackPath,
  sha,
  all = false,
  source = "ack-script",
  reason = "manual",
  now = isoNow(),
  logger = console,
  lockOptions = {},
}) {
  const reportMap = new Map(reports.map((report) => [report.sha, report]));
  let found = true;

  const result = await updateAckState({
    ackPath,
    now,
    logger,
    lockOptions,
    mutate: (ackState) => {
      let changed = false;
      if (all) {
        for (const report of reports.filter((item) => item.actionable)) {
          const next = {
            version_token: report.versionToken,
            acked_at: now,
            reason,
            source,
          };
          const current = ackState.acks[report.sha];
          if (
            current &&
            current.version_token === next.version_token &&
            current.acked_at === next.acked_at &&
            current.reason === next.reason &&
            current.source === next.source
          ) {
            continue;
          }
          ackState.acks[report.sha] = next;
          changed = true;
        }
      } else {
        const target = reportMap.get(String(sha));
        if (!target) {
          found = false;
          return false;
        }
        const next = {
          version_token: target.versionToken,
          acked_at: now,
          reason,
          source,
        };
        const current = ackState.acks[target.sha];
        if (
          !current ||
          current.version_token !== next.version_token ||
          current.acked_at !== next.acked_at ||
          current.reason !== next.reason ||
          current.source !== next.source
        ) {
          ackState.acks[target.sha] = next;
          changed = true;
        }
      }
      return changed;
    },
  });

  return { changed: result.changed, found, ackPath };
}

export async function clearMissingAcks({
  reports,
  ackPath,
  now = isoNow(),
  logger = console,
  lockOptions = {},
}) {
  const reportVersions = new Map(
    reports.map((report) => [report.sha, report.versionToken]),
  );
  const result = await updateAckState({
    ackPath,
    now,
    logger,
    lockOptions,
    mutate: (ackState) => {
      let changed = false;
      for (const [sha, ack] of Object.entries(ackState.acks)) {
        const token = reportVersions.get(sha);
        const report = reports.find((item) => item.sha === sha);
        if (
          !token ||
          !report ||
          !ackMatchesReportVersion(sha, report.parsed, ack.version_token)
        ) {
          delete ackState.acks[sha];
          changed = true;
        }
      }
      return changed;
    },
  });

  return { changed: result.changed, ackPath };
}
