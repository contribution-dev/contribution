export const REVIEW_SEVERITY_RANK = {
  none: 0,
  minor: 1,
  major: 2,
  blocker: 3,
};

export function normalizeReviewSeverity(value, { fallback = "none" } = {}) {
  const raw = String(value ?? "")
    .trim()
    .toLowerCase();
  if (
    raw === "none" ||
    raw === "minor" ||
    raw === "major" ||
    raw === "blocker"
  ) {
    return raw;
  }
  return normalizeReviewSeverity(fallback, { fallback: "none" });
}

export function reviewSeverityRank(value, options = {}) {
  return REVIEW_SEVERITY_RANK[normalizeReviewSeverity(value, options)] ?? 0;
}

export function parseMinReviewSeverity(value, fallback = "major") {
  return normalizeReviewSeverity(value ?? fallback, { fallback });
}
