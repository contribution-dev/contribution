import { spawnSync } from "node:child_process";
import { mkdir, readdir, rename, rm, stat } from "node:fs/promises";
import path from "node:path";
import { reviewSeverityRank } from "./review-severity.mjs";

export const REVIEW_QUEUE_SCHEMA_VERSION = 1;
export const REVIEW_QUEUE_STATUSES = ["pending", "active"];
export const REVIEW_QUEUE_LANES = ["codex"];
export const LEGACY_REVIEW_QUEUE_STATUSES = [
  "pending",
  "active",
  "completed",
  "failed",
];
export const DEFAULT_STALE_AFTER_MS = 10 * 60 * 1000;
export const NON_ACTIONABLE_SUMMARY_PATTERN =
  /no commit-scoped bugs|no commit-scoped correctness issues|no actionable findings|no high-confidence behavioral regressions|no concrete bugs|no commit-scoped bugs or regressions found/i;
export const CODE_REVIEW_ROOT_DIRNAME = ".code-reviews";
export const LEGACY_CODE_REVIEW_ROOT_DIRNAME = path.join(".codex", "reviews");
export const INCOMPLETE_REVIEW_STATUSES = ["partial_success"];

const SHA_PATTERN = /^[0-9a-f]{40}$/;

export function isoNow() {
  return new Date().toISOString();
}

export function isValidReviewSha(sha) {
  return SHA_PATTERN.test(String(sha ?? "").trim());
}

export function defaultReviewsDir(repoRoot) {
  return path.join(repoRoot, CODE_REVIEW_ROOT_DIRNAME);
}

export function legacyReviewsDir(repoRoot) {
  return path.join(repoRoot, LEGACY_CODE_REVIEW_ROOT_DIRNAME);
}

export function resolveReviewRootOverride(preferredDir = "") {
  const explicit = String(preferredDir || "").trim();
  if (explicit) return explicit;
  const envPreferred = String(process.env.CODE_REVIEW_DIR ?? "").trim();
  if (envPreferred) return envPreferred;
  const legacyPreferred = String(process.env.CODEX_REVIEW_DIR ?? "").trim();
  if (legacyPreferred) return legacyPreferred;
  return "";
}

export function normalizeReviewRootOverride(
  repoRoot,
  override = "",
  cwd = process.cwd(),
) {
  const value = String(override ?? "").trim();
  if (!value) {
    return "";
  }
  const resolved = path.isAbsolute(value) ? value : path.resolve(cwd, value);
  const normalizedResolved = path.resolve(resolved);
  for (const rootDir of [
    defaultReviewsDir(repoRoot),
    legacyReviewsDir(repoRoot),
  ]) {
    const normalizedRoot = path.resolve(rootDir);
    const relative = path.relative(normalizedRoot, normalizedResolved);
    if (!relative || relative.startsWith("..") || path.isAbsolute(relative)) {
      continue;
    }
    const segments = relative.split(path.sep).filter(Boolean);
    if (
      segments.length > 0 &&
      segments.every((segment) => segment === "logs")
    ) {
      return normalizedRoot;
    }
  }
  return normalizedResolved;
}

export function resolveRepoRoot(cwd = process.cwd()) {
  const fallback = path.resolve(String(cwd || process.cwd()));
  const result = spawnSync("git", ["rev-parse", "--show-toplevel"], {
    cwd: fallback,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "ignore"],
  });
  if (result.status !== 0) {
    return fallback;
  }
  const resolved = String(result.stdout ?? "").trim();
  return resolved || fallback;
}

async function mergeLegacyStatusDir(sourceDir, targetDir) {
  const sourceInfo = await stat(sourceDir).catch(() => null);
  if (!sourceInfo?.isDirectory()) {
    return;
  }
  await mkdir(targetDir, { recursive: true, mode: 0o700 }).catch(() => {});
  const entries = await readdir(sourceDir).catch(() => []);
  for (const entry of entries) {
    const sourcePath = path.join(sourceDir, entry);
    const targetPath = path.join(targetDir, entry);
    const targetInfo = await stat(targetPath).catch(() => null);
    if (!targetInfo) {
      await rename(sourcePath, targetPath).catch(() => {});
    }
  }
  const remaining = await readdir(sourceDir).catch(() => []);
  if (remaining.length === 0) {
    await rm(sourceDir, { recursive: true, force: true }).catch(() => {});
  }
}

export async function migrateLegacyReviewQueueLayout(reviewsDir, lane) {
  if (lane !== "codex") {
    return;
  }
  const legacyRoot = path.join(reviewsDir, "queue");
  for (const status of LEGACY_REVIEW_QUEUE_STATUSES) {
    const legacyDir = path.join(legacyRoot, status);
    const legacyInfo = await stat(legacyDir).catch(() => null);
    if (!legacyInfo?.isDirectory()) {
      continue;
    }
    const targetDir = path.join(reviewsDir, "queue", "codex", status);
    const targetInfo = await stat(targetDir).catch(() => null);
    await mkdir(path.dirname(targetDir), {
      recursive: true,
      mode: 0o700,
    }).catch(() => {});
    if (!targetInfo) {
      await rename(legacyDir, targetDir).catch(() => {});
      continue;
    }
    if (targetInfo.isDirectory()) {
      await mergeLegacyStatusDir(legacyDir, targetDir);
    }
  }
}

export function parseBacklogMeta(markdown) {
  const trimmed = String(markdown ?? "");
  const match = trimmed.match(
    /<!-- CODEX_REVIEW_META_START\s*([\s\S]*?)\s*CODEX_REVIEW_META_END -->/,
  );
  if (!match?.[1]) {
    return parseStructuredBacklogMarkdown(trimmed);
  }
  try {
    const parsed = JSON.parse(match[1]);
    return {
      summary: String(parsed?.summary ?? ""),
      findings: Array.isArray(parsed?.findings) ? parsed.findings : [],
    };
  } catch {
    return parseStructuredBacklogMarkdown(trimmed);
  }
}

export function normalizeBacklogMeta(value) {
  const summary = String(value?.summary ?? "").trim();
  const findings = Array.isArray(value?.findings) ? value.findings : [];
  return { summary, findings };
}

function parseEvidenceLine(line) {
  const match = String(line ?? "")
    .trim()
    .match(/^- `(?<location>[^`]+)`(?:\s+[-\u2014]\s*(?<reason>.*))?$/);
  if (!match?.groups?.location) {
    return null;
  }
  const location = match.groups.location.trim();
  const reason = String(match.groups.reason ?? "").trim();
  const lastColon = location.lastIndexOf(":");
  return {
    file: lastColon === -1 ? location : location.slice(0, lastColon),
    lines: lastColon === -1 ? "" : location.slice(lastColon + 1),
    reason,
  };
}

function parseStructuredBacklogMarkdown(markdown) {
  const lines = String(markdown ?? "").split(/\r?\n/);
  const findings = [];
  let summary = "";
  let currentFinding = null;
  let inFindingsSection = false;
  let inReviewStatusSection = false;
  let collectingEvidence = false;
  const summaryLines = [];

  const finishFinding = () => {
    if (!currentFinding) {
      return;
    }
    findings.push({
      severity: String(currentFinding.severity ?? "").toLowerCase(),
      confidence: Number(currentFinding.confidence ?? 0),
      title: String(currentFinding.title ?? "").trim(),
      finding_id: String(currentFinding.finding_id ?? "").trim(),
      hypothesis: String(currentFinding.hypothesis ?? "").trim(),
      impact: String(currentFinding.impact ?? "").trim(),
      failure_scenario: String(currentFinding.failure_scenario ?? "").trim(),
      broken_invariant: String(currentFinding.broken_invariant ?? "").trim(),
      evidence: Array.isArray(currentFinding.evidence)
        ? currentFinding.evidence
        : [],
      recommended_direction: String(
        currentFinding.recommended_direction ?? "",
      ).trim(),
      review_pass: String(currentFinding.review_pass ?? "").trim(),
    });
    currentFinding = null;
  };

  for (const rawLine of lines) {
    const line = String(rawLine ?? "");
    const trimmed = line.trim();

    if (trimmed === "## Findings") {
      finishFinding();
      inFindingsSection = true;
      inReviewStatusSection = false;
      collectingEvidence = false;
      continue;
    }
    if (trimmed === "## Review Status") {
      finishFinding();
      inFindingsSection = false;
      inReviewStatusSection = true;
      collectingEvidence = false;
      continue;
    }
    if (!inFindingsSection && !inReviewStatusSection) {
      continue;
    }

    if (inReviewStatusSection) {
      if (trimmed && !trimmed.startsWith("Current status:")) {
        summaryLines.push(trimmed);
      }
      continue;
    }

    const headingMatch = trimmed.match(
      /^### \[(?<id>[^\]]+)\] \[(?<severity>[^\]]+)\] (?<title>.+?) \(confidence (?<confidence>\d+(?:\.\d+)?)\)$/,
    );
    if (headingMatch?.groups) {
      finishFinding();
      currentFinding = {
        finding_id: headingMatch.groups.id,
        severity: headingMatch.groups.severity,
        title: headingMatch.groups.title,
        confidence: headingMatch.groups.confidence,
        hypothesis: "",
        impact: "",
        failure_scenario: "",
        broken_invariant: "",
        evidence: [],
        recommended_direction: "",
        review_pass: "",
      };
      collectingEvidence = false;
      continue;
    }

    if (!currentFinding) {
      continue;
    }

    if (trimmed === "- Evidence:") {
      collectingEvidence = true;
      continue;
    }
    if (collectingEvidence && trimmed.startsWith("- `")) {
      const evidence = parseEvidenceLine(trimmed);
      if (evidence) {
        currentFinding.evidence.push(evidence);
      }
      continue;
    }
    if (trimmed.startsWith("- ")) {
      collectingEvidence = false;
      const bullet = trimmed.slice(2);
      if (bullet.startsWith("Hypothesis: ")) {
        currentFinding.hypothesis = bullet.slice("Hypothesis: ".length);
      } else if (bullet.startsWith("Impact: ")) {
        currentFinding.impact = bullet.slice("Impact: ".length);
      } else if (bullet.startsWith("Failure scenario: ")) {
        currentFinding.failure_scenario = bullet.slice(
          "Failure scenario: ".length,
        );
      } else if (bullet.startsWith("Broken invariant: ")) {
        currentFinding.broken_invariant = bullet.slice(
          "Broken invariant: ".length,
        );
      } else if (bullet.startsWith("Review pass: ")) {
        currentFinding.review_pass = bullet.slice("Review pass: ".length);
      } else if (bullet.startsWith("Recommended direction: ")) {
        currentFinding.recommended_direction = bullet.slice(
          "Recommended direction: ".length,
        );
      }
    }
  }

  finishFinding();
  summary = summaryLines.join("\n").trim();
  return { summary, findings };
}

function severityRank(severity) {
  return reviewSeverityRank(severity);
}

function severityLabel(severity) {
  const normalized = String(severity ?? "")
    .trim()
    .toLowerCase();
  if (normalized === "blocker") return "blocker";
  if (normalized === "major") return "major";
  if (normalized === "minor") return "minor";
  return "";
}

function normalizeConfidenceValue(value) {
  const normalized = Number(value ?? 0);
  return Number.isFinite(normalized) ? normalized : 0;
}

export function summarizeFindingPriorityFromMeta(meta) {
  const findings = Array.isArray(meta?.findings) ? meta.findings : [];
  return findings.reduce(
    (summary, finding) => {
      const nextSeverity = severityRank(finding?.severity);
      const nextSeverityLabel = severityLabel(finding?.severity);
      const nextConfidence = normalizeConfidenceValue(finding?.confidence);
      if (nextSeverity > summary.highestSeverity) {
        return {
          findingsCount: summary.findingsCount + 1,
          highestSeverity: nextSeverity,
          highestSeverityLabel: nextSeverityLabel,
          highestConfidence: nextConfidence,
        };
      }
      return {
        findingsCount: summary.findingsCount + 1,
        highestSeverity: summary.highestSeverity,
        highestSeverityLabel:
          nextSeverity === summary.highestSeverity &&
          nextConfidence > summary.highestConfidence &&
          nextSeverityLabel
            ? nextSeverityLabel
            : summary.highestSeverityLabel,
        highestConfidence:
          nextSeverity === summary.highestSeverity &&
          nextConfidence > summary.highestConfidence
            ? nextConfidence
            : summary.highestConfidence,
      };
    },
    {
      findingsCount: 0,
      highestSeverity: 0,
      highestSeverityLabel: "",
      highestConfidence: 0,
    },
  );
}

export function classifyBacklogArtifact({
  filePath,
  markdown,
  reviewStatus = "",
  report = null,
}) {
  const meta =
    report && typeof report === "object" && !Array.isArray(report)
      ? normalizeBacklogMeta(report)
      : parseBacklogMeta(markdown);
  const priority = summarizeFindingPriorityFromMeta(meta);
  const summary = String(meta.summary ?? "").trim();
  const incompleteReview =
    priority.findingsCount === 0 &&
    isActionableCodexReviewReport(
      {
        findings: meta.findings,
        review_status: reviewStatus,
      },
      true,
    );
  const contradictory =
    priority.findingsCount > 0 && NON_ACTIONABLE_SUMMARY_PATTERN.test(summary);

  let category = "clear";
  if (contradictory) {
    category = "contradictory";
  } else if (priority.findingsCount > 0) {
    category = "actionable";
  } else if (incompleteReview) {
    category = "actionable";
  }

  return {
    filePath,
    summary,
    findings: meta.findings,
    findingsCount: priority.findingsCount,
    highestSeverity: priority.highestSeverity,
    highestSeverityLabel: priority.highestSeverityLabel,
    highestConfidence: priority.highestConfidence,
    category,
  };
}

export function normalizeReviewTrigger(trigger) {
  const value = String(trigger ?? "").trim();
  if (value === "post-commit" || value === "post-push" || value === "manual") {
    return value;
  }
  return "manual";
}

export function normalizeReviewReasons(reasons) {
  if (!Array.isArray(reasons)) {
    return [];
  }
  return reasons
    .flatMap((reason) =>
      String(reason ?? "")
        .split(",")
        .map((part) => part.trim()),
    )
    .filter(Boolean);
}

export function normalizeReviewQueueLane(lane = "codex") {
  const normalizedLane = String(lane ?? "")
    .trim()
    .toLowerCase();
  return REVIEW_QUEUE_LANES.includes(normalizedLane) ? normalizedLane : "codex";
}

export function normalizedJobReasons(job) {
  return Array.from(
    new Set(
      [
        job?.reason_last,
        ...(Array.isArray(job?.reasons_seen) ? job.reasons_seen : []),
      ]
        .flatMap((value) =>
          String(value ?? "")
            .split(",")
            .map((part) => part.trim().toLowerCase()),
        )
        .filter(Boolean),
    ),
  );
}

export function hasRepairPriorityReason(job) {
  return normalizedJobReasons(job).some(
    (reason) =>
      reason === "missing-report" ||
      reason.startsWith("review-") ||
      reason.startsWith("queue-"),
  );
}

export function hasStalePriorityReason(job) {
  return String(job?.reason_last ?? "")
    .trim()
    .toLowerCase()
    .startsWith("stale-");
}

export function shouldPreserveCurrentStaleReason(job, nextReason, force) {
  return hasStalePriorityReason(job) && !nextReason && force !== true;
}

export function pendingJobPriority(job) {
  const trigger = normalizeReviewTrigger(job?.trigger_last);
  if (hasStalePriorityReason(job)) {
    return 4;
  }
  if (trigger === "post-push") {
    return 0;
  }
  if (job?.force_run === true) {
    return 1;
  }
  if (hasRepairPriorityReason(job)) {
    return 2;
  }
  if (trigger === "post-commit") {
    return 3;
  }
  if (
    String(job?.source_last ?? "")
      .trim()
      .toLowerCase() === "backfill"
  ) {
    return 6;
  }
  return 5;
}

export function comparePendingJobsForClaim(left, right) {
  const leftPriority = pendingJobPriority(left.job);
  const rightPriority = pendingJobPriority(right.job);
  if (leftPriority !== rightPriority) {
    return leftPriority - rightPriority;
  }
  const leftRetryAfterAt = String(left.job.retry_after_at ?? "");
  const rightRetryAfterAt = String(right.job.retry_after_at ?? "");
  if (leftRetryAfterAt !== rightRetryAfterAt) {
    if (!leftRetryAfterAt) return -1;
    if (!rightRetryAfterAt) return 1;
    return leftRetryAfterAt.localeCompare(rightRetryAfterAt);
  }
  const leftEnqueuedAt = String(left.job.enqueued_at ?? "");
  const rightEnqueuedAt = String(right.job.enqueued_at ?? "");
  if (leftEnqueuedAt !== rightEnqueuedAt) {
    return leftEnqueuedAt.localeCompare(rightEnqueuedAt);
  }
  return String(left.job.sha ?? "").localeCompare(String(right.job.sha ?? ""));
}

export function createDefaultReviewQueueLaneStates() {
  return {
    codex: {
      status: "pending",
      error_code: "none",
      attempts: 0,
      completed: false,
      started_at: "",
      completed_at: "",
      model: "codex-cli",
    },
  };
}

export function normalizeReviewQueueLaneStates(existing) {
  const defaults = createDefaultReviewQueueLaneStates();
  const codex =
    existing?.codex && typeof existing.codex === "object" ? existing.codex : {};
  return {
    codex: {
      ...defaults.codex,
      ...codex,
    },
  };
}

export function createReviewQueueBaseJob({
  sha,
  trigger,
  source = "unknown",
  reason = "",
  lane = "codex",
}) {
  const now = isoNow();
  return {
    schema_version: REVIEW_QUEUE_SCHEMA_VERSION,
    sha,
    lane: normalizeReviewQueueLane(lane),
    status: "pending",
    trigger_last: normalizeReviewTrigger(trigger),
    triggers_seen: [normalizeReviewTrigger(trigger)],
    source_last: String(source ?? "").trim() || "unknown",
    reason_last: String(reason ?? "").trim(),
    reasons_seen: String(reason ?? "").trim() ? [String(reason).trim()] : [],
    created_at: now,
    enqueued_at: now,
    updated_at: now,
    retry_after_at: "",
    started_at: "",
    completed_at: "",
    attempts: 0,
    last_exit_code: null,
    last_error: "",
    force_run: false,
    queue_version: 2,
    worker: {
      id: "",
      pid: 0,
      host: "",
      claimed_at: "",
      heartbeat_at: "",
    },
    lane_states: createDefaultReviewQueueLaneStates(),
  };
}

export function normalizeReviewQueueJob(parsed, statusHint = "") {
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    return null;
  }
  const sha = String(parsed.sha ?? "").trim();
  if (!isValidReviewSha(sha)) return null;
  const status = LEGACY_REVIEW_QUEUE_STATUSES.includes(
    String(parsed.status ?? ""),
  )
    ? String(parsed.status)
    : statusHint || "pending";
  const triggersSeen = Array.isArray(parsed.triggers_seen)
    ? parsed.triggers_seen
        .map((value) => normalizeReviewTrigger(value))
        .filter(Boolean)
    : [];
  const reasonsSeen = Array.isArray(parsed.reasons_seen)
    ? parsed.reasons_seen
        .map((value) => String(value ?? "").trim())
        .filter(Boolean)
    : [];
  return {
    ...createReviewQueueBaseJob({
      sha,
      trigger: normalizeReviewTrigger(
        parsed.trigger_last ?? triggersSeen[0] ?? "manual",
      ),
      source: String(parsed.source_last ?? "unknown"),
      reason: String(parsed.reason_last ?? ""),
      lane: normalizeReviewQueueLane(parsed.lane ?? "codex"),
    }),
    ...parsed,
    sha,
    status,
    trigger_last: normalizeReviewTrigger(
      parsed.trigger_last ?? triggersSeen[0] ?? "manual",
    ),
    triggers_seen:
      triggersSeen.length > 0 ? Array.from(new Set(triggersSeen)) : ["manual"],
    reasons_seen: Array.from(new Set(reasonsSeen)),
    lane: normalizeReviewQueueLane(parsed.lane ?? "codex"),
    force_run: parsed.force_run === true,
    retry_after_at: normalizeRetryAfterAt(parsed.retry_after_at),
    worker:
      parsed.worker &&
      typeof parsed.worker === "object" &&
      !Array.isArray(parsed.worker)
        ? {
            id: String(parsed.worker.id ?? "").trim(),
            pid: Number.parseInt(String(parsed.worker.pid ?? "0"), 10) || 0,
            host: String(parsed.worker.host ?? "").trim(),
            claimed_at: String(parsed.worker.claimed_at ?? "").trim(),
            heartbeat_at: String(parsed.worker.heartbeat_at ?? "").trim(),
          }
        : {
            id: "",
            pid: 0,
            host: "",
            claimed_at: "",
            heartbeat_at: "",
          },
    lane_states: normalizeReviewQueueLaneStates(parsed.lane_states),
  };
}

export function normalizeRetryAfterAt(value) {
  const normalized = String(value ?? "").trim();
  return Number.isFinite(Date.parse(normalized)) ? normalized : "";
}

export function isReviewJobRetryDeferred(job, nowMs = Date.now()) {
  const retryAfterMs = Date.parse(String(job?.retry_after_at ?? ""));
  return Number.isFinite(retryAfterMs) && nowMs < retryAfterMs;
}

export function normalizeReviewQueuePauseState(parsed, lane = "codex") {
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    return {
      paused: false,
      lane: normalizeReviewQueueLane(lane),
      reason: "",
      message: "",
      sha: "",
      paused_at: "",
      updated_at: "",
      resume_after_at: "",
      paused_by: "",
    };
  }
  return {
    paused: parsed.paused === true,
    lane: normalizeReviewQueueLane(parsed.lane ?? lane),
    reason: String(parsed.reason ?? "").trim(),
    message: String(parsed.message ?? "").trim(),
    sha: isValidReviewSha(parsed.sha) ? String(parsed.sha).trim() : "",
    paused_at: String(parsed.paused_at ?? "").trim(),
    updated_at: String(parsed.updated_at ?? "").trim(),
    resume_after_at: String(parsed.resume_after_at ?? "").trim(),
    paused_by: String(parsed.paused_by ?? "").trim(),
  };
}

export function normalizeReviewQueueBacklogState(parsed, lane = "codex") {
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    return {
      active: false,
      lane: normalizeReviewQueueLane(lane),
      enqueue_after: "",
      reason: "",
      activated_at: "",
      updated_at: "",
    };
  }
  const enqueueAfter = String(parsed.enqueue_after ?? "").trim();
  return {
    active: parsed.active === true && Number.isFinite(Date.parse(enqueueAfter)),
    lane: normalizeReviewQueueLane(parsed.lane ?? lane),
    enqueue_after: Number.isFinite(Date.parse(enqueueAfter))
      ? enqueueAfter
      : "",
    reason: String(parsed.reason ?? "").trim(),
    activated_at: String(parsed.activated_at ?? "").trim(),
    updated_at: String(parsed.updated_at ?? "").trim(),
  };
}

export function isQueueRecoveryReport(report) {
  const findingModels = Array.isArray(report?.finding_models)
    ? report.finding_models
    : [];
  return findingModels.some(
    (value) => String(value ?? "").trim() === "queue-recovery",
  );
}

export function isNonFindingCodexReviewStatusActionable(reviewStatus) {
  const normalized = String(reviewStatus ?? "")
    .trim()
    .toLowerCase();
  if (!normalized || normalized === "ok") {
    return false;
  }
  return normalized !== "infra_error";
}

export function isActionableCodexReviewReport(report, includeFailed = false) {
  const findings = Array.isArray(report?.findings) ? report.findings : [];
  if (findings.length > 0) return true;
  return Boolean(
    includeFailed &&
    isNonFindingCodexReviewStatusActionable(report?.review_status),
  );
}

export function isIncompleteCodexReviewReport(report) {
  if (!report || typeof report !== "object" || Array.isArray(report)) {
    return false;
  }
  const findings = Array.isArray(report?.findings) ? report.findings : [];
  if (findings.length > 0) {
    return false;
  }
  const reviewStatus = String(report?.review_status ?? "")
    .trim()
    .toLowerCase();
  return (
    reviewStatus === "partial_success" && reportSatisfiesLane(report, "codex")
  );
}

export function reportSatisfiesCanonicalCodexLane(report) {
  if (!report || typeof report !== "object" || Array.isArray(report)) {
    return false;
  }
  const findings = Array.isArray(report?.findings) ? report.findings : [];
  if (findings.length > 0) {
    return false;
  }
  const reviewStatus = String(report?.review_status ?? "")
    .trim()
    .toLowerCase();
  return reviewStatus === "ok" && reportSatisfiesLane(report, "codex");
}

export function reportSatisfiesLane(report, lane = "codex") {
  if (!report || typeof report !== "object" || Array.isArray(report)) {
    return false;
  }
  const reviewStatus = String(report?.review_status ?? "")
    .trim()
    .toLowerCase();
  const codexStatus = String(report?.review_engines?.codex?.status ?? "")
    .trim()
    .toLowerCase();
  if (codexStatus) {
    return (
      codexStatus === "ok" && ["ok", "partial_success"].includes(reviewStatus)
    );
  }
  return ["ok", "partial_success"].includes(reviewStatus);
}

export function hasDurableReviewEvidence(report, lane = "codex") {
  if (reportSatisfiesLane(report, lane)) {
    return true;
  }
  return Array.isArray(report?.findings) && report.findings.length > 0;
}

export function isSyntheticFailurePlaceholder(report, lane = "codex") {
  if (!report || typeof report !== "object" || Array.isArray(report)) {
    return false;
  }
  if (isQueueRecoveryReport(report)) {
    return true;
  }
  if (hasDurableReviewEvidence(report, lane)) {
    return false;
  }
  const reviewStatus = String(report?.review_status ?? "")
    .trim()
    .toLowerCase();
  return Boolean(reviewStatus && reviewStatus !== "ok");
}

export function isReviewQueueJobStale({
  status,
  job,
  isWorkerAlive,
  nowMs = Date.now(),
  staleAfterMs = DEFAULT_STALE_AFTER_MS,
}) {
  const heartbeatRaw =
    job?.worker?.heartbeat_at ||
    job?.updated_at ||
    job?.started_at ||
    job?.enqueued_at;
  const heartbeatMs = Date.parse(String(heartbeatRaw ?? ""));
  const workerPid = Number.parseInt(String(job?.worker?.pid ?? "0"), 10);
  const hasWorkerPid = Number.isInteger(workerPid) && workerPid > 0;
  const workerAlive =
    status === "active" && hasWorkerPid ? isWorkerAlive(workerPid) : false;
  const stale =
    status === "active" &&
    (!Number.isFinite(heartbeatMs) ||
      nowMs - heartbeatMs > staleAfterMs ||
      (hasWorkerPid && !workerAlive));
  return {
    workerAlive,
    stale,
  };
}
