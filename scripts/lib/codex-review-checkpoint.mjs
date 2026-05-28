import path from "node:path";
import { rm } from "node:fs/promises";
import { readJsonFile, writeJsonFileAtomic } from "./json-state.mjs";
import {
  isoNow,
  isValidReviewSha,
  normalizeReviewQueueLane,
} from "./codex-review-state.mjs";

export const REVIEW_CHECKPOINT_SCHEMA_VERSION = 1;

function normalizeStringArray(values) {
  if (!Array.isArray(values)) {
    return [];
  }
  return values.map((value) => String(value ?? "").trim()).filter(Boolean);
}

function normalizeFindings(findings) {
  return Array.isArray(findings) ? findings : [];
}

function normalizeCheckpoint(parsed, { sha, lane } = {}) {
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    return null;
  }
  const normalizedSha = isValidReviewSha(parsed.sha) ? String(parsed.sha) : sha;
  if (!isValidReviewSha(normalizedSha)) {
    return null;
  }
  return {
    schema_version: REVIEW_CHECKPOINT_SCHEMA_VERSION,
    sha: normalizedSha,
    lane: normalizeReviewQueueLane(parsed.lane ?? lane ?? "codex"),
    completed_modes: normalizeStringArray(parsed.completed_modes),
    summaries: normalizeStringArray(parsed.summaries),
    aggregated_findings: normalizeFindings(parsed.aggregated_findings),
    next_mode: String(parsed.next_mode ?? "").trim(),
    codex_attempts: Math.max(0, Number(parsed.codex_attempts ?? 0) || 0),
    last_error_kind: String(parsed.last_error_kind ?? "").trim(),
    last_error_code: String(parsed.last_error_code ?? "").trim(),
    updated_at: String(parsed.updated_at ?? "").trim(),
  };
}

export function reviewCheckpointDir(reviewsDir, lane = "codex") {
  return path.join(
    reviewsDir,
    "queue",
    normalizeReviewQueueLane(lane),
    "checkpoints",
  );
}

export function reviewCheckpointPath(reviewsDir, sha, lane = "codex") {
  return path.join(reviewCheckpointDir(reviewsDir, lane), `${sha}.json`);
}

export async function readReviewCheckpoint(reviewsDir, sha, lane = "codex") {
  try {
    const parsed = await readJsonFile(
      reviewCheckpointPath(reviewsDir, sha, lane),
    );
    return normalizeCheckpoint(parsed, { sha, lane });
  } catch {
    return null;
  }
}

export async function writeReviewCheckpoint(
  reviewsDir,
  {
    sha,
    lane = "codex",
    completedModes = [],
    summaries = [],
    aggregatedFindings = [],
    nextMode = "",
    codexAttempts = 0,
    lastErrorKind = "",
    lastErrorCode = "",
  } = {},
) {
  if (!isValidReviewSha(sha)) {
    throw new Error(`Invalid checkpoint sha: ${sha}`);
  }
  const payload = {
    schema_version: REVIEW_CHECKPOINT_SCHEMA_VERSION,
    sha,
    lane: normalizeReviewQueueLane(lane),
    completed_modes: normalizeStringArray(completedModes),
    summaries: normalizeStringArray(summaries),
    aggregated_findings: normalizeFindings(aggregatedFindings),
    next_mode: String(nextMode ?? "").trim(),
    codex_attempts: Math.max(0, Number(codexAttempts ?? 0) || 0),
    last_error_kind: String(lastErrorKind ?? "").trim(),
    last_error_code: String(lastErrorCode ?? "").trim(),
    updated_at: isoNow(),
  };
  await writeJsonFileAtomic(
    reviewCheckpointPath(reviewsDir, sha, lane),
    payload,
    {
      tempPrefix: "codex-review-checkpoint",
    },
  );
  return normalizeCheckpoint(payload, { sha, lane });
}

export async function clearReviewCheckpoint(reviewsDir, sha, lane = "codex") {
  await rm(reviewCheckpointPath(reviewsDir, sha, lane), { force: true });
}
