#!/usr/bin/env node

import { pathMatchesAnyGlob } from "./lib.mjs";

export const REVIEW_STATUS = {
  SUCCESS: "success",
  NO_MATCH: "no-match",
  PENDING: "pending",
  NON_SUCCESS: "non-success",
};

export function toRegex(pattern) {
  try {
    // nosemgrep: javascript.lang.security.audit.detect-non-literal-regexp.detect-non-literal-regexp -- review check patterns are repo-owned policy configuration.
    return new RegExp(pattern, "i");
  } catch {
    const escaped = pattern.replace(/[|\\{}()[\]^$+?.]/g, "\\$&");
    // nosemgrep: javascript.lang.security.audit.detect-non-literal-regexp.detect-non-literal-regexp -- invalid policy patterns are escaped before fallback matching.
    return new RegExp(escaped, "i");
  }
}

export function selectMatchingRuns(checkRuns, patternStrings) {
  const patterns = patternStrings.map((pattern) => toRegex(pattern));
  return checkRuns
    .filter((run) => patterns.some((pattern) => pattern.test(run.name ?? "")))
    .sort((a, b) => b.id - a.id);
}

export function resolveReviewStatus(checkRuns, patternStrings) {
  const matchingRuns = selectMatchingRuns(checkRuns, patternStrings);
  if (matchingRuns.length === 0) {
    return { status: REVIEW_STATUS.NO_MATCH, latestRun: null };
  }

  const latestRun = matchingRuns[0];
  if (latestRun.status !== "completed") {
    return { status: REVIEW_STATUS.PENDING, latestRun };
  }
  if (latestRun.conclusion === "success") {
    return { status: REVIEW_STATUS.SUCCESS, latestRun };
  }
  return { status: REVIEW_STATUS.NON_SUCCESS, latestRun };
}

export function computeRiskTier(contract, changedFiles) {
  for (const tier of contract.riskTierPriority) {
    const patterns = contract.riskTierRules[tier] ?? [];
    const matched = changedFiles.some((changedFile) =>
      pathMatchesAnyGlob(changedFile, patterns),
    );
    if (matched) return tier;
  }
  return "low";
}

export function isReviewRequired(contract, riskTier) {
  return (
    contract.reviewAgent.mode === "required" &&
    contract.reviewAgent.requiredTiers.includes(riskTier)
  );
}

export function hasShaDedupComment({
  comments,
  marker,
  shaTokenPrefix,
  headSha,
}) {
  const shaToken = `${shaTokenPrefix}${headSha}`;
  return comments.some((comment) => {
    const body = String(comment?.body ?? "");
    return body.includes(marker) && body.includes(shaToken);
  });
}

export function isSameCommitSha(leftSha, rightSha) {
  if (!leftSha || !rightSha) return false;
  const left = String(leftSha).toLowerCase();
  const right = String(rightSha).toLowerCase();
  return left === right || left.startsWith(right) || right.startsWith(left);
}

export function isStaleWorkflowHead({ eventHeadSha, currentHeadSha }) {
  if (!eventHeadSha || !currentHeadSha) return false;
  return !isSameCommitSha(eventHeadSha, currentHeadSha);
}

export function percentile(values, p) {
  if (!Array.isArray(values) || values.length === 0) return null;
  const sorted = [...values].sort((a, b) => a - b);
  const rank = (p / 100) * (sorted.length - 1);
  const low = Math.floor(rank);
  const high = Math.ceil(rank);
  if (low === high) return sorted[low];
  const weight = rank - low;
  return sorted[low] * (1 - weight) + sorted[high] * weight;
}

export function ageInDays(createdAtIso, now = new Date()) {
  const created = new Date(createdAtIso);
  if (Number.isNaN(created.getTime())) return null;
  const diffMs = now.getTime() - created.getTime();
  return Math.floor(diffMs / (24 * 60 * 60 * 1000));
}
