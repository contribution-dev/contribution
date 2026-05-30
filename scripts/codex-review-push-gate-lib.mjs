#!/usr/bin/env node

import { execFileSync, spawnSync } from "node:child_process";
import crypto from "node:crypto";
import { readFileSync } from "node:fs";
import {
  mkdir,
  open,
  readFile,
  rename,
  unlink,
  writeFile,
} from "node:fs/promises";
import path from "node:path";
import {
  queueStateForSha,
  readReviewQueuePause,
} from "./codex-review-queue-lib.mjs";
import { isTerminalManualTakeoverSha } from "./lib/codex-review-manual-takeover.mjs";
import {
  ZERO_SHA,
  collectOutgoingByUpdate,
  computeOutgoingShas,
  parseRefUpdates,
} from "./lib/pre-push-branch-state.mjs";
import {
  parseMinReviewSeverity,
  reviewSeverityRank,
} from "./lib/review-severity.mjs";
import {
  isActionableCodexReviewReport,
  isIncompleteCodexReviewReport,
} from "./lib/codex-review-state.mjs";

export { ZERO_SHA, computeOutgoingShas, parseRefUpdates };

export const REVIEW_OPERATOR_STATE_SCHEMA_VERSION = 1;
export const REVIEWED_SHAS_SCHEMA_VERSION = 5;
const REVIEWED_LANES = ["codex"];

export function severityRank(value) {
  return reviewSeverityRank(value);
}

export function parseMinSeverity(value) {
  return parseMinReviewSeverity(value, "major");
}

export function defaultGitExec(args, repoRoot) {
  return execFileSync("git", args, {
    cwd: repoRoot,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
}

export function isReviewProcessRunningForSha(sha, processListText) {
  if (!sha) return false;
  const expectedSha = String(sha).trim();
  for (const line of String(processListText ?? "").split(/\r?\n/u)) {
    const tokens = line.trim().split(/\s+/u);
    if (!tokens.some((token) => token.includes("codex-review-commit"))) {
      continue;
    }
    for (let index = 0; index < tokens.length - 1; index += 1) {
      if (tokens[index] === "--sha" && tokens[index + 1] === expectedSha) {
        return true;
      }
    }
  }
  return false;
}

function parseProcessTable(processTableText) {
  const map = new Map();
  const lines = String(processTableText ?? "")
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
  for (const line of lines) {
    const match = /^(\d+)\s+(.+)$/.exec(line);
    if (!match) continue;
    map.set(match[1], match[2]);
  }
  return map;
}

function snapshotProcessTable() {
  const attempts = [
    ["-axww", "-o", "pid=,command="],
    ["-axo", "pid=,command="],
  ];
  for (const args of attempts) {
    const ps = spawnSync("ps", args, {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    });
    if (ps.status !== 0) continue;
    const processTableText = String(ps.stdout ?? "");
    const processTable = parseProcessTable(processTableText);
    return {
      available: true,
      processTableText,
      processTable,
    };
  }
  return {
    available: false,
    processTableText: "",
    processTable: new Map(),
  };
}

export async function defaultIsReviewInProgress({ reviewsDir, sha }) {
  const queueState = await queueStateForSha(reviewsDir, sha);
  const snapshot = snapshotProcessTable();
  const processTable = snapshot.processTable;
  const processListText = Array.from(processTable.values()).join("\n");
  if (
    (queueState.status === "pending" || queueState.status === "active") &&
    !queueState.stale
  ) {
    return true;
  }
  if (isReviewProcessRunningForSha(sha, processListText)) {
    return true;
  }
  return false;
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export async function waitForReviewToSettle({
  reviewsDir,
  sha,
  timeoutMs,
  pollMs = 1000,
  isReviewInProgress = defaultIsReviewInProgress,
  sleepFn = sleep,
}) {
  const deadline = Date.now() + timeoutMs;
  let waited = false;

  while (true) {
    const inProgress = await isReviewInProgress({ reviewsDir, sha });
    if (!inProgress) {
      return { waited, timedOut: false };
    }
    if (Date.now() >= deadline) {
      return { waited, timedOut: true };
    }
    waited = true;
    await sleepFn(pollMs);
  }
}

function summarizeFindings(findings) {
  const normalizedFindings = Array.isArray(findings) ? findings : [];
  let worst = "none";
  for (const finding of normalizedFindings) {
    const severity = normalizeSeverity(finding?.severity);
    if (severityRank(severity) > severityRank(worst)) {
      worst = severity;
    }
  }
  return {
    count: normalizedFindings.length,
    worstSeverity: worst,
  };
}

function normalizeFindingToken(value) {
  return String(value ?? "")
    .trim()
    .toLowerCase();
}

function isSupportedReviewedLane(lane) {
  return REVIEWED_LANES.includes(
    String(lane ?? "")
      .trim()
      .toLowerCase(),
  );
}

function createReviewedLaneState() {
  return {
    any: new Set(),
    clean: new Set(),
  };
}

function createReviewedLaneMetadataState() {
  return {
    reviewedAtBySha: new Map(),
  };
}

function addReviewedLaneStatus(lanes, sha, lane, status) {
  const normalizedLane = String(lane ?? "")
    .trim()
    .toLowerCase();
  if (!sha || !isSupportedReviewedLane(normalizedLane)) {
    return;
  }
  const normalizedStatus = normalizeFindingToken(status);
  if (normalizedStatus !== "reviewed" && normalizedStatus !== "clean") {
    return;
  }
  lanes[normalizedLane].any.add(sha);
  if (normalizedStatus === "clean") {
    lanes[normalizedLane].clean.add(sha);
  }
}

function setReviewedLaneStatus(lanes, sha, lane, status) {
  const normalizedLane = String(lane ?? "")
    .trim()
    .toLowerCase();
  if (!sha || !isSupportedReviewedLane(normalizedLane)) {
    return;
  }
  lanes[normalizedLane].any.delete(sha);
  lanes[normalizedLane].clean.delete(sha);

  const normalizedStatus = normalizeFindingToken(status);
  if (
    !normalizedStatus ||
    normalizedStatus === "missing" ||
    normalizedStatus === "none"
  ) {
    return;
  }
  addReviewedLaneStatus(lanes, sha, normalizedLane, normalizedStatus);
}

function overrideEntriesForLane(overrides, lane) {
  const value = overrides?.[lane];
  if (!value) return [];
  if (value instanceof Map) {
    return [...value.entries()];
  }
  if (typeof value === "object") {
    return Object.entries(value);
  }
  return [];
}

function parseReviewedPayload(parsed) {
  const reviewed = Array.isArray(parsed?.reviewed) ? parsed.reviewed : [];
  const lanes = {
    codex: createReviewedLaneState(),
  };
  const laneMetadata = {
    codex: createReviewedLaneMetadataState(),
  };

  for (const entry of reviewed) {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) {
      continue;
    }
    const sha = String(entry.sha ?? "").trim();
    if (!sha) continue;

    addReviewedLaneStatus(lanes, sha, "codex", entry?.status);
    addReviewedLaneStatus(lanes, sha, "codex", entry?.lanes?.codex);
    const reviewedAt = String(entry?.reviewed_at ?? "").trim();
    if (lanes.codex.clean.has(sha) && Number.isFinite(Date.parse(reviewedAt))) {
      laneMetadata.codex.reviewedAtBySha.set(sha, reviewedAt);
    }
  }

  return {
    any: new Set(lanes.codex.any),
    clean: new Set(lanes.codex.clean),
    lanes,
    laneMetadata,
  };
}

export function findingSignature(finding) {
  return [
    normalizeFindingToken(finding?.key),
    normalizeFindingToken(finding?.id),
    normalizeFindingToken(finding?.title),
    normalizeFindingToken(finding?.severity),
    normalizeFindingToken(finding?.file),
    Number.isInteger(finding?.start) ? String(finding.start) : "",
    Number.isInteger(finding?.end) ? String(finding.end) : "",
  ].join("|");
}

function stableOperatorEvidence(item) {
  if (!item || typeof item !== "object" || Array.isArray(item)) return null;
  return {
    file: String(item.file ?? ""),
    lines: String(item.lines ?? ""),
    reason: String(item.reason ?? ""),
  };
}

function stableOperatorFinding(item) {
  if (!item || typeof item !== "object" || Array.isArray(item)) return null;
  const evidence = Array.isArray(item.evidence)
    ? item.evidence
        .map((entry) => stableOperatorEvidence(entry))
        .filter(Boolean)
        .sort((left, right) =>
          `${left.file}:${left.lines}:${left.reason}`.localeCompare(
            `${right.file}:${right.lines}:${right.reason}`,
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
    finding_id: String(item.finding_id ?? item.id ?? item.key ?? ""),
    hypothesis: String(item.hypothesis ?? ""),
    impact: String(item.impact ?? ""),
    recommended_direction: String(item.recommended_direction ?? ""),
    evidence,
  };
}

function reportContentHashForOperatorState(report) {
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
          .map((finding) => stableOperatorFinding(finding))
          .filter(Boolean)
          .sort((left, right) =>
            `${left.finding_id}:${left.title}:${left.severity}`.localeCompare(
              `${right.finding_id}:${right.title}:${right.severity}`,
            ),
          )
      : [],
  };
  const raw = JSON.stringify(normalized);
  return crypto.createHash("sha1").update(raw, "utf8").digest("hex");
}

export function reviewVersionTokenForOperatorState(sha, report) {
  const normalizedSha = String(sha ?? "")
    .trim()
    .toLowerCase();
  return `operator-v1:${normalizedSha}:${reportContentHashForOperatorState(report)}`;
}

function sanitizeBranchName(branchName) {
  const trimmed = String(branchName ?? "").trim();
  if (!trimmed || trimmed === "HEAD") return "detached";
  return trimmed.replace(/[^a-zA-Z0-9._-]+/g, "_");
}

function resolveDismissalsPath(repoRoot, branchName) {
  const branchFile = `${sanitizeBranchName(branchName)}.json`;
  return path.join(repoRoot, ".git", "codex-review", "dismissals", branchFile);
}

function resolveReviewedShasPath(repoRoot) {
  return path.join(repoRoot, ".git", "codex-review", "reviewed", "global.json");
}

export function resolveOperatorStatePath(reviewsDir) {
  return path.join(reviewsDir, ".operator-state.json");
}

export function normalizeOperatorState(parsed) {
  const reports =
    parsed?.reports &&
    typeof parsed.reports === "object" &&
    !Array.isArray(parsed.reports)
      ? parsed.reports
      : {};
  const closedFindings =
    parsed?.closed_findings &&
    typeof parsed.closed_findings === "object" &&
    !Array.isArray(parsed.closed_findings)
      ? parsed.closed_findings
      : {};
  return {
    schema_version: REVIEW_OPERATOR_STATE_SCHEMA_VERSION,
    updated_at: String(parsed?.updated_at ?? ""),
    reports,
    closed_findings: closedFindings,
  };
}

export async function readOperatorState({
  reviewsDir,
  readFileFn = readFile,
} = {}) {
  if (!reviewsDir) {
    return normalizeOperatorState({});
  }
  try {
    const raw = await readFileFn(resolveOperatorStatePath(reviewsDir), "utf8");
    return normalizeOperatorState(JSON.parse(raw));
  } catch {
    return normalizeOperatorState({});
  }
}

export function readOperatorStateSync({
  reviewsDir,
  readFileSyncFn = readFileSync,
} = {}) {
  if (!reviewsDir) {
    return normalizeOperatorState({});
  }
  try {
    const raw = readFileSyncFn(resolveOperatorStatePath(reviewsDir), "utf8");
    return normalizeOperatorState(JSON.parse(raw));
  } catch {
    return normalizeOperatorState({});
  }
}

export function closedFindingStateKey(sha, signature) {
  const normalizedSha = String(sha ?? "")
    .trim()
    .toLowerCase();
  const normalizedSignature = String(signature ?? "").trim();
  return normalizedSha && normalizedSignature
    ? `${normalizedSha}:${normalizedSignature}`
    : "";
}

function operatorReportClosedAtCurrentVersion(operatorState, sha, report) {
  const normalizedSha = String(sha ?? "")
    .trim()
    .toLowerCase();
  if (!normalizedSha) return false;
  const reportState = operatorState?.reports?.[normalizedSha];
  const closedVersionToken = String(
    reportState?.closed_version_token ?? "",
  ).trim();
  return (
    closedVersionToken &&
    closedVersionToken ===
      reviewVersionTokenForOperatorState(normalizedSha, report)
  );
}

export function operatorClosedFindingSignaturesForReport({
  operatorState,
  sha,
  report,
}) {
  const normalizedSha = String(sha ?? "")
    .trim()
    .toLowerCase();
  const signatures = new Set();
  if (!normalizedSha) return signatures;
  const findings = Array.isArray(report?.findings) ? report.findings : [];
  const closeWholeReport = operatorReportClosedAtCurrentVersion(
    operatorState,
    normalizedSha,
    report,
  );
  const reportState = operatorState?.reports?.[normalizedSha];
  const closedReportVersionToken = String(
    reportState?.closed_version_token ?? "",
  ).trim();

  for (const finding of findings) {
    const signature = normalizeFindingToken(findingSignature(finding));
    if (!signature) continue;
    const scopedKey = closedFindingStateKey(normalizedSha, signature);
    const closedFinding = operatorState?.closed_findings?.[scopedKey];
    const closedSha = String(closedFinding?.sha ?? "")
      .trim()
      .toLowerCase();
    const closedFindingVersionToken = String(
      closedFinding?.version_token ?? "",
    ).trim();
    const staleReportLevelFindingClosure =
      closedReportVersionToken &&
      closedFindingVersionToken &&
      closedFindingVersionToken === closedReportVersionToken &&
      !closeWholeReport;
    if (
      closeWholeReport ||
      (closedSha === normalizedSha && !staleReportLevelFindingClosure)
    ) {
      signatures.add(signature);
    }
  }
  return signatures;
}

export function applyOperatorClosuresToReport({ operatorState, sha, report }) {
  if (!report || typeof report !== "object" || Array.isArray(report)) {
    return report;
  }
  const originalFindings = Array.isArray(report.findings)
    ? report.findings
    : [];
  if (originalFindings.length === 0) {
    return report;
  }
  const closedSignatures = operatorClosedFindingSignaturesForReport({
    operatorState,
    sha,
    report,
  });
  if (closedSignatures.size === 0) {
    return report;
  }
  const openFindings = originalFindings.filter(
    (finding) =>
      !closedSignatures.has(normalizeFindingToken(findingSignature(finding))),
  );
  return openFindings.length === originalFindings.length
    ? report
    : {
        ...report,
        findings: openFindings,
      };
}

export async function loadDismissedFindingSignatures({
  repoRoot,
  branchName,
  readFileFn = readFile,
}) {
  const dismissalsPath = resolveDismissalsPath(repoRoot, branchName);
  let parsed;
  try {
    const raw = await readFileFn(dismissalsPath, "utf8");
    parsed = JSON.parse(raw);
  } catch {
    return new Map();
  }

  const dismissed = Array.isArray(parsed?.dismissed) ? parsed.dismissed : [];
  const map = new Map();
  for (const entry of dismissed) {
    const sha = String(entry?.sha ?? "").trim();
    const lane = String(entry?.lane ?? "")
      .trim()
      .toLowerCase();
    const signature = normalizeFindingToken(entry?.signature);
    if (!sha || !signature) continue;
    const keys = new Set();
    if (!lane) {
      keys.add(sha);
    } else if (lane === "codex") {
      keys.add(`codex:${sha}`);
    } else {
      keys.add(`${lane}:${sha}`);
    }
    for (const key of keys) {
      const set = map.get(key) ?? new Set();
      set.add(signature);
      map.set(key, set);
    }
  }
  return map;
}

export async function loadReviewedShas({ repoRoot, readFileFn = readFile }) {
  const globalReviewed = {
    any: new Set(),
    clean: new Set(),
    lanes: {
      codex: createReviewedLaneState(),
    },
    laneMetadata: {
      codex: createReviewedLaneMetadataState(),
    },
  };
  const reviewedPath = resolveReviewedShasPath(repoRoot);
  try {
    const raw = await readFileFn(reviewedPath, "utf8");
    const parsed = JSON.parse(raw);
    const parsedReviewed = parseReviewedPayload(parsed);
    for (const sha of parsedReviewed.any) globalReviewed.any.add(sha);
    for (const sha of parsedReviewed.clean) globalReviewed.clean.add(sha);
    for (const sha of parsedReviewed.lanes.codex.any) {
      globalReviewed.lanes.codex.any.add(sha);
    }
    for (const sha of parsedReviewed.lanes.codex.clean) {
      globalReviewed.lanes.codex.clean.add(sha);
    }
    for (const [sha, reviewedAt] of parsedReviewed.laneMetadata.codex
      .reviewedAtBySha) {
      globalReviewed.laneMetadata.codex.reviewedAtBySha.set(sha, reviewedAt);
    }
  } catch {}

  return globalReviewed;
}

export async function saveReviewedShas({
  repoRoot,
  reviewedAnyShas,
  reviewedCleanShas,
  reviewedLaneAnyShas = {},
  reviewedLaneCleanShas = {},
  reviewedLaneStatusOverrides = {},
  reviewedLaneReviewedAt = {},
  mkdirFn = mkdir,
  readFileFn = readFile,
  openFn = open,
  renameFn = rename,
  unlinkFn = unlink,
  writeFileFn = writeFile,
}) {
  const reviewedPath = resolveReviewedShasPath(repoRoot);
  await mkdirFn(path.dirname(reviewedPath), { recursive: true });
  const lockPath = `${reviewedPath}.lock`;
  const lockDeadlineMs = Date.now() + 5000;
  let lockHandle = null;
  while (!lockHandle) {
    try {
      lockHandle = await openFn(lockPath, "wx");
    } catch {
      if (Date.now() >= lockDeadlineMs) {
        throw new Error("timed out waiting for reviewed ledger lock");
      }
      await new Promise((resolve) => setTimeout(resolve, 50));
    }
  }

  try {
    const lanes = {
      codex: createReviewedLaneState(),
    };
    const laneMetadata = {
      codex: createReviewedLaneMetadataState(),
    };

    for (const lane of REVIEWED_LANES) {
      for (const sha of reviewedLaneAnyShas?.[lane] ?? []) {
        addReviewedLaneStatus(lanes, sha, lane, "reviewed");
      }
      for (const sha of reviewedLaneCleanShas?.[lane] ?? []) {
        addReviewedLaneStatus(lanes, sha, lane, "clean");
      }
    }
    for (const sha of reviewedAnyShas ?? reviewedCleanShas ?? []) {
      addReviewedLaneStatus(lanes, sha, "codex", "reviewed");
    }
    for (const sha of reviewedCleanShas ?? []) {
      addReviewedLaneStatus(lanes, sha, "codex", "clean");
    }

    try {
      const existingRaw = await readFileFn(reviewedPath, "utf8");
      const existing = parseReviewedPayload(JSON.parse(existingRaw));
      for (const lane of REVIEWED_LANES) {
        for (const sha of existing.lanes[lane].any) {
          addReviewedLaneStatus(lanes, sha, lane, "reviewed");
        }
        for (const sha of existing.lanes[lane].clean) {
          addReviewedLaneStatus(lanes, sha, lane, "clean");
        }
      }
      for (const [sha, reviewedAt] of existing.laneMetadata.codex
        .reviewedAtBySha) {
        laneMetadata.codex.reviewedAtBySha.set(sha, reviewedAt);
      }
    } catch {}

    for (const lane of REVIEWED_LANES) {
      for (const [sha, status] of overrideEntriesForLane(
        reviewedLaneStatusOverrides,
        lane,
      )) {
        setReviewedLaneStatus(lanes, String(sha ?? "").trim(), lane, status);
      }
    }

    for (const [sha, reviewedAt] of overrideEntriesForLane(
      reviewedLaneReviewedAt,
      "codex",
    )) {
      const normalizedSha = String(sha ?? "").trim();
      const normalizedReviewedAt = String(reviewedAt ?? "").trim();
      if (
        !normalizedSha ||
        !Number.isFinite(Date.parse(normalizedReviewedAt))
      ) {
        continue;
      }
      laneMetadata.codex.reviewedAtBySha.set(
        normalizedSha,
        normalizedReviewedAt,
      );
    }

    const allShas = new Set(lanes.codex.any);
    const reviewed = Array.from(allShas)
      .sort()
      .map((sha) => {
        const codexStatus = lanes.codex.clean.has(sha)
          ? "clean"
          : lanes.codex.any.has(sha)
            ? "reviewed"
            : "";
        const laneStatuses = {
          ...(codexStatus ? { codex: codexStatus } : {}),
        };
        const next = {
          sha,
          ...(codexStatus ? { status: codexStatus } : {}),
        };
        const reviewedAt = laneMetadata.codex.reviewedAtBySha.get(sha) ?? "";
        if (codexStatus === "clean" && reviewedAt) {
          next.reviewed_at = reviewedAt;
        }
        if (Object.keys(laneStatuses).length > 0) {
          next.lanes = laneStatuses;
        }
        return next;
      });
    const payload = {
      schema_version: REVIEWED_SHAS_SCHEMA_VERSION,
      scope: "global",
      reviewed,
      updated_at: new Date().toISOString(),
    };
    const tempPath = `${reviewedPath}.tmp.${process.pid}.${Date.now()}`;
    await writeFileFn(
      tempPath,
      `${JSON.stringify(payload, null, 2)}\n`,
      "utf8",
    );
    await renameFn(tempPath, reviewedPath);
  } finally {
    try {
      await lockHandle?.close();
    } catch {}
    await unlinkFn(lockPath).catch(() => {});
  }
}

export async function readReviewGateState({
  reviewsDir,
  sha,
  minSeverity,
  dismissedSignatures = new Set(),
}) {
  const jsonPath = path.join(reviewsDir, `${sha}.json`);
  const threshold = parseMinSeverity(minSeverity);
  const queueState = await queueStateForSha(reviewsDir, sha);
  const pauseState = await readReviewQueuePause(reviewsDir, "codex");
  const queuePaused = pauseState.paused === true;

  let parsed;
  try {
    const raw = await readFile(jsonPath, "utf8");
    parsed = JSON.parse(raw);
  } catch {
    parsed = null;
  }

  if (!parsed) {
    return {
      sha,
      status: queueState.queued
        ? queueState.stale
          ? "stale-active"
          : queueState.status
        : "missing",
      reason: queueState.queued
        ? queueState.stale
          ? "queue-stale"
          : "queue-pending"
        : "missing-or-invalid-json",
      reviewStatus: "",
      failureReason: "",
      findingsCount: 0,
      worstSeverity: "none",
      actionable: false,
      failed: false,
      blocking: false,
      hasCodexReview: false,
      hasLocalReview: false,
      missingRequiredReviews: false,
      localReviewStatus: "missing",
      hasReportArtifact: false,
      queueStatus: queueState.status,
      queueStale: queueState.stale,
      queuePaused,
    };
  }

  const reviewStatus = String(parsed?.review_status ?? "")
    .trim()
    .toLowerCase();
  const failureReason = String(parsed?.failure_reason ?? "")
    .trim()
    .toLowerCase();
  const codexReviewStatus = String(parsed?.review_engines?.codex?.status ?? "");
  const normalizedCodexStatus = codexReviewStatus.trim().toLowerCase();
  const inferredCodexOkFromReviewStatus =
    reviewStatus === "ok" || reviewStatus === "partial_success";
  const hasCodexReview =
    normalizedCodexStatus.length > 0
      ? normalizedCodexStatus === "ok"
      : inferredCodexOkFromReviewStatus;
  const failed = !hasCodexReview;
  const findings = Array.isArray(parsed?.findings) ? parsed.findings : [];
  const operatorState = await readOperatorState({ reviewsDir });
  const closedSignatures = operatorClosedFindingSignaturesForReport({
    operatorState,
    sha,
    report: parsed,
  });
  const effectiveDismissedSignatures = new Set([
    ...dismissedSignatures,
    ...closedSignatures,
  ]);
  const unresolvedFindings = findings.filter(
    (finding) =>
      !effectiveDismissedSignatures.has(
        normalizeFindingToken(findingSignature(finding)),
      ),
  );
  const { count, worstSeverity } = summarizeFindings(unresolvedFindings);
  const incomplete = isIncompleteCodexReviewReport({
    ...parsed,
    findings: unresolvedFindings,
  });
  const actionable =
    failed ||
    isActionableCodexReviewReport(
      {
        ...parsed,
        findings: unresolvedFindings,
      },
      true,
    );
  const terminalManualTakeover = isTerminalManualTakeoverSha(reviewsDir, sha);
  const blocking =
    !terminalManualTakeover &&
    (failed ||
      incomplete ||
      (count > 0 && severityRank(worstSeverity) >= severityRank(threshold)));

  return {
    sha,
    status: terminalManualTakeover ? "manual-takeover" : "present",
    reason: terminalManualTakeover ? "manual-takeover" : "report-present",
    reviewStatus,
    failureReason,
    findingsCount: count,
    worstSeverity,
    actionable: terminalManualTakeover ? false : actionable,
    failed,
    incomplete,
    blocking,
    hasCodexReview,
    hasLocalReview: false,
    missingRequiredReviews: false,
    localReviewStatus: "missing",
    hasReportArtifact: true,
    queueStatus: queueState.status,
    queueStale: queueState.stale,
    queuePaused,
  };
}

function defaultShouldContinue() {
  return true;
}

function normalizeShaList(value) {
  return (Array.isArray(value) ? value : [])
    .map((sha) =>
      String(sha ?? "")
        .trim()
        .toLowerCase(),
    )
    .filter(Boolean);
}

function normalizeShaBranches(value) {
  const map = new Map();
  const entries =
    value instanceof Map
      ? [...value.entries()]
      : value && typeof value === "object" && !Array.isArray(value)
        ? Object.entries(value)
        : [];
  for (const [sha, branches] of entries) {
    const normalizedSha = String(sha ?? "")
      .trim()
      .toLowerCase();
    if (!normalizedSha) continue;
    const branchSet = new Set(
      (Array.isArray(branches) ? branches : [branches])
        .map((branch) => String(branch ?? "").trim())
        .filter(Boolean),
    );
    if (branchSet.size > 0) {
      map.set(normalizedSha, branchSet);
    }
  }
  return map;
}

function refUpdatesFromPushContext(pushContext) {
  return Array.isArray(pushContext?.refUpdates) ? pushContext.refUpdates : [];
}

export async function executePushGate({
  repoRoot,
  reviewsDir,
  stdinText,
  remoteName = "",
  pushContext = null,
  minSeverity = "major",
  timeoutMs = 900000,
  computeOutgoing = computeOutgoingShas,
  gitExec,
  waitForReview = waitForReviewToSettle,
  readReviewState = readReviewGateState,
  shouldContinue = defaultShouldContinue,
}) {
  const updates =
    refUpdatesFromPushContext(pushContext).length > 0
      ? refUpdatesFromPushContext(pushContext)
      : parseRefUpdates(stdinText);
  const usingDefaultOutgoing = computeOutgoing === computeOutgoingShas;
  const contextOutgoingShas = normalizeShaList(pushContext?.outgoingShas);
  const contextPushTipShas = normalizeShaList(pushContext?.pushTipShas);
  const contextShaBranches = normalizeShaBranches(pushContext?.shaBranches);
  const contextRemoteName = String(pushContext?.remoteName ?? "").trim();
  const effectiveRemoteName = contextRemoteName || remoteName;
  const collected =
    contextOutgoingShas.length === 0 && usingDefaultOutgoing
      ? collectOutgoingByUpdate({
          updates,
          remoteName: effectiveRemoteName,
          gitExec,
        })
      : null;
  const shas =
    contextOutgoingShas.length > 0
      ? contextOutgoingShas
      : collected
        ? collected.orderedShas
        : computeOutgoing({
            updates,
            remoteName: effectiveRemoteName,
            gitExec,
          });
  const shaBranches =
    contextShaBranches.size > 0
      ? contextShaBranches
      : (collected?.shaBranches ?? new Map());
  let headBranchName =
    String(pushContext?.headBranchName ?? "").trim() || "HEAD";
  if (headBranchName === "HEAD") {
    try {
      headBranchName =
        String(gitExec(["rev-parse", "--abbrev-ref", "HEAD"]) ?? "").trim() ||
        "HEAD";
    } catch {
      headBranchName = "HEAD";
    }
  }

  const branchStateCache = new Map();
  const pushedBranchNames = new Set(
    (Array.isArray(pushContext?.pushedBranchNames) &&
    pushContext.pushedBranchNames.length > 0
      ? pushContext.pushedBranchNames
      : updates
          .filter(
            (update) =>
              update.localRef?.startsWith("refs/heads/") &&
              update.localSha !== ZERO_SHA,
          )
          .map((update) => update.localRef.replace(/^refs\/heads\//, ""))
    )
      .map((branchName) => String(branchName ?? "").trim())
      .filter(Boolean),
  );
  const reviewedBranchScopes = new Set([headBranchName, ...pushedBranchNames]);
  for (const branches of shaBranches.values()) {
    for (const branchName of branches) {
      reviewedBranchScopes.add(branchName);
    }
  }
  const reviewedShas = { any: new Set(), clean: new Set() };
  for (const branchName of reviewedBranchScopes) {
    const scopedReviewed = await loadReviewedShas({
      repoRoot,
      branchName,
    });
    for (const sha of scopedReviewed.any) reviewedShas.any.add(sha);
    for (const sha of scopedReviewed.clean) reviewedShas.clean.add(sha);
  }
  const reviewedAnyShas = new Set(reviewedShas.any);
  const reviewedCleanShas = new Set(reviewedShas.clean);
  let reviewedShasDirty = false;
  const pushTipShas = new Set(
    contextPushTipShas.length > 0
      ? contextPushTipShas
      : updates
          .filter(
            (update) =>
              update.localRef?.startsWith("refs/heads/") &&
              update.localSha !== ZERO_SHA,
          )
          .map((update) => update.localSha),
  );
  if (pushTipShas.size === 0) {
    for (const sha of shas) pushTipShas.add(sha);
  }

  const getBranchState = async (branchName) => {
    const scopedBranchName =
      String(branchName || headBranchName).trim() || "HEAD";
    const cached = branchStateCache.get(scopedBranchName);
    if (cached) return cached;
    const dismissedBySha = await loadDismissedFindingSignatures({
      repoRoot,
      branchName: scopedBranchName,
    });
    const entry = {
      branchName: scopedBranchName,
      dismissedBySha,
    };
    branchStateCache.set(scopedBranchName, entry);
    return entry;
  };

  const getScopeBranchesForSha = (sha) => {
    const scoped = shaBranches.get(sha);
    if (scoped && scoped.size > 0) return Array.from(scoped);
    return [headBranchName];
  };

  let runningWaited = 0;
  let actionable = 0;
  let failed = 0;
  const blocked = [];

  for (const sha of shas) {
    if (!shouldContinue()) {
      break;
    }

    const waitResult = await waitForReview({
      reviewsDir,
      sha,
      timeoutMs,
    });
    if (waitResult.waited) {
      runningWaited += 1;
    }

    const scopeBranches = getScopeBranchesForSha(sha);
    const branchStates = await Promise.all(
      scopeBranches.map((branchName) => getBranchState(branchName)),
    );
    const dismissedSignatures = new Set();
    for (const branchState of branchStates) {
      for (const key of [sha, `codex:${sha}`]) {
        const scopedDismissals = branchState.dismissedBySha.get(key);
        if (!scopedDismissals) continue;
        for (const signature of scopedDismissals) {
          dismissedSignatures.add(signature);
        }
      }
    }
    let state = await readReviewState({
      reviewsDir,
      sha,
      minSeverity,
      dismissedSignatures,
    });

    if (waitResult.timedOut) {
      blocked.push({
        sha,
        status: "in-progress",
        severity: "none",
        findings: 0,
        reason: "review-in-progress",
      });
      continue;
    }

    const queueOwnsRepair = state.queuePaused
      ? !state.queueStale && Boolean(state.queueStatus)
      : !state.queueStale && state.queueStatus === "active";

    if (!state.hasReportArtifact || state.missingRequiredReviews) {
      const reason = state.queuePaused
        ? "review-paused"
        : String(state.queueStatus ?? "").trim() === "pending" &&
            state.queueStale !== true
          ? "review-pending"
          : queueOwnsRepair || waitResult.timedOut
            ? "review-in-progress"
            : "review-report-missing";
      blocked.push({
        sha,
        status: state.queuePaused
          ? "paused"
          : String(state.queueStatus ?? "").trim() === "pending" &&
              state.queueStale !== true
            ? "pending"
            : waitResult.timedOut
              ? "in-progress"
              : state.status || "missing-review",
        severity: "none",
        findings: state.findingsCount ?? 0,
        reason,
      });
      continue;
    }

    if (!reviewedAnyShas.has(sha)) {
      reviewedAnyShas.add(sha);
      reviewedShasDirty = true;
    }

    if (state.actionable) actionable += 1;
    if (state.failed) failed += 1;
    if (!state.blocking && !reviewedCleanShas.has(sha)) {
      reviewedCleanShas.add(sha);
      reviewedShasDirty = true;
    }

    if (state.blocking) {
      blocked.push({
        sha,
        status: state.reviewStatus || "unknown",
        severity: state.worstSeverity,
        findings: state.findingsCount,
        reason: state.failed
          ? state.failureReason
            ? `review-status-${state.failureReason}`
            : "review-status-failed"
          : state.incomplete
            ? state.failureReason
              ? `review-incomplete-${state.failureReason}`
              : "review-incomplete"
            : `severity-threshold-${parseMinSeverity(minSeverity)}`,
      });
      continue;
    }
  }

  if (reviewedShasDirty) {
    try {
      await saveReviewedShas({
        repoRoot,
        reviewedAnyShas,
        reviewedCleanShas,
      });
    } catch {
      // Best-effort persistence only; never fail pushes on sidecar state writes.
    }
  }

  return {
    shas,
    summary: {
      shas: shas.length,
      running_waited: runningWaited,
      sync_reruns: 0,
      actionable,
      failed,
      blocked: blocked.length,
    },
    blocked,
  };
}
