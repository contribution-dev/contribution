const RATE_LIMIT_PATTERNS = ["rate limit", "too many requests", "429"];
const HARD_QUOTA_PATTERNS = ["usage limit", "credits", "billing", "quota"];
const SYSTEM_ERROR_PATTERNS = [
  "codex is not authenticated",
  "not authenticated",
  "authentication failed",
  "connector bootstrap",
  "bootstrap error",
  "configuration error",
  "invalid output schema",
  "codex cli is not available in path",
  "codex cli is not available",
];
const TRANSIENT_TRANSPORT_PATTERNS = [
  "connection reset",
  "socket hang up",
  "network error",
  "temporarily unavailable",
  "econnreset",
  "etimedout",
  "ehostunreach",
  "enotfound",
  "fetch failed",
  "transport error",
];

export const DEFAULT_HARD_QUOTA_PAUSE_RETRY_MS = 30 * 60_000;
const PROVIDER_RETRY_CLOCK_TIME_ZONE = "America/New_York";

function normalizeSearchText(...values) {
  return values
    .map((value) => String(value ?? "").trim())
    .filter(Boolean)
    .join("\n")
    .toLowerCase();
}

function matchPattern(text, patterns) {
  return patterns.find((pattern) => text.includes(pattern)) ?? "";
}

function baseFailure(
  kind,
  code,
  text,
  matchedPattern = "",
  resumeAfterAt = "",
) {
  return {
    kind,
    code,
    text,
    matchedPattern,
    resumeAfterAt,
  };
}

function pauseTimestampMs(pauseState = {}) {
  const timestamps = [
    Date.parse(String(pauseState.updated_at ?? "").trim()),
    Date.parse(String(pauseState.paused_at ?? "").trim()),
  ].filter(Number.isFinite);
  if (timestamps.length === 0) {
    return 0;
  }
  return Math.max(...timestamps);
}

function normalizeResumeAfterAt(value = "") {
  const parsed = Date.parse(String(value ?? "").trim());
  if (!Number.isFinite(parsed)) {
    return "";
  }
  return new Date(parsed).toISOString();
}

function parseResetAtEpochMs(rawValue) {
  const digits = String(rawValue ?? "").trim();
  if (!/^\d{10,13}$/.test(digits)) {
    return Number.NaN;
  }
  const parsed = Number.parseInt(digits, 10);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return Number.NaN;
  }
  return digits.length >= 13 ? parsed : parsed * 1000;
}

function readDecimalFrom(text, startFrom) {
  let cursor = startFrom;
  while (
    cursor < text.length &&
    (text.charCodeAt(cursor) === 32 ||
      text.charCodeAt(cursor) === 9 ||
      text.charCodeAt(cursor) === 10 ||
      text.charCodeAt(cursor) === 13)
  ) {
    cursor += 1;
  }
  const valueStart = cursor;
  let hasDigits = false;
  while (
    cursor < text.length &&
    ((text.charCodeAt(cursor) >= 48 && text.charCodeAt(cursor) <= 57) ||
      text.charCodeAt(cursor) === 46)
  ) {
    hasDigits = true;
    cursor += 1;
  }
  if (!hasDigits) {
    return Number.NaN;
  }
  const valueText = text.slice(valueStart, cursor);
  return Number.parseFloat(valueText);
}

function readDecimalAfterKey(text, key) {
  const quotedKey = `"${key}"`;
  const keyIndex = text.indexOf(quotedKey);
  if (keyIndex < 0) {
    return Number.NaN;
  }
  const colonIndex = text.indexOf(":", keyIndex + quotedKey.length);
  if (colonIndex < 0) {
    return Number.NaN;
  }
  return readDecimalFrom(text, colonIndex + 1);
}

function parsePrimaryUsedPercent(primaryObjectText) {
  return readDecimalAfterKey(primaryObjectText, "used_percent");
}

function parsePrimaryResetAt(primaryObjectText) {
  const resetText = readDecimalAfterKey(primaryObjectText, "resets_at");
  if (!Number.isFinite(resetText)) {
    return Number.NaN;
  }
  return parseResetAtEpochMs(resetText);
}

function extractStructuredResetAtMs(text, nowMs) {
  const primaryMatches = [];
  let searchFrom = 0;
  const referenceNowMs = Number.isFinite(nowMs) ? nowMs : 0;
  while (true) {
    const primaryStart = text.indexOf('"primary"', searchFrom);
    if (primaryStart < 0) {
      break;
    }
    const openBraceStart = text.indexOf("{", primaryStart);
    if (openBraceStart < 0) {
      break;
    }
    const primaryObjectClose = text.indexOf("}", openBraceStart);
    const primaryObjectText =
      primaryObjectClose < 0
        ? text.slice(openBraceStart)
        : text.slice(openBraceStart, primaryObjectClose + 1);

    const usedPercent = parsePrimaryUsedPercent(primaryObjectText);
    if (usedPercent === 100 || usedPercent === 100.0) {
      const parsedResetAt = parsePrimaryResetAt(primaryObjectText);
      if (Number.isFinite(parsedResetAt) && parsedResetAt > referenceNowMs) {
        primaryMatches.push(parsedResetAt);
      }
    }

    searchFrom = Math.max(searchFrom + 1, primaryObjectClose + 1);
  }

  const structuredResets = primaryMatches.filter(
    (value) => Number.isFinite(value) && value > referenceNowMs,
  );
  if (structuredResets.length > 0) {
    return Math.min(...structuredResets);
  }

  const resetMatches = [...text.matchAll(/"resets_at"\s*:\s*(\d{10,13})/g)]
    .map((match) => parseResetAtEpochMs(match[1]))
    .filter((value) => Number.isFinite(value) && value > referenceNowMs);
  if (resetMatches.length === 0) {
    return Number.NaN;
  }
  return Math.min(...resetMatches);
}

function extractClockTimeResumeAt(text, nowMs) {
  const match = /try again at\s+(\d{1,2}):(\d{2})\s*(am|pm)\b/i.exec(text);
  if (!match) {
    return "";
  }
  const hours = Number.parseInt(match[1], 10);
  const minutes = Number.parseInt(match[2], 10);
  const meridiem = match[3].toLowerCase();
  if (
    !Number.isInteger(hours) ||
    !Number.isInteger(minutes) ||
    hours < 1 ||
    hours > 12 ||
    minutes < 0 ||
    minutes > 59
  ) {
    return "";
  }

  const normalizedHours = (hours % 12) + (meridiem === "pm" ? 12 : 0);
  const nowDateParts = zonedDateParts(nowMs, PROVIDER_RETRY_CLOCK_TIME_ZONE);
  let candidateMs = zonedWallTimeToUtcMs(
    PROVIDER_RETRY_CLOCK_TIME_ZONE,
    nowDateParts.year,
    nowDateParts.month,
    nowDateParts.day,
    normalizedHours,
    minutes,
  );
  if (candidateMs < nowMs) {
    const nextDate = addUtcCalendarDays(
      nowDateParts.year,
      nowDateParts.month,
      nowDateParts.day,
      1,
    );
    candidateMs = zonedWallTimeToUtcMs(
      PROVIDER_RETRY_CLOCK_TIME_ZONE,
      nextDate.year,
      nextDate.month,
      nextDate.day,
      normalizedHours,
      minutes,
    );
  }
  return new Date(candidateMs).toISOString();
}

function zonedDateParts(ms, timeZone) {
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(new Date(ms));
  return {
    year: Number.parseInt(partValue(parts, "year"), 10),
    month: Number.parseInt(partValue(parts, "month"), 10),
    day: Number.parseInt(partValue(parts, "day"), 10),
  };
}

function zonedDateTimeParts(ms, timeZone) {
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hourCycle: "h23",
  }).formatToParts(new Date(ms));
  return {
    year: Number.parseInt(partValue(parts, "year"), 10),
    month: Number.parseInt(partValue(parts, "month"), 10),
    day: Number.parseInt(partValue(parts, "day"), 10),
    hour: Number.parseInt(partValue(parts, "hour"), 10),
    minute: Number.parseInt(partValue(parts, "minute"), 10),
    second: Number.parseInt(partValue(parts, "second"), 10),
  };
}

function partValue(parts, type) {
  return parts.find((part) => part.type === type)?.value ?? "0";
}

function zonedOffsetMs(timeZone, utcMs) {
  const parts = zonedDateTimeParts(utcMs, timeZone);
  const zonedAsUtcMs = Date.UTC(
    parts.year,
    parts.month - 1,
    parts.day,
    parts.hour,
    parts.minute,
    parts.second,
  );
  return zonedAsUtcMs - utcMs;
}

function zonedWallTimeToUtcMs(timeZone, year, month, day, hour, minute) {
  const wallAsUtcMs = Date.UTC(year, month - 1, day, hour, minute, 0, 0);
  const firstPassUtcMs = wallAsUtcMs - zonedOffsetMs(timeZone, wallAsUtcMs);
  return wallAsUtcMs - zonedOffsetMs(timeZone, firstPassUtcMs);
}

function addUtcCalendarDays(year, month, day, days) {
  const date = new Date(Date.UTC(year, month - 1, day + days, 0, 0, 0, 0));
  return {
    year: date.getUTCFullYear(),
    month: date.getUTCMonth() + 1,
    day: date.getUTCDate(),
  };
}

function extractReviewFailureResumeAfterAt(text, { nowMs = Date.now() } = {}) {
  const structuredResetAtMs = extractStructuredResetAtMs(text, nowMs);
  if (Number.isFinite(structuredResetAtMs)) {
    return new Date(structuredResetAtMs).toISOString();
  }
  return extractClockTimeResumeAt(text, nowMs);
}

export function classifyReviewFailure({
  errorCode = "",
  message = "",
  outputText = "",
  stderr = "",
  stdout = "",
} = {}) {
  const normalizedCode = String(errorCode ?? "")
    .trim()
    .toLowerCase();
  const text = normalizeSearchText(message, outputText, stderr, stdout);
  const resumeAfterAt = extractReviewFailureResumeAfterAt(text);

  if (!normalizedCode && !text) {
    return baseFailure("none", "none", text);
  }
  if (
    normalizedCode === "timeout" ||
    normalizedCode === "timed_out" ||
    normalizedCode === "codex-exec-timeout"
  ) {
    return baseFailure("timeout", "timeout", text);
  }
  if (
    normalizedCode === "hard_quota" ||
    normalizedCode === "quota" ||
    normalizedCode === "codex-exec-quota"
  ) {
    return baseFailure("hard_quota", "hard_quota", text, "", resumeAfterAt);
  }
  if (
    normalizedCode === "system_error" ||
    normalizedCode === "codex-exec-system-error"
  ) {
    return baseFailure("system_error", "system_error", text);
  }
  if (
    normalizedCode === "transient_transport" ||
    normalizedCode === "codex-exec-transient-transport"
  ) {
    return baseFailure("transient_transport", "transient_transport", text);
  }

  if (text.includes("timed out")) {
    return baseFailure("timeout", "timeout", text, "timed out");
  }

  const hardQuotaPattern = matchPattern(text, HARD_QUOTA_PATTERNS);
  if (hardQuotaPattern) {
    return baseFailure(
      "hard_quota",
      "hard_quota",
      text,
      hardQuotaPattern,
      resumeAfterAt,
    );
  }

  if (normalizedCode === "rate_limit") {
    return baseFailure("rate_limit", "rate_limit", text);
  }

  const rateLimitPattern = matchPattern(text, RATE_LIMIT_PATTERNS);
  if (rateLimitPattern) {
    return baseFailure("rate_limit", "rate_limit", text, rateLimitPattern);
  }

  const systemPattern = matchPattern(text, SYSTEM_ERROR_PATTERNS);
  if (systemPattern) {
    return baseFailure("system_error", "system_error", text, systemPattern);
  }

  const transientPattern = matchPattern(text, TRANSIENT_TRANSPORT_PATTERNS);
  if (transientPattern) {
    return baseFailure(
      "transient_transport",
      "transient_transport",
      text,
      transientPattern,
    );
  }

  if (normalizedCode && normalizedCode !== "none") {
    return baseFailure("exec_failed", "exec_failed", text);
  }

  return baseFailure("exec_failed", "exec_failed", text);
}

export function isRetryableReviewFailureKind(kind) {
  return String(kind ?? "") === "timeout";
}

export function shouldPauseReviewFailureKind(kind) {
  return kind === "hard_quota" || kind === "system_error";
}

export function shouldRequeueReviewFailureKind(kind) {
  return (
    kind === "rate_limit" ||
    kind === "hard_quota" ||
    kind === "system_error" ||
    kind === "transient_transport"
  );
}

export function computeRetryAfterAt({
  attempt = 1,
  failureKind = "rate_limit",
  nowMs = Date.now(),
  random = Math.random,
} = {}) {
  const normalizedAttempt = Math.max(1, Number(attempt ?? 1) || 1);
  const baseDelayMs = failureKind === "rate_limit" ? 60_000 : 15_000;
  const maxDelayMs = failureKind === "rate_limit" ? 30 * 60_000 : 5 * 60_000;
  const delayMs = Math.min(
    maxDelayMs,
    baseDelayMs * 2 ** Math.max(0, normalizedAttempt - 1),
  );
  const jitterCeiling = Math.max(
    1,
    Math.min(15_000, Math.floor(delayMs * 0.2)),
  );
  const jitterMs = Math.floor(Math.max(0, random()) * jitterCeiling);
  return new Date(nowMs + delayMs + jitterMs).toISOString();
}

export function classifyReviewPauseState(pauseState = {}) {
  if (pauseState?.paused !== true) {
    return "none";
  }
  const explicitReason = String(pauseState.reason ?? "")
    .trim()
    .toLowerCase();
  if (explicitReason === "hard_quota" || explicitReason === "system_error") {
    return explicitReason;
  }
  if (explicitReason === "quota") {
    return "legacy_quota";
  }
  const classified = classifyReviewFailure({
    errorCode: explicitReason,
    message: [pauseState.reason, pauseState.message].filter(Boolean).join("\n"),
  });
  if (classified.kind !== "none" && classified.kind !== "exec_failed") {
    return classified.kind;
  }
  return explicitReason || "unknown";
}

export function resolveHardQuotaPauseRetryMs(
  rawValue = process.env.CODEX_REVIEW_HARD_QUOTA_PAUSE_RETRY_SECONDS,
) {
  const trimmed = String(rawValue ?? "").trim();
  if (!trimmed) {
    return DEFAULT_HARD_QUOTA_PAUSE_RETRY_MS;
  }
  const parsedSeconds = Number.parseInt(trimmed, 10);
  if (!Number.isInteger(parsedSeconds) || parsedSeconds < 0) {
    return DEFAULT_HARD_QUOTA_PAUSE_RETRY_MS;
  }
  return parsedSeconds * 1000;
}

export function computeReviewPauseResumeAt(
  pauseState = {},
  {
    hardQuotaPauseRetryMs = resolveHardQuotaPauseRetryMs(),
    resumeAfterAt = "",
  } = {},
) {
  const pauseClass = classifyReviewPauseState(pauseState);
  if (pauseClass !== "hard_quota" && pauseClass !== "legacy_quota") {
    return "";
  }
  const explicitResumeAt = normalizeResumeAfterAt(resumeAfterAt);
  const persistedResumeAt = String(pauseState.resume_after_at ?? "").trim();
  const persistedResumeAtMs = Date.parse(persistedResumeAt);
  if (explicitResumeAt && Number.isFinite(persistedResumeAtMs)) {
    return Date.parse(explicitResumeAt) > persistedResumeAtMs
      ? explicitResumeAt
      : persistedResumeAt;
  }
  if (explicitResumeAt) {
    return explicitResumeAt;
  }
  const pausedAtMs = pauseTimestampMs(pauseState);
  if (!Number.isFinite(pausedAtMs) || pausedAtMs <= 0) {
    if (Number.isFinite(persistedResumeAtMs)) {
      return persistedResumeAt;
    }
    return "";
  }
  const retryMs = Math.max(0, Number(hardQuotaPauseRetryMs) || 0);
  const fallbackResumeAt = new Date(pausedAtMs + retryMs).toISOString();
  if (Number.isFinite(persistedResumeAtMs)) {
    return Date.parse(fallbackResumeAt) > persistedResumeAtMs
      ? fallbackResumeAt
      : persistedResumeAt;
  }
  return fallbackResumeAt;
}

export function shouldAutoClearReviewPauseState(
  pauseState = {},
  {
    nowMs = Date.now(),
    hardQuotaPauseRetryMs = resolveHardQuotaPauseRetryMs(),
  } = {},
) {
  const resumeAt = computeReviewPauseResumeAt(pauseState, {
    hardQuotaPauseRetryMs,
  });
  const resumeAtMs = Date.parse(resumeAt);
  return Number.isFinite(resumeAtMs) && nowMs >= resumeAtMs;
}
