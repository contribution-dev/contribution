#!/usr/bin/env node

import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { reportsDirForLane } from "./codex-review-inbox-lib.mjs";
import { loadReviewedShas } from "./codex-review-push-gate-lib.mjs";
import {
  collectQueueSummary,
  DEFAULT_STALE_AFTER_MS,
  queueStateForSha,
} from "./codex-review-queue-lib.mjs";

const SHA_JSON_PATTERN = /^[0-9a-f]{40}\.json$/;

async function readJson(filePath) {
  try {
    const raw = await readFile(filePath, "utf8");
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

function shaFromJsonEntry(entry) {
  if (!SHA_JSON_PATTERN.test(String(entry ?? ""))) {
    return "";
  }
  return String(entry).slice(0, -5);
}

export function reportSatisfiesCodexLane(report) {
  if (!report || typeof report !== "object" || Array.isArray(report)) {
    return false;
  }
  const reviewStatus = String(report.review_status ?? "")
    .trim()
    .toLowerCase();
  const codexStatus = String(report.review_engines?.codex?.status ?? "")
    .trim()
    .toLowerCase();
  if (codexStatus) {
    return (
      codexStatus === "ok" && ["ok", "partial_success"].includes(reviewStatus)
    );
  }
  return ["ok", "partial_success"].includes(reviewStatus);
}

function jobSatisfiesLane(job, lane) {
  if (!job || typeof job !== "object" || Array.isArray(job)) {
    return false;
  }
  const reviewStatus = String(job.review_status ?? "")
    .trim()
    .toLowerCase();
  if (lane === "codex") {
    const codexStatus = String(job.lane_states?.codex?.status ?? "")
      .trim()
      .toLowerCase();
    if (codexStatus) {
      return (
        codexStatus === "ok" && ["ok", "partial_success"].includes(reviewStatus)
      );
    }
    return ["ok", "partial_success"].includes(reviewStatus);
  }
  return false;
}

export async function collectReviewCoverage(reviewsDir) {
  const summary = await collectQueueSummary(reviewsDir, { lane: "codex" });
  const codexSatisfiedShas = new Set();

  for (const entry of summary.jobs) {
    if (jobSatisfiesLane(entry.job, "codex")) {
      codexSatisfiedShas.add(entry.job.sha);
    }
  }

  const reportDir = reportsDirForLane(reviewsDir, "codex");
  const entries = await readdir(reportDir).catch(() => []);
  for (const entry of entries) {
    const sha = shaFromJsonEntry(entry);
    if (!sha) {
      continue;
    }
    const report = await readJson(path.join(reportDir, entry));
    if (reportSatisfiesCodexLane(report)) {
      codexSatisfiedShas.add(sha);
    }
  }

  return {
    queueSummary: summary,
    codexSatisfiedShas: [...codexSatisfiedShas].sort(),
    codexBacklogTotal: summary.lanes.codex.pending + summary.lanes.codex.active,
    codexActive: summary.lanes.codex.active,
  };
}

export async function collectLaneThroughput(reviewsDir, lane) {
  const reportDir = reportsDirForLane(reviewsDir, lane);
  const entries = await readdir(reportDir).catch(() => []);
  const completionsBySha = new Map();

  for (const entry of entries) {
    const sha = shaFromJsonEntry(entry);
    if (!sha) {
      continue;
    }
    const report = await readJson(path.join(reportDir, entry));
    if (!report || typeof report !== "object" || Array.isArray(report)) {
      continue;
    }
    const completedAtMs = Date.parse(
      String(
        report?.review_engines?.[lane]?.completed_at ??
          report?.last_reviewed ??
          report?.updated_at ??
          "",
      ),
    );
    if (!Number.isFinite(completedAtMs)) {
      continue;
    }
    completionsBySha.set(sha, {
      completedAtMs,
      satisfied: reportSatisfiesCodexLane(report),
    });
  }

  if (lane === "codex") {
    const repoRoot = path.dirname(reviewsDir);
    const reviewed = await loadReviewedShas({ repoRoot });
    for (const [sha, reviewedAt] of reviewed.laneMetadata.codex
      .reviewedAtBySha) {
      const completedAtMs = Date.parse(String(reviewedAt ?? ""));
      if (!Number.isFinite(completedAtMs)) {
        continue;
      }
      const existing = completionsBySha.get(sha);
      if (!existing || existing.completedAtMs < completedAtMs) {
        completionsBySha.set(sha, {
          completedAtMs,
          satisfied: true,
        });
      }
    }
  }

  const completions = [...completionsBySha.values()];
  completions.sort((left, right) => left.completedAtMs - right.completedAtMs);

  const nowMs = Date.now();
  const windows = {};
  for (const hours of [1, 3, 6]) {
    const cutoffMs = nowMs - hours * 60 * 60 * 1000;
    const recent = completions.filter(
      (entry) => entry.completedAtMs >= cutoffMs,
    );
    const satisfied = recent.filter((entry) => entry.satisfied);
    windows[`${hours}h`] = {
      completions: recent.length,
      satisfiedCompletions: satisfied.length,
      completionsPerHour: recent.length / hours,
      satisfiedPerHour: satisfied.length / hours,
    };
  }

  return {
    lane,
    completedTotal: completions.length,
    lastCompletedAt:
      completions.length > 0
        ? new Date(
            completions[completions.length - 1].completedAtMs,
          ).toISOString()
        : "",
    windows,
  };
}

export async function collectLaneReportOutcomes(
  reviewsDir,
  lane,
  { limit = 5, sinceMs = 0 } = {},
) {
  const reportDir = reportsDirForLane(reviewsDir, lane);
  const entries = await readdir(reportDir).catch(() => []);
  const counts = new Map();
  const cutoffMs = Number.isFinite(sinceMs) && sinceMs > 0 ? sinceMs : 0;

  for (const entry of entries) {
    const sha = shaFromJsonEntry(entry);
    if (!sha) {
      continue;
    }
    const reportPath = path.join(reportDir, entry);
    if (cutoffMs > 0) {
      const info = await readJson(reportPath);
      const completedAtMs = Date.parse(
        String(
          info?.review_engines?.[lane]?.completed_at ??
            info?.last_reviewed ??
            info?.updated_at ??
            "",
        ),
      );
      if (Number.isFinite(completedAtMs) && completedAtMs < cutoffMs) {
        continue;
      }
      if (!info) {
        continue;
      }
      const engine = info?.review_engines?.[lane] ?? {};
      const status =
        String(engine?.status ?? "missing")
          .trim()
          .toLowerCase() || "missing";
      const errorCode =
        String(engine?.error_code ?? info?.failure_reason ?? "none")
          .trim()
          .toLowerCase() || "none";
      const key = `${status}|${errorCode}`;
      counts.set(key, (counts.get(key) ?? 0) + 1);
      continue;
    }
    const report = await readJson(reportPath);
    if (!report) {
      continue;
    }
    const engine = report?.review_engines?.[lane] ?? {};
    const status =
      String(engine?.status ?? "missing")
        .trim()
        .toLowerCase() || "missing";
    const errorCode =
      String(engine?.error_code ?? report?.failure_reason ?? "none")
        .trim()
        .toLowerCase() || "none";
    const key = `${status}|${errorCode}`;
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }

  const top = [...counts.entries()]
    .sort(
      (left, right) => right[1] - left[1] || left[0].localeCompare(right[0]),
    )
    .slice(0, limit)
    .map(([key, count]) => ({ key, count }));

  return {
    lane,
    totalReports: [...counts.values()].reduce((sum, count) => sum + count, 0),
    sinceMs: cutoffMs,
    top,
  };
}

export async function collectStaleActiveJobs(
  reviewsDir,
  { lane = "all" } = {},
) {
  const summary = await collectQueueSummary(reviewsDir, { lane });
  const jobs = [];
  for (const entry of summary.jobs) {
    if (entry.status !== "active") {
      continue;
    }
    const state = await queueStateForSha(reviewsDir, entry.job.sha, {
      lane: entry.lane,
    });
    if (state.stale) {
      jobs.push({
        lane: entry.lane,
        sha: entry.job.sha,
        heartbeatAt:
          entry.job.worker?.heartbeat_at ||
          entry.job.started_at ||
          entry.job.updated_at ||
          "",
      });
    }
  }
  return {
    staleAfterSeconds: Math.floor(DEFAULT_STALE_AFTER_MS / 1000),
    jobs,
  };
}

export function estimateEtaSeconds(backlog, ratePerHour) {
  if (!Number.isFinite(backlog) || backlog <= 0) {
    return 0;
  }
  if (!Number.isFinite(ratePerHour) || ratePerHour <= 0) {
    return null;
  }
  return Math.ceil((backlog / ratePerHour) * 60 * 60);
}

export function formatEta(seconds) {
  if (seconds === null) {
    return "unknown";
  }
  if (seconds <= 0) {
    return "0m";
  }
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.ceil((seconds % 3600) / 60);
  if (hours <= 0) {
    return `${minutes}m`;
  }
  if (minutes <= 0) {
    return `${hours}h`;
  }
  return `${hours}h ${minutes}m`;
}
