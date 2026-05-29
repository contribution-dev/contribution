#!/usr/bin/env node

import crypto from "node:crypto";
import {
  existsSync,
  readFileSync,
  renameSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import {
  isActionableCodexReviewReport,
  parseBacklogMeta,
} from "./codex-review-state.mjs";
import {
  normalizeReviewSeverity,
  parseMinReviewSeverity,
  reviewSeverityRank,
} from "./review-severity.mjs";

function quotedShellArg(value) {
  const text = String(value ?? "");
  return `'${text.replace(/'/g, `'\\''`)}'`;
}

function parseNumericLineRange(value) {
  const match = String(value ?? "")
    .trim()
    .match(/^(?<start>\d+)(?:-(?<end>\d+))?$/);
  if (!match?.groups?.start) return null;
  const start = Number.parseInt(match.groups.start, 10);
  const end = Number.parseInt(match.groups.end ?? match.groups.start, 10);
  if (!Number.isInteger(start) || !Number.isInteger(end)) return null;
  return {
    start: Math.min(start, end),
    end: Math.max(start, end),
  };
}

function uniqueEvidence(findings, limit = 4) {
  const seen = new Set();
  const results = [];
  for (const finding of Array.isArray(findings) ? findings : []) {
    for (const evidence of Array.isArray(finding?.evidence)
      ? finding.evidence
      : []) {
      const file = String(evidence?.file ?? "").trim();
      if (!file) continue;
      const lines = String(evidence?.lines ?? "").trim();
      const key = `${file}:${lines}`;
      if (seen.has(key)) continue;
      seen.add(key);
      results.push({ file, lines });
      if (results.length >= limit) {
        return results;
      }
    }
  }
  return results;
}

function handoffCommands(commitSha, findings) {
  const evidence = uniqueEvidence(findings);
  const files = evidence.map((item) => item.file);
  const commands = [];
  if (files.length > 0) {
    commands.push(
      `git show --stat --patch ${commitSha} -- ${files.map((file) => quotedShellArg(file)).join(" ")}`,
    );
  } else {
    commands.push(`git show --stat --patch ${commitSha}`);
  }

  for (const item of evidence.slice(0, 3)) {
    const range = parseNumericLineRange(item.lines);
    if (!range) continue;
    commands.push(
      `git show ${quotedShellArg(`${commitSha}:${item.file}`)} | sed -n '${range.start},${range.end}p'`,
    );
  }
  return [...new Set(commands)];
}

function buildAgentHandoff({ commitSha, findings }) {
  const evidence = uniqueEvidence(findings, 6);
  const files = [...new Set(evidence.map((item) => item.file))];
  const findingsToValidate = Array.isArray(findings)
    ? findings.map(
        (finding) =>
          `[${finding.finding_id}] [${String(finding.severity || "").toUpperCase()}] ${finding.title}`,
      )
    : [];

  return {
    files,
    findingsToValidate: findingsToValidate.slice(0, 3),
    extraFindingsCount: Math.max(0, findingsToValidate.length - 3),
    commands: handoffCommands(commitSha, findings).slice(0, 3),
  };
}

function clampConfidence(value) {
  const numeric = Number(value);
  if (!Number.isFinite(numeric) || numeric < 0) {
    return 0;
  }
  if (numeric > 1) {
    return 1;
  }
  return numeric;
}

function clampMinConfidence(value, fallback = 0) {
  const numeric = Number(value);
  if (!Number.isFinite(numeric)) {
    return clampConfidence(fallback);
  }
  return clampConfidence(numeric);
}

export function normalizeEvidence(item) {
  if (!item || typeof item !== "object" || Array.isArray(item)) {
    return {
      file: "",
      lines: "",
      reason: "",
    };
  }

  return {
    file: String(item.file || ""),
    lines: String(item.lines || ""),
    reason: String(item.reason || item.why || ""),
  };
}

function compactString(value) {
  return String(value ?? "").trim();
}

function parsePassLabels(value) {
  return compactString(value)
    .split(",")
    .map((label) => label.trim())
    .filter(Boolean);
}

function mergePassLabels(...values) {
  const labels = values
    .flatMap((value) => compactString(value).split(","))
    .map((value) => value.trim())
    .filter(Boolean);
  return [...new Set(labels)].sort().join(",");
}

export function parseLegacyVerifyPrompt(prompt) {
  if (typeof prompt !== "string") {
    return {
      hypothesis: "",
      impact: "",
      evidence: [],
    };
  }

  let body = prompt.trim();
  const prefix = "Verify this issue exists and fix it:";
  if (body.startsWith(prefix)) {
    body = body.slice(prefix.length).trim();
  }

  const evidence = [];
  if (body.includes("`") && body.includes(":")) {
    const parts = body.split("`");
    for (let index = 1; index < parts.length; index += 2) {
      const candidate = parts[index]?.trim() ?? "";
      if (!candidate.includes("/") || !candidate.includes(":")) {
        continue;
      }
      const [file, ...rest] = candidate.split(":");
      evidence.push({
        file,
        lines: rest.join(":"),
        reason: "Legacy migrated evidence",
      });
      break;
    }
  }

  return {
    hypothesis: body,
    impact: body,
    evidence,
  };
}

export function findingIdFor(item) {
  const raw = JSON.stringify({
    severity: String(item?.severity || ""),
    title: String(item?.title || ""),
    hypothesis: String(item?.hypothesis || ""),
    impact: String(item?.impact || ""),
    evidence: Array.isArray(item?.evidence) ? item.evidence : [],
    recommended_direction: String(item?.recommended_direction || ""),
  });
  return `F-${crypto.createHash("sha1").update(raw, "utf8").digest("hex").slice(0, 10)}`;
}

function normalizeToken(value) {
  return compactString(value)
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}

export function findingSignature(item) {
  const evidence = Array.isArray(item?.evidence)
    ? item.evidence
        .map(
          (entry) =>
            `${normalizeToken(entry?.file)}:${normalizeToken(entry?.lines)}`,
        )
        .filter(Boolean)
        .slice(0, 3)
    : [];
  const raw = JSON.stringify({
    title: normalizeToken(item?.title),
    hypothesis: normalizeToken(item?.hypothesis).slice(0, 120),
    evidence,
  });
  return crypto
    .createHash("sha1")
    .update(raw, "utf8")
    .digest("hex")
    .slice(0, 16);
}

export function normalizeFinding(item) {
  if (!item || typeof item !== "object" || Array.isArray(item)) {
    return null;
  }

  const severity = normalizeSeverityValue(item.severity);

  let normalized;
  if (
    Object.prototype.hasOwnProperty.call(item, "verify_and_fix_prompt") &&
    !Object.prototype.hasOwnProperty.call(item, "hypothesis")
  ) {
    const legacy = parseLegacyVerifyPrompt(
      String(item.verify_and_fix_prompt || ""),
    );
    normalized = {
      severity,
      confidence: clampConfidence(item.confidence),
      title: String(item.title || item.name || ""),
      finding_id: String(item.finding_id || item.id || ""),
      hypothesis: legacy.hypothesis || String(item.title || item.name || ""),
      impact: legacy.impact,
      evidence: legacy.evidence,
      recommended_direction:
        "Validate the issue and implement the minimal safe, preferably subtractive fix.",
      failure_scenario: "",
      broken_invariant: "",
      review_pass: compactString(item.review_pass),
    };
  } else {
    const evidence = Array.isArray(item.evidence)
      ? item.evidence.map(normalizeEvidence)
      : [];
    normalized = {
      severity,
      confidence: clampConfidence(item.confidence),
      title: String(item.title || item.name || ""),
      finding_id: String(item.finding_id || item.id || ""),
      hypothesis: String(item.hypothesis || item.description || ""),
      impact: String(item.impact || ""),
      evidence,
      recommended_direction: String(
        item.recommended_direction || item.recommendation || "",
      ),
      failure_scenario: compactString(item.failure_scenario),
      broken_invariant: compactString(item.broken_invariant),
      review_pass: compactString(item.review_pass),
    };
  }

  if (!normalized.finding_id) {
    normalized.finding_id = findingIdFor(normalized);
  }

  return normalized;
}

function mergeEvidenceLists(primary, secondary) {
  const merged = [];
  const seen = new Set();
  for (const item of [...(primary ?? []), ...(secondary ?? [])]) {
    const normalized = normalizeEvidence(item);
    const key = `${compactString(normalized.file)}:${compactString(normalized.lines)}:${compactString(normalized.reason)}`;
    if (!key || seen.has(key)) {
      continue;
    }
    seen.add(key);
    merged.push(normalized);
  }
  return merged;
}

function mergeFindingPair(left, right) {
  const leftNormalized = normalizeFinding(left);
  const rightNormalized = normalizeFinding(right);
  if (!leftNormalized) return rightNormalized;
  if (!rightNormalized) return leftNormalized;

  const primary =
    Number(rightNormalized.confidence ?? 0) >
    Number(leftNormalized.confidence ?? 0)
      ? rightNormalized
      : leftNormalized;
  const secondary =
    primary === leftNormalized ? rightNormalized : leftNormalized;

  return {
    ...primary,
    severity:
      reviewSeverityRank(leftNormalized.severity, { fallback: "minor" }) >=
      reviewSeverityRank(rightNormalized.severity, { fallback: "minor" })
        ? normalizeReviewSeverity(leftNormalized.severity, {
            fallback: "minor",
          })
        : normalizeReviewSeverity(rightNormalized.severity, {
            fallback: "minor",
          }),
    confidence: Math.max(
      clampConfidence(leftNormalized.confidence),
      clampConfidence(rightNormalized.confidence),
    ),
    evidence: mergeEvidenceLists(primary.evidence, secondary.evidence),
    review_pass: mergePassLabels(
      leftNormalized.review_pass,
      rightNormalized.review_pass,
    ),
    failure_scenario:
      primary.failure_scenario || secondary.failure_scenario || "",
    broken_invariant:
      primary.broken_invariant || secondary.broken_invariant || "",
  };
}

export function mergeReviewFindings(items) {
  const merged = [];
  const byId = new Map();
  const bySignature = new Map();

  for (const sourceItem of Array.isArray(items) ? items : []) {
    const item = normalizeFinding(sourceItem);
    if (!item) {
      continue;
    }

    const idKey = compactString(item.finding_id);
    const signatureKey = findingSignature(item);
    let index = -1;

    if (idKey && byId.has(idKey)) {
      index = byId.get(idKey);
    } else if (signatureKey && bySignature.has(signatureKey)) {
      index = bySignature.get(signatureKey);
    }

    if (index === -1) {
      merged.push(item);
      index = merged.length - 1;
    } else {
      merged[index] = mergeFindingPair(merged[index], item);
    }

    if (idKey) {
      byId.set(idKey, index);
    }
    if (signatureKey) {
      bySignature.set(signatureKey, index);
    }
  }

  return merged;
}

export function looksLikeReviewPayload(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return false;
  }
  const hasSummary =
    Object.prototype.hasOwnProperty.call(value, "summary") ||
    Object.prototype.hasOwnProperty.call(value, "message");
  const findingsCandidate = value.findings ?? value.issues;
  return hasSummary && Array.isArray(findingsCandidate);
}

export function normalizeReviewPayload(obj) {
  return normalizeReviewPayloadForLane(obj, "codex");
}

export function normalizeReviewPayloadForLane(obj, reviewLane = "codex") {
  if (!obj || typeof obj !== "object" || Array.isArray(obj)) {
    throw new Error("Review JSON root must be object");
  }

  let candidate = obj;
  if (!looksLikeReviewPayload(candidate)) {
    for (const key of ["output", "result", "response", "data", "review"]) {
      const wrapped = candidate[key];
      if (looksLikeReviewPayload(wrapped)) {
        candidate = wrapped;
        break;
      }
    }
  }

  if (!looksLikeReviewPayload(candidate)) {
    throw new Error("Review JSON did not contain expected review payload keys");
  }

  const findings = mergeReviewFindings(
    candidate.findings ?? candidate.issues ?? [],
  );

  return {
    schema_version: 2,
    summary: String(candidate.summary ?? candidate.message ?? ""),
    findings,
  };
}

export function parseLegacyMeta(markdownText) {
  if (typeof markdownText !== "string" || markdownText.length === 0) {
    return null;
  }

  const startMarker = "<!-- CODEX_REVIEW_META_START";
  const endMarker = "CODEX_REVIEW_META_END -->";
  const startIndex = markdownText.indexOf(startMarker);
  const endIndex = markdownText.indexOf(endMarker);
  if (startIndex === -1 || endIndex === -1 || endIndex <= startIndex) {
    return null;
  }

  const payload = markdownText
    .slice(startIndex + startMarker.length, endIndex)
    .trim();
  if (!payload) {
    return null;
  }

  try {
    return JSON.parse(payload);
  } catch {
    return null;
  }
}

export function loadExistingReviewData(targetJson, targetMd) {
  if (existsSync(targetJson)) {
    try {
      const parsed = JSON.parse(readFileSync(targetJson, "utf8"));
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
        return parsed;
      }
    } catch {
      // Fall through to the legacy markdown sidecar before giving up.
    }
  }

  if (existsSync(targetMd)) {
    const markdown = readFileSync(targetMd, "utf8");
    const legacy = parseLegacyMeta(markdown);
    if (legacy && typeof legacy === "object" && !Array.isArray(legacy)) {
      return {
        summary: String(legacy.summary || ""),
        triggers_seen: Array.isArray(legacy.triggers)
          ? legacy.triggers.map(String).filter(Boolean)
          : [],
        findings: Array.isArray(legacy.findings) ? legacy.findings : [],
        review_status: "ok",
      };
    }
    const structured = parseBacklogMeta(markdown);
    if (
      structured &&
      typeof structured === "object" &&
      !Array.isArray(structured) &&
      (String(structured.summary ?? "").trim() ||
        (Array.isArray(structured.findings) && structured.findings.length > 0))
    ) {
      return {
        summary: String(structured.summary || ""),
        triggers_seen: [],
        findings: Array.isArray(structured.findings) ? structured.findings : [],
        review_status: "ok",
      };
    }
  }

  return {};
}

function resolvePersistedFindings({
  existingFindings,
  newFindings,
  reviewStatus,
  completedReviewPasses = [],
}) {
  const normalizedNewFindings = mergeReviewFindings(newFindings);

  // A completed rerun for the same SHA should be authoritative. Otherwise a
  // stale false positive can survive even after the latest review no longer
  // reports it.
  if (reviewStatus === "ok") {
    return normalizedNewFindings;
  }

  if (reviewStatus === "partial_success") {
    const completedPasses = new Set(
      (Array.isArray(completedReviewPasses)
        ? completedReviewPasses
        : []
      ).flatMap((value) => parsePassLabels(value)),
    );
    if (completedPasses.size === 0) {
      return mergeReviewFindings([...existingFindings, ...newFindings]);
    }
    const retainedExisting = existingFindings.filter((item) => {
      const labels = parsePassLabels(item?.review_pass);
      if (labels.length === 0) {
        return true;
      }
      return !labels.every((label) => completedPasses.has(label));
    });
    return mergeReviewFindings([...retainedExisting, ...newFindings]);
  }

  return mergeReviewFindings([...existingFindings, ...newFindings]);
}

function atomicWrite(filePath, content) {
  const dir = path.dirname(filePath) || ".";
  const tempPath = path.join(
    dir,
    `.codex-review.${process.pid}.${Date.now()}.${crypto.randomUUID()}.tmp`,
  );
  writeFileSync(tempPath, content, "utf8");
  renameSync(tempPath, filePath);
}

function renderReviewMarkdown({
  sidecar,
  findings,
  findingsCount,
  actionRequired,
}) {
  const commitSha = String(sidecar?.sha ?? "").trim();
  const reviewStatus = String(sidecar?.review_status ?? "").trim();
  const summary = String(sidecar?.summary ?? "");
  const heading = actionRequired
    ? "Codex Review Action Needed"
    : "Codex Review Report";
  const lines = [];
  lines.push(`# ${heading}: ${commitSha}`);
  lines.push("");
  lines.push(
    "Validate these findings against the current checkout. Fix every valid finding with the smallest safe change. If a finding is stale or wrong, explain why.",
  );
  lines.push("");

  if (reviewStatus !== "ok" && findings.length === 0) {
    lines.push("## Review Status");
    lines.push("");
    lines.push(summary || "Review did not finish cleanly.");
    lines.push("");
    lines.push(`Current status: \`${reviewStatus || "unknown"}\``);
    lines.push("");
  } else {
    lines.push("## Findings");
    lines.push("");
    if (findings.length === 0) {
      lines.push("No actionable findings.");
      lines.push("");
    } else {
      for (const finding of findings) {
        lines.push(
          `### [${finding.finding_id}] [${String(finding.severity || "").toUpperCase()}] ${finding.title} (confidence ${Number(finding.confidence || 0).toFixed(2)})`,
        );
        lines.push("");
        lines.push(`- Hypothesis: ${finding.hypothesis}`);
        lines.push(`- Impact: ${finding.impact}`);
        if (finding.review_pass) {
          lines.push(`- Review pass: ${finding.review_pass}`);
        }
        if (Array.isArray(finding.evidence) && finding.evidence.length > 0) {
          lines.push("- Evidence:");
          for (const evidence of finding.evidence.slice(0, 3)) {
            let location = String(evidence.file || "");
            if (evidence.lines) {
              location = `${location}:${evidence.lines}`;
            }
            lines.push(
              evidence.reason
                ? `  - \`${location}\` — ${evidence.reason}`
                : `  - \`${location}\``,
            );
          }
        }
        lines.push(`- Recommended direction: ${finding.recommended_direction}`);
        lines.push("");
      }
    }
  }

  return `${lines.join("\n")}\n`;
}

export function buildReviewArtifacts({
  newData,
  targetJson,
  targetMd,
  commitSha,
  triggerName,
  reviewStatus,
  findingModelLabel,
  repoName,
  repoRoot,
  reviewLane = "codex",
  reviewEngines = null,
  failureReason = "",
  retry = null,
  queueEnqueuedAt = "",
  preserveExistingQueueEnqueuedAt = false,
}) {
  const existing = loadExistingReviewData(targetJson, targetMd);
  const newFindings = Array.isArray(newData?.findings) ? newData.findings : [];
  const existingFindings = Array.isArray(existing.findings)
    ? existing.findings
    : [];
  const completedReviewPasses = Array.isArray(
    reviewEngines?.codex?.review_passes,
  )
    ? reviewEngines.codex.review_passes
    : [];

  const merged = resolvePersistedFindings({
    existingFindings,
    newFindings,
    reviewStatus,
    completedReviewPasses,
  });

  const existingTriggers = Array.isArray(existing.triggers_seen)
    ? existing.triggers_seen.map(String).filter(Boolean)
    : [];
  const triggersSeen = Array.from(
    new Set([...existingTriggers, triggerName]),
  ).sort();

  const existingFindingModels = Array.isArray(existing.finding_models)
    ? existing.finding_models.map(String).filter(Boolean)
    : [];
  const findingModels = new Set(existingFindingModels);
  if (newFindings.length > 0 && String(findingModelLabel || "").trim()) {
    findingModels.add(String(findingModelLabel).trim());
  }
  let findingModelsSorted = Array.from(findingModels).sort();
  if (merged.length > 0 && findingModelsSorted.length === 0) {
    findingModelsSorted = ["unknown"];
  }

  const summary =
    typeof newData?.summary === "string"
      ? newData.summary
      : String(existing.summary || "");
  const normalizedQueueEnqueuedAt = String(
    queueEnqueuedAt ||
      (preserveExistingQueueEnqueuedAt ? existing.queue_enqueued_at : "") ||
      "",
  ).trim();
  const nowUtc = new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
  const sidecar = {
    schema_version: 2,
    sha: commitSha,
    lane: String(reviewLane || "codex"),
    repository: {
      name: repoName,
      root: repoRoot,
    },
    last_reviewed: nowUtc,
    trigger_last: triggerName,
    triggers_seen: triggersSeen,
    review_status: reviewStatus,
    ...(String(failureReason ?? "").trim()
      ? { failure_reason: String(failureReason).trim() }
      : {}),
    summary,
    finding_models: findingModelsSorted,
    findings: merged,
    ...(reviewEngines &&
    typeof reviewEngines === "object" &&
    !Array.isArray(reviewEngines)
      ? { review_engines: reviewEngines }
      : {}),
    ...(retry && typeof retry === "object" && !Array.isArray(retry)
      ? { retry }
      : {}),
    ...(Number.isFinite(Date.parse(normalizedQueueEnqueuedAt))
      ? { queue_enqueued_at: normalizedQueueEnqueuedAt }
      : {}),
  };

  const actionRequired = isActionableCodexReviewReport(
    {
      findings: merged,
      review_status: reviewStatus,
    },
    true,
  );
  return {
    sidecar,
    markdown: renderReviewMarkdown({
      sidecar,
      findings: merged,
      findingsCount: merged.length,
      actionRequired,
    }),
    findingsCount: merged.length,
    actionRequired,
  };
}

export function writeReviewMarkdownProjection({ sidecar, targetMd }) {
  const findings = Array.isArray(sidecar?.findings) ? sidecar.findings : [];
  const reviewStatus = String(sidecar?.review_status ?? "").trim();
  const actionRequired = isActionableCodexReviewReport(
    {
      findings,
      review_status: reviewStatus,
    },
    true,
  );
  if (!actionRequired) {
    rmSync(targetMd, { force: true });
    return { actionRequired: false };
  }
  atomicWrite(
    targetMd,
    renderReviewMarkdown({
      sidecar,
      findings,
      findingsCount: findings.length,
      actionRequired: true,
    }),
  );
  return { actionRequired: true };
}

export function writeReviewArtifacts(input) {
  const result = buildReviewArtifacts(input);
  atomicWrite(input.targetJson, `${JSON.stringify(result.sidecar, null, 2)}\n`);
  if (result.actionRequired) {
    atomicWrite(input.targetMd, result.markdown);
  } else {
    rmSync(input.targetMd, { force: true });
  }
  return result;
}

export function shouldNotifyFindings({
  data,
  minSeverity,
  useLegacyConf,
  legacyBlockerConf,
  reviewLane = "codex",
  minConfidence = 0,
}) {
  const threshold = reviewSeverityRank(
    parseMinReviewSeverity(minSeverity, "minor"),
  );
  const normalizedMinConfidence = clampMinConfidence(minConfidence, 0);
  const normalizedLegacyBlockerConf = clampMinConfidence(legacyBlockerConf, 0);
  const applyLaneMinConfidence =
    String(reviewLane || "codex").toLowerCase() === "local";
  const findings = Array.isArray(data?.findings) ? data.findings : [];
  for (const finding of findings) {
    const severity = normalizeReviewSeverity(finding?.severity);
    const findingRank = reviewSeverityRank(severity);
    if (findingRank < threshold) {
      continue;
    }
    let requiredConfidence = applyLaneMinConfidence
      ? normalizedMinConfidence
      : 0;
    if (useLegacyConf && severity === "blocker") {
      requiredConfidence = normalizedLegacyBlockerConf;
    }
    if (Number(finding?.confidence || 0) < requiredConfidence) {
      continue;
    }
    return true;
  }
  return false;
}

function printUsage() {
  console.error(
    "Usage: node scripts/lib/codex-review-findings.mjs <normalize-review-payload|merge-review-artifacts|should-notify-findings> ...",
  );
}

function main() {
  const [command, ...args] = process.argv.slice(2);
  if (!command) {
    printUsage();
    process.exitCode = 1;
    return;
  }

  if (command === "normalize-review-payload") {
    const [inputPath, outputPath] = args;
    if (!inputPath || !outputPath) {
      printUsage();
      process.exitCode = 1;
      return;
    }
    const input = JSON.parse(readFileSync(inputPath, "utf8"));
    const normalized = normalizeReviewPayload(input);
    atomicWrite(outputPath, `${JSON.stringify(normalized, null, 2)}\n`);
    return;
  }

  if (command === "merge-review-artifacts") {
    const [
      jsonPath,
      targetJson,
      targetMd,
      commitSha,
      triggerName,
      reviewStatus,
      findingModelLabel,
      repoName,
      repoRoot,
      reviewLane,
    ] = args;
    if (
      !jsonPath ||
      !targetJson ||
      !targetMd ||
      !commitSha ||
      !triggerName ||
      !reviewStatus ||
      repoName === undefined ||
      repoRoot === undefined
    ) {
      printUsage();
      process.exitCode = 1;
      return;
    }
    const newData = JSON.parse(readFileSync(jsonPath, "utf8"));
    const result = writeReviewArtifacts({
      newData,
      targetJson,
      targetMd,
      commitSha,
      triggerName,
      reviewStatus,
      findingModelLabel,
      repoName,
      repoRoot,
      reviewLane,
    });
    console.log(String(result.findingsCount));
    return;
  }

  if (command === "should-notify-findings") {
    const [
      jsonPath,
      minSeverity,
      useLegacyConfRaw,
      legacyBlockerConfRaw,
      minConfidenceRaw,
      reviewLane = "codex",
    ] = args;
    if (
      !jsonPath ||
      !minSeverity ||
      useLegacyConfRaw === undefined ||
      legacyBlockerConfRaw === undefined
    ) {
      printUsage();
      process.exitCode = 1;
      return;
    }
    const data = JSON.parse(readFileSync(jsonPath, "utf8"));
    const shouldNotify = shouldNotifyFindings({
      data,
      minSeverity,
      useLegacyConf: useLegacyConfRaw === "1",
      legacyBlockerConf: Number(legacyBlockerConfRaw),
      reviewLane,
      minConfidence: minConfidenceRaw,
    });
    console.log(shouldNotify ? "1" : "0");
    return;
  }

  printUsage();
  process.exitCode = 1;
}

const invokedPath = process.argv[1];
if (invokedPath && import.meta.url === pathToFileURL(invokedPath).href) {
  try {
    main();
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}
