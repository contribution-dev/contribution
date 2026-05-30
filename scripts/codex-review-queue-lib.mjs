#!/usr/bin/env node

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
import { writeReviewArtifacts } from "./lib/codex-review-findings.mjs";
import {
  computeReviewPauseResumeAt,
  shouldAutoClearReviewPauseState,
} from "./lib/codex-review-failure.mjs";
import {
  comparePendingJobsForClaim,
  createDefaultReviewQueueLaneStates,
  createReviewQueueBaseJob,
  DEFAULT_STALE_AFTER_MS,
  isoNow,
  isReviewQueueJobStale,
  isReviewJobRetryDeferred,
  isValidReviewSha,
  normalizeRetryAfterAt,
  normalizeReviewQueueBacklogState,
  normalizeReviewQueueJob,
  normalizeReviewQueueLane,
  normalizeReviewQueuePauseState,
  normalizeReviewReasons,
  normalizeReviewTrigger,
  reportSatisfiesLane,
  REVIEW_QUEUE_LANES,
  REVIEW_QUEUE_STATUSES,
  shouldPreserveCurrentStaleReason,
} from "./lib/codex-review-state.mjs";

export {
  DEFAULT_STALE_AFTER_MS,
  REVIEW_QUEUE_LANES,
  REVIEW_QUEUE_SCHEMA_VERSION,
  REVIEW_QUEUE_STATUSES,
} from "./lib/codex-review-state.mjs";

function queueRoot(reviewsDir, lane = "codex") {
  return path.join(reviewsDir, "queue", normalizeReviewQueueLane(lane));
}

function queuePausePath(reviewsDir, lane = "codex") {
  return path.join(queueRoot(reviewsDir, lane), "pause.json");
}

function queueBacklogPath(reviewsDir, lane = "codex") {
  return path.join(queueRoot(reviewsDir, lane), "backlog.json");
}

function statusDir(reviewsDir, lane, status) {
  return path.join(queueRoot(reviewsDir, lane), status);
}

function jobFilePath(reviewsDir, lane, status, sha) {
  return path.join(statusDir(reviewsDir, lane, status), `${sha}.json`);
}

function allJobPaths(reviewsDir, lane, sha) {
  return REVIEW_QUEUE_STATUSES.map((status) => ({
    status,
    filePath: jobFilePath(reviewsDir, lane, status, sha),
  }));
}

function createBaseJob({
  sha,
  trigger,
  source = "unknown",
  reason = "",
  lane = "codex",
}) {
  return createReviewQueueBaseJob({ sha, trigger, source, reason, lane });
}

async function atomicWriteJson(filePath, payload) {
  const directory = path.dirname(filePath);
  await mkdir(directory, { recursive: true, mode: 0o700 });
  const tempPath = path.join(
    directory,
    `.codex-review-queue.${process.pid}.${Date.now()}.${crypto
      .randomBytes(4)
      .toString("hex")}.tmp`,
  );
  await writeFile(tempPath, `${JSON.stringify(payload, null, 2)}\n`, {
    encoding: "utf8",
    mode: 0o600,
  });
  await rename(tempPath, filePath);
}

async function readJson(filePath) {
  const raw = await readFile(filePath, "utf8");
  return JSON.parse(raw);
}

async function readLaneReport(reviewsDir, sha, lane = "codex") {
  try {
    return await readJson(path.join(reviewsDir, `${sha}.json`));
  } catch {
    return null;
  }
}

function normalizeJob(parsed, statusHint = "") {
  return normalizeReviewQueueJob(parsed, statusHint);
}

function updatePendingMetadata(job, { trigger, source, reason, force }) {
  const next = { ...job };
  const normalizedReason = String(reason ?? "").trim();
  next.status = "pending";
  next.trigger_last = normalizeReviewTrigger(trigger);
  next.updated_at = isoNow();
  next.enqueued_at = next.updated_at;
  next.retry_after_at =
    force === true ? "" : normalizeRetryAfterAt(job.retry_after_at);
  next.source_last =
    String(source ?? "").trim() || next.source_last || "unknown";
  next.reason_last = shouldPreserveCurrentStaleReason(
    job,
    normalizedReason,
    force,
  )
    ? String(job?.reason_last ?? "").trim()
    : normalizedReason;
  next.triggers_seen = Array.from(
    new Set([
      ...(Array.isArray(next.triggers_seen) ? next.triggers_seen : []),
      next.trigger_last,
    ]),
  );
  if (next.reason_last) {
    next.reasons_seen = Array.from(
      new Set([
        ...normalizeReviewReasons(next.reasons_seen),
        ...normalizeReviewReasons([next.reason_last]),
      ]),
    );
  }
  return next;
}

function workerIdentity(overrides = {}) {
  return {
    id:
      String(overrides.id ?? "").trim() ||
      `worker-${process.pid}-${crypto.randomBytes(4).toString("hex")}`,
    pid:
      Number.isInteger(overrides.pid) && overrides.pid > 0
        ? overrides.pid
        : process.pid,
    host:
      String(overrides.host ?? "").trim() ||
      process.env.HOSTNAME ||
      process.env.COMPUTERNAME ||
      "localhost",
  };
}

async function locateJob(reviewsDir, sha, lane = "codex") {
  const normalizedLane = normalizeReviewQueueLane();
  for (const candidate of allJobPaths(reviewsDir, normalizedLane, sha)) {
    try {
      const parsed = normalizeJob(
        await readJson(candidate.filePath),
        candidate.status,
      );
      if (!parsed) continue;
      return {
        status: candidate.status,
        filePath: candidate.filePath,
        lane: normalizedLane,
        job: parsed,
      };
    } catch {}
  }
  return null;
}

export async function ensureReviewQueue(reviewsDir, lane = "codex") {
  const normalizedLane = normalizeReviewQueueLane(lane);
  const root = queueRoot(reviewsDir, normalizedLane);
  await mkdir(root, { recursive: true, mode: 0o700 });
  for (const status of REVIEW_QUEUE_STATUSES) {
    await mkdir(statusDir(reviewsDir, normalizedLane, status), {
      recursive: true,
      mode: 0o700,
    });
  }
  return root;
}

export async function enqueueReviewJob({
  reviewsDir,
  sha,
  lane = "codex",
  trigger = "manual",
  source = "unknown",
  reason = "",
  force = false,
}) {
  const normalizedSha = String(sha ?? "")
    .trim()
    .toLowerCase();
  const normalizedLane = normalizeReviewQueueLane(lane);
  if (!isValidReviewSha(normalizedSha)) {
    throw new Error(`Invalid SHA: ${sha}`);
  }
  await ensureReviewQueue(reviewsDir, lane);
  const existing = await locateJob(reviewsDir, normalizedSha, normalizedLane);
  const pendingPath = jobFilePath(
    reviewsDir,
    normalizedLane,
    "pending",
    normalizedSha,
  );

  if (!existing && !force) {
    const report = await readLaneReport(
      reviewsDir,
      normalizedSha,
      normalizedLane,
    );
    if (reportSatisfiesLane(report, normalizedLane)) {
      return { job: null, status: "completed", enqueued: false, deduped: true };
    }
  }

  if (!existing) {
    const created = createBaseJob({
      sha: normalizedSha,
      lane: normalizedLane,
      trigger,
      source,
      reason,
    });
    created.force_run = force === true;
    await atomicWriteJson(pendingPath, created);
    return { job: created, status: "pending", enqueued: true, deduped: false };
  }

  let nextJob = updatePendingMetadata(existing.job, {
    trigger,
    source,
    reason,
    force,
  });
  nextJob.force_run = existing.job.force_run === true || force === true;
  let nextPath = existing.filePath;
  let enqueued = false;
  if (existing.status === "active") {
    nextJob.status = "active";
    await atomicWriteJson(nextPath, nextJob);
    return { job: nextJob, status: existing.status, enqueued, deduped: true };
  }

  if (existing.status !== "pending") {
    const report = await readLaneReport(
      reviewsDir,
      normalizedSha,
      normalizedLane,
    );
    const laneSatisfied =
      report &&
      typeof report === "object" &&
      !Array.isArray(report) &&
      reportSatisfiesLane(report, normalizedLane);
    const shouldRequeue = force || !laneSatisfied;
    if (!shouldRequeue) {
      await atomicWriteJson(nextPath, {
        ...existing.job,
        updated_at: isoNow(),
      });
      return {
        job: existing.job,
        status: existing.status,
        enqueued,
        deduped: true,
      };
    }
    nextPath = pendingPath;
    nextJob.status = "pending";
    nextJob.started_at = "";
    nextJob.completed_at = "";
    nextJob.worker = {
      id: "",
      pid: 0,
      host: "",
      claimed_at: "",
      heartbeat_at: "",
    };
    nextJob.last_error = "";
    nextJob.lane_states = createDefaultReviewQueueLaneStates();
    await rm(existing.filePath, { force: true });
    enqueued = true;
  } else {
    enqueued = true;
  }

  await atomicWriteJson(nextPath, nextJob);
  return { job: nextJob, status: nextJob.status, enqueued, deduped: true };
}

export async function listReviewJobs(
  reviewsDir,
  statuses = REVIEW_QUEUE_STATUSES,
  { lane = "codex" } = {},
) {
  const normalizedLane = normalizeReviewQueueLane();
  await ensureReviewQueue(reviewsDir, normalizedLane);
  const result = [];
  for (const status of statuses) {
    const dir = statusDir(reviewsDir, normalizedLane, status);
    const entries = await readdir(dir).catch(() => []);
    for (const entry of entries) {
      if (!entry.endsWith(".json")) continue;
      const filePath = path.join(dir, entry);
      try {
        const job = normalizeJob(await readJson(filePath), status);
        if (!job) continue;
        result.push({
          lane: normalizedLane,
          status,
          filePath,
          job,
        });
      } catch {}
    }
  }
  return result.sort((left, right) =>
    String(left.job.enqueued_at ?? "").localeCompare(
      String(right.job.enqueued_at ?? ""),
    ),
  );
}

function isWorkerAlive(pid) {
  const value = Number.parseInt(String(pid ?? "0"), 10);
  if (!Number.isInteger(value) || value <= 0) return false;
  try {
    process.kill(value, 0);
    return true;
  } catch {
    return false;
  }
}

function resetRunningLaneStates(job) {
  const next = createDefaultReviewQueueLaneStates();
  next.codex = {
    ...next.codex,
    ...(job.lane_states?.codex ?? {}),
  };
  if (next.codex.status === "running") {
    next.codex.status = "pending";
    next.codex.started_at = "";
    next.codex.completed_at = "";
  }
  return next;
}

export async function reclaimStaleActiveJobs(
  reviewsDir,
  {
    lane = "codex",
    staleAfterMs = DEFAULT_STALE_AFTER_MS,
    nowMs = Date.now(),
    beforeRemoveActive = async () => {},
  } = {},
) {
  const normalizedLane = normalizeReviewQueueLane();
  const activeJobs = await listReviewJobs(reviewsDir, ["active"], {
    lane: normalizedLane,
  });
  const reclaimed = [];
  for (const entry of activeJobs) {
    const { stale: staleByTime, workerAlive } = isReviewQueueJobStale({
      status: "active",
      job: entry.job,
      isWorkerAlive,
      nowMs,
      staleAfterMs,
    });
    const staleByPid = entry.job.worker?.pid && !workerAlive;
    if (!staleByTime && !staleByPid) continue;

    const nextJob = {
      ...entry.job,
      status: "pending",
      updated_at: isoNow(),
      reason_last: staleByPid ? "stale-worker-dead" : "stale-heartbeat-timeout",
      reasons_seen: Array.from(
        new Set([
          ...normalizeReviewReasons(entry.job.reasons_seen),
          staleByPid ? "stale-worker-dead" : "stale-heartbeat-timeout",
        ]),
      ),
      worker: {
        id: "",
        pid: 0,
        host: "",
        claimed_at: "",
        heartbeat_at: "",
      },
      lane_states: resetRunningLaneStates(entry.job),
    };
    const pendingPath = jobFilePath(
      reviewsDir,
      normalizedLane,
      "pending",
      entry.job.sha,
    );
    await atomicWriteJson(pendingPath, nextJob);
    await beforeRemoveActive({
      activePath: entry.filePath,
      pendingPath,
      job: nextJob,
    });
    await removeStaleActiveJobFile(entry.filePath, entry.job);
    reclaimed.push(nextJob.sha);
  }
  return reclaimed;
}

async function removeStaleActiveJobFile(filePath, originalJob) {
  let current;
  try {
    current = normalizeJob(await readJson(filePath), "active");
  } catch {
    return false;
  }
  if (!isSameActiveJobForReclaim(current, originalJob)) {
    return false;
  }
  await rm(filePath, { force: true });
  return true;
}

function isSameActiveJobForReclaim(current, original) {
  if (!current || !original) return false;
  return (
    current.status === "active" &&
    current.sha === original.sha &&
    current.worker?.id === original.worker?.id &&
    current.worker?.pid === original.worker?.pid &&
    current.worker?.claimed_at === original.worker?.claimed_at &&
    current.worker?.heartbeat_at === original.worker?.heartbeat_at
  );
}

export async function claimNextReviewJob(
  reviewsDir,
  workerInfo = {},
  lane = "codex",
) {
  const normalizedLane = normalizeReviewQueueLane(lane);
  await ensureReviewQueue(reviewsDir, normalizedLane);
  await reclaimStaleActiveJobs(reviewsDir, { lane: normalizedLane });
  const pauseState = await readReviewQueuePause(reviewsDir, normalizedLane);
  if (pauseState.paused) {
    return null;
  }
  const backlogState = await readReviewQueueBacklog(reviewsDir, normalizedLane);
  const nowMs = Date.now();
  const pendingJobs = (
    await listReviewJobs(reviewsDir, ["pending"], {
      lane: normalizedLane,
    })
  ).sort(comparePendingJobsForClaim);
  const worker = workerIdentity(workerInfo);
  for (const entry of pendingJobs) {
    if (isJobBlockedByBacklog(entry.job, backlogState)) {
      continue;
    }
    if (isReviewJobRetryDeferred(entry.job, nowMs)) {
      continue;
    }
    if (
      await collapseQueuedJobFromExistingReport(
        reviewsDir,
        entry,
        normalizedLane,
      )
    ) {
      continue;
    }
    const activePath = jobFilePath(
      reviewsDir,
      normalizedLane,
      "active",
      entry.job.sha,
    );
    try {
      await rename(entry.filePath, activePath);
    } catch {
      continue;
    }
    const now = isoNow();
    const nextJob = {
      ...entry.job,
      lane: normalizedLane,
      status: "active",
      updated_at: now,
      started_at: now,
      retry_after_at: "",
      attempts: Number(entry.job.attempts ?? 0) + 1,
      worker: {
        id: worker.id,
        pid: worker.pid,
        host: worker.host,
        claimed_at: now,
        heartbeat_at: now,
      },
      lane_states: {
        codex: {
          ...createDefaultReviewQueueLaneStates().codex,
          ...(entry.job.lane_states?.codex ?? {}),
          status: "running",
          started_at: now,
        },
      },
    };
    await atomicWriteJson(activePath, nextJob);
    return {
      lane: normalizedLane,
      status: "active",
      filePath: activePath,
      job: nextJob,
    };
  }
  return null;
}

export async function heartbeatReviewJob(
  reviewsDir,
  sha,
  workerId,
  lane = "codex",
) {
  const existing = await locateJob(reviewsDir, sha, lane);
  if (!existing || existing.status !== "active") return false;
  if (
    workerId &&
    existing.job.worker?.id &&
    existing.job.worker.id !== workerId
  ) {
    return false;
  }
  const nextJob = {
    ...existing.job,
    updated_at: isoNow(),
    worker: {
      ...existing.job.worker,
      heartbeat_at: isoNow(),
    },
  };
  await atomicWriteJson(existing.filePath, nextJob);
  return true;
}

export async function readReviewJob(reviewsDir, sha, lane = "codex") {
  const existing = await locateJob(reviewsDir, sha, lane);
  return existing ? existing.job : null;
}

function laneStatesFromReport(report) {
  const normalized = createDefaultReviewQueueLaneStates();
  const incoming =
    report?.review_engines &&
    typeof report.review_engines === "object" &&
    !Array.isArray(report.review_engines)
      ? report.review_engines
      : {};
  return {
    codex: {
      ...normalized.codex,
      ...(incoming.codex && typeof incoming.codex === "object"
        ? incoming.codex
        : {}),
      status:
        String(incoming.codex?.status ?? "unknown")
          .trim()
          .toLowerCase() || "unknown",
      completed: incoming.codex?.completed === true,
      attempts: Number(incoming.codex?.attempts ?? 0) || 0,
      started_at: String(incoming.codex?.started_at ?? "").trim(),
      completed_at: String(incoming.codex?.completed_at ?? "").trim(),
      model: String(incoming.codex?.model ?? "codex-cli").trim() || "codex-cli",
      error_code: String(incoming.codex?.error_code ?? "none").trim() || "none",
    },
  };
}

async function collapseQueuedJobFromExistingReport(
  reviewsDir,
  entry,
  lane = "codex",
) {
  if (entry.job.force_run === true) {
    return false;
  }
  const report = await readLaneReport(reviewsDir, entry.job.sha, lane);
  if (!reportSatisfiesLane(report, lane)) {
    return false;
  }
  await rm(entry.filePath, { force: true });
  return true;
}

export async function finalizeReviewJob(
  reviewsDir,
  sha,
  {
    lane = "codex",
    workerId = "",
    report = null,
    exitCode = 0,
    failureReason = "",
  } = {},
) {
  const normalizedLane = normalizeReviewQueueLane(lane);
  const existing = await locateJob(reviewsDir, sha, normalizedLane);
  if (!existing) {
    return null;
  }
  if (existing.status !== "active") {
    return existing.job;
  }
  if (
    workerId &&
    existing.job.worker?.id &&
    existing.job.worker.id !== workerId
  ) {
    return existing.job;
  }

  const now = isoNow();
  const hasReport = Boolean(report && typeof report === "object");
  const nextJob = {
    ...existing.job,
    lane: normalizedLane,
    status: hasReport ? "completed" : "failed",
    force_run: false,
    updated_at: now,
    completed_at: now,
    last_exit_code:
      Number.isInteger(exitCode) || typeof exitCode === "number"
        ? exitCode
        : null,
    last_error: String(failureReason || report?.failure_reason || "").trim(),
    worker: {
      ...existing.job.worker,
      heartbeat_at: now,
    },
    lane_states: hasReport
      ? laneStatesFromReport(report)
      : {
          codex: {
            ...createDefaultReviewQueueLaneStates().codex,
            ...(existing.job.lane_states?.codex ?? {}),
            status: "failed",
            error_code: "missing-report",
            completed_at: now,
          },
        },
  };
  await rm(existing.filePath, { force: true });
  return nextJob;
}

export async function requeueReviewJob(
  reviewsDir,
  sha,
  { lane = "codex", workerId = "", reason = "", retryAfterAt = "" } = {},
) {
  const normalizedLane = normalizeReviewQueueLane(lane);
  const existing = await locateJob(reviewsDir, sha, normalizedLane);
  if (!existing) {
    return null;
  }
  if (existing.status !== "active") {
    return existing.job;
  }
  if (
    workerId &&
    existing.job.worker?.id &&
    existing.job.worker.id !== workerId
  ) {
    return null;
  }

  const now = isoNow();
  const nextJob = {
    ...existing.job,
    lane: normalizedLane,
    status: "pending",
    updated_at: now,
    enqueued_at: now,
    retry_after_at: normalizeRetryAfterAt(retryAfterAt),
    worker: {
      id: "",
      pid: 0,
      host: "",
      claimed_at: "",
      heartbeat_at: "",
    },
    trigger_last: normalizeReviewTrigger(existing.job.trigger_last),
    reason_last: reason
      ? String(reason)
      : String(existing.job.reason_last ?? ""),
    reasons_seen: reason
      ? Array.from(
          new Set([
            ...normalizeReviewReasons(existing.job.reasons_seen),
            String(reason),
          ]),
        )
      : normalizeReviewReasons(existing.job.reasons_seen),
    lane_states: resetRunningLaneStates(existing.job),
  };
  const pendingPath = jobFilePath(reviewsDir, normalizedLane, "pending", sha);
  await atomicWriteJson(pendingPath, nextJob);
  await rm(existing.filePath, { force: true });
  return nextJob;
}

export async function collectQueueSummary(reviewsDir, { lane = "all" } = {}) {
  const lanes =
    String(lane ?? "")
      .trim()
      .toLowerCase() === "all"
      ? REVIEW_QUEUE_LANES
      : [normalizeReviewQueueLane(lane)];
  const pauses = Object.fromEntries(
    await Promise.all(
      lanes.map(async (item) => [
        item,
        await readReviewQueuePause(reviewsDir, item),
      ]),
    ),
  );
  const backlogs = Object.fromEntries(
    await Promise.all(
      lanes.map(async (item) => [
        item,
        await readReviewQueueBacklog(reviewsDir, item),
      ]),
    ),
  );
  const jobs = (
    await Promise.all(
      lanes.map((item) =>
        listReviewJobs(reviewsDir, REVIEW_QUEUE_STATUSES, { lane: item }),
      ),
    )
  ).flat();
  const summary = {
    total: jobs.length,
    pending: 0,
    active: 0,
    completed: 0,
    failed: 0,
    oldestPendingAgeSeconds: 0,
    oldestActiveAgeSeconds: 0,
    pauses,
    backlogs,
    lanes: Object.fromEntries(
      lanes.map((item) => [
        item,
        {
          total: 0,
          pending: 0,
          pendingBlockedByBacklog: 0,
          pendingDeferredByRetry: 0,
          active: 0,
          completed: 0,
          failed: 0,
          oldestPendingAgeSeconds: 0,
          oldestActiveAgeSeconds: 0,
        },
      ]),
    ),
    jobs,
  };
  const now = Date.now();
  const oldestPendingByLane = new Map();
  const oldestActiveByLane = new Map();
  const pendingAgeSeconds = (entry) => {
    const enqueuedMs = Date.parse(String(entry?.job?.enqueued_at ?? ""));
    if (!Number.isFinite(enqueuedMs)) {
      return null;
    }
    return Math.max(0, Math.floor((now - enqueuedMs) / 1000));
  };
  const activeAgeSeconds = (entry) => {
    const startedMs = Date.parse(
      String(
        entry?.job?.started_at ||
          entry?.job?.worker?.claimed_at ||
          entry?.job?.updated_at ||
          "",
      ),
    );
    if (!Number.isFinite(startedMs)) {
      return null;
    }
    return Math.max(0, Math.floor((now - startedMs) / 1000));
  };
  const updateOldestAge = (map, currentLane, entry, ageSeconds) => {
    const currentAgeSeconds = ageSeconds(entry);
    if (currentAgeSeconds === null) {
      return;
    }
    const existing = map.get(currentLane);
    if (!existing || currentAgeSeconds > existing.ageSeconds) {
      map.set(currentLane, {
        entry,
        ageSeconds: currentAgeSeconds,
      });
    }
  };
  for (const entry of jobs) {
    summary[entry.status] += 1;
    if (summary.lanes[entry.lane]) {
      summary.lanes[entry.lane].total += 1;
      summary.lanes[entry.lane][entry.status] += 1;
      if (
        entry.status === "pending" &&
        isJobBlockedByBacklog(entry.job, backlogs[entry.lane])
      ) {
        summary.lanes[entry.lane].pendingBlockedByBacklog += 1;
      }
      if (
        entry.status === "pending" &&
        isReviewJobRetryDeferred(entry.job, now)
      ) {
        summary.lanes[entry.lane].pendingDeferredByRetry += 1;
      }
    }
    if (entry.status === "pending") {
      updateOldestAge(
        oldestPendingByLane,
        entry.lane,
        entry,
        pendingAgeSeconds,
      );
    }
    if (entry.status === "active") {
      updateOldestAge(oldestActiveByLane, entry.lane, entry, activeAgeSeconds);
    }
  }
  const oldestPending = [...oldestPendingByLane.values()].sort(
    (left, right) => right.ageSeconds - left.ageSeconds,
  )[0];
  const oldestActive = [...oldestActiveByLane.values()].sort(
    (left, right) => right.ageSeconds - left.ageSeconds,
  )[0];
  if (oldestPending) {
    summary.oldestPendingAgeSeconds = oldestPending.ageSeconds;
  }
  if (oldestActive) {
    summary.oldestActiveAgeSeconds = oldestActive.ageSeconds;
  }
  for (const [currentLane, laneSummary] of Object.entries(summary.lanes)) {
    const oldestPendingForLane = oldestPendingByLane.get(currentLane);
    const oldestActiveForLane = oldestActiveByLane.get(currentLane);
    if (oldestPendingForLane) {
      laneSummary.oldestPendingAgeSeconds = oldestPendingForLane.ageSeconds;
    }
    if (oldestActiveForLane) {
      laneSummary.oldestActiveAgeSeconds = oldestActiveForLane.ageSeconds;
    }
  }
  return summary;
}

export async function readReviewQueuePause(reviewsDir, lane = "codex") {
  const normalizedLane = normalizeReviewQueueLane(lane);
  await ensureReviewQueue(reviewsDir, normalizedLane);
  const filePath = queuePausePath(reviewsDir, normalizedLane);
  const parsed = await readJson(filePath).catch(() => null);
  const normalized = normalizeReviewQueuePauseState(parsed, normalizedLane);
  if (normalized.paused && shouldAutoClearReviewPauseState(normalized)) {
    return normalizeReviewQueuePauseState(null, normalizedLane);
  }
  return normalized;
}

export async function readReviewQueueBacklog(reviewsDir, lane = "codex") {
  const normalizedLane = normalizeReviewQueueLane(lane);
  await ensureReviewQueue(reviewsDir, normalizedLane);
  const filePath = queueBacklogPath(reviewsDir, normalizedLane);
  const parsed = await readJson(filePath).catch(() => null);
  return normalizeReviewQueueBacklogState(parsed, normalizedLane);
}

export async function pauseReviewQueue(
  reviewsDir,
  {
    lane = "codex",
    reason = "",
    message = "",
    sha = "",
    pausedBy = "",
    resumeAfterAt = "",
  } = {},
) {
  const normalizedLane = normalizeReviewQueueLane(lane);
  await ensureReviewQueue(reviewsDir, normalizedLane);
  const now = isoNow();
  const existing = await readReviewQueuePause(reviewsDir, normalizedLane);
  const payload = {
    paused: true,
    lane: normalizedLane,
    reason: String(reason ?? "").trim(),
    message: String(message ?? "").trim(),
    sha: isValidReviewSha(sha) ? String(sha).trim() : existing.sha,
    paused_at: existing.paused_at || now,
    updated_at: now,
    resume_after_at: computeReviewPauseResumeAt(
      {
        ...existing,
        paused: true,
        reason: String(reason ?? "").trim(),
        updated_at: now,
        paused_at: existing.paused_at || now,
      },
      { resumeAfterAt },
    ),
    paused_by: String(pausedBy ?? "").trim(),
  };
  await atomicWriteJson(queuePausePath(reviewsDir, normalizedLane), payload);
  return payload;
}

export async function clearReviewQueuePause(reviewsDir, lane = "codex") {
  const normalizedLane = normalizeReviewQueueLane(lane);
  await rm(queuePausePath(reviewsDir, normalizedLane), { force: true });
}

export async function setReviewQueueBacklog(
  reviewsDir,
  { lane = "codex", enqueueAfter = "", reason = "" } = {},
) {
  const normalizedLane = normalizeReviewQueueLane(lane);
  await ensureReviewQueue(reviewsDir, normalizedLane);
  const parsedEnqueueAfter = Date.parse(String(enqueueAfter ?? "").trim());
  if (!Number.isFinite(parsedEnqueueAfter)) {
    throw new Error(`Invalid enqueueAfter: ${enqueueAfter}`);
  }
  const now = isoNow();
  const existing = await readReviewQueueBacklog(reviewsDir, normalizedLane);
  const payload = {
    active: true,
    lane: normalizedLane,
    enqueue_after: new Date(parsedEnqueueAfter).toISOString(),
    reason: String(reason ?? "").trim(),
    activated_at: existing.activated_at || now,
    updated_at: now,
  };
  await atomicWriteJson(queueBacklogPath(reviewsDir, normalizedLane), payload);
  return payload;
}

export async function clearReviewQueueBacklog(reviewsDir, lane = "codex") {
  const normalizedLane = normalizeReviewQueueLane(lane);
  await rm(queueBacklogPath(reviewsDir, normalizedLane), { force: true });
}

function isJobBlockedByBacklog(job, backlogState) {
  if (!backlogState?.active) {
    return false;
  }
  const enqueueAfterMs = Date.parse(String(backlogState.enqueue_after ?? ""));
  const enqueuedAtMs = Date.parse(String(job?.enqueued_at ?? ""));
  if (!Number.isFinite(enqueueAfterMs) || !Number.isFinite(enqueuedAtMs)) {
    return false;
  }
  return enqueuedAtMs <= enqueueAfterMs;
}

export async function queueStateForSha(
  reviewsDir,
  sha,
  { lane = "codex" } = {},
) {
  await ensureReviewQueue(reviewsDir, lane);
  const existing = await locateJob(reviewsDir, sha, lane);
  if (!existing) {
    return {
      queued: false,
      status: "",
      stale: false,
      workerAlive: false,
    };
  }
  const { workerAlive, stale } = isReviewQueueJobStale({
    status: existing.status,
    job: existing.job,
    isWorkerAlive,
  });
  return {
    queued: existing.status === "pending" || existing.status === "active",
    status: existing.status,
    stale,
    workerAlive,
    job: existing.job,
  };
}

export async function writeSyntheticFailureReport({
  reviewsDir,
  sha,
  lane = "codex",
  trigger = "manual",
  repoRoot,
  queueEnqueuedAt = "",
  reviewStatus = "infra_error",
  failureReason = "worker_failure",
  summary = "Review worker exited before a merged report artifact was written.",
  codexStatus = "failed",
}) {
  const normalizedLane = normalizeReviewQueueLane(lane);
  const now = isoNow();
  await mkdir(reviewsDir, { recursive: true, mode: 0o700 });
  const jsonPath = path.join(reviewsDir, `${sha}.json`);
  const mdPath = path.join(reviewsDir, `${sha}.md`);
  const result = writeReviewArtifacts({
    newData: {
      schema_version: 2,
      summary,
      findings: [],
    },
    targetJson: jsonPath,
    targetMd: mdPath,
    commitSha: sha,
    triggerName: normalizeReviewTrigger(trigger),
    reviewStatus,
    findingModelLabel: "",
    repoName: path.basename(repoRoot),
    repoRoot,
    reviewLane: normalizedLane,
    queueEnqueuedAt,
    failureReason,
    reviewEngines: {
      codex: {
        status: codexStatus,
        model: "codex-cli",
        error_code: failureReason,
        attempts: 1,
        max_attempts: 1,
        timed_out: failureReason === "timeout",
        completed: codexStatus === "ok",
        started_at: now,
        completed_at: now,
      },
    },
    retry: {
      policy: "exponential_backoff_with_jitter",
      max_attempts: 1,
      codex_attempts: 1,
      retries_exhausted: true,
    },
  });
  return result.sidecar;
}
