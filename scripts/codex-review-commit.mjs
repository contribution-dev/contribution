#!/usr/bin/env node

import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import path from "node:path";
import process from "node:process";
import { pathToFileURL } from "node:url";
import {
  resolveRepoRoot,
  resolveReviewsDir,
} from "./codex-review-inbox-lib.mjs";
import {
  mergeReviewFindings,
  normalizeReviewPayload,
  writeReviewArtifacts,
} from "./lib/codex-review-findings.mjs";
import {
  buildReviewPrompt,
  DEFAULT_FINDING_MIN_CONFIDENCE,
  DEFAULT_MAX_FINDINGS,
  HIGH_RISK_MAX_FINDINGS,
  DEFAULT_PROMPT_MAX_CHARS,
  DEFAULT_PROMPT_MAX_FILE_CHARS,
} from "./lib/codex-review-prompt.mjs";
import { classifyReviewFailure } from "./lib/codex-review-failure.mjs";
import { executeReviewPasses as executeSharedReviewPasses } from "./lib/codex-review-execution.mjs";
import { verifyFindings } from "./codex-review-verify-findings.mjs";

export function parseArgs(argv) {
  const args = {
    sha: "",
    trigger: "",
    lane: process.env.CODE_REVIEW_LANE ?? "codex",
  };

  for (let index = 2; index < argv.length; index += 1) {
    const arg = argv[index];
    const next = argv[index + 1];
    switch (arg) {
      case "--sha":
        args.sha = String(next ?? "").trim();
        index += 1;
        break;
      case "--trigger":
        args.trigger = String(next ?? "").trim();
        index += 1;
        break;
      case "--lane":
        args.lane = String(next ?? "").trim();
        index += 1;
        break;
      case "-h":
      case "--help":
        process.stdout.write(
          "Usage: scripts/codex-review-commit --sha <commit-sha> --trigger <post-commit|post-push|manual> [--lane <codex>]\n",
        );
        process.exit(0);
        break;
      default:
        throw new Error(`Unknown argument: ${arg}`);
    }
  }

  if (!args.sha || !args.trigger) {
    throw new Error("Missing required --sha or --trigger");
  }
  if (!["post-commit", "post-push", "manual"].includes(args.trigger)) {
    throw new Error(`Invalid --trigger: ${args.trigger}`);
  }
  if (args.lane !== "codex") {
    throw new Error(`Invalid --lane: ${args.lane}`);
  }

  return args;
}

function extendPath(env) {
  const prefixes = [
    "/opt/homebrew/bin",
    "/usr/local/bin",
    path.join(process.env.HOME ?? "", ".volta/bin"),
    path.join(process.env.HOME ?? "", ".asdf/shims"),
  ].filter(Boolean);
  const currentPath = String(env.PATH ?? "");
  const nextPath = [...prefixes, currentPath]
    .filter(Boolean)
    .join(path.delimiter);
  return {
    ...env,
    PATH: nextPath,
  };
}

function parsePositiveInt(value, fallback) {
  const parsed = Number.parseInt(String(value ?? ""), 10);
  if (!Number.isInteger(parsed) || parsed <= 0) {
    return fallback;
  }
  return parsed;
}

export const DEFAULT_COMMIT_REVIEW_CODEX_MODEL = "gpt-5.5";

export function resolveCommitReviewCodexModel(rawValue) {
  const trimmed = String(rawValue ?? "").trim();
  return trimmed || DEFAULT_COMMIT_REVIEW_CODEX_MODEL;
}

function commandAvailable(command, env) {
  const result = spawnSync("sh", ["-lc", `command -v ${command}`], {
    encoding: "utf8",
    stdio: ["ignore", "ignore", "ignore"],
    env,
  });
  return !result.error && result.status === 0;
}

function git(repoRoot, args) {
  return spawnSync("git", args, {
    cwd: repoRoot,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  });
}

function commitExists(repoRoot, sha) {
  const result = git(repoRoot, [
    "rev-parse",
    "--verify",
    "--quiet",
    `${sha}^{commit}`,
  ]);
  return result.status === 0;
}

function codexAuthenticated(env, repoRoot) {
  const result = spawnSync("codex", ["login", "status"], {
    cwd: repoRoot,
    encoding: "utf8",
    stdio: ["ignore", "ignore", "ignore"],
    env,
  });
  return result.status === 0;
}

export function determinePassModes(context) {
  const modes = ["general"];
  for (const mode of context.contractReviewModes ?? []) {
    modes.push(mode);
  }
  if (context.isHighRisk || context.diffBundle.truncated) {
    modes.push("focused");
  }
  return modes;
}

const FINDING_SEVERITY_RANK = {
  minor: 1,
  major: 2,
  blocker: 3,
};

function compactString(value) {
  return String(value ?? "").trim();
}

function normalizeToken(value) {
  return compactString(value)
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}

function parsePassLabels(value) {
  return compactString(value)
    .split(",")
    .map((label) => label.trim())
    .filter(Boolean);
}

function mergePassLabels(...values) {
  return [...new Set(values.flatMap((value) => parsePassLabels(value)))]
    .sort()
    .join(",");
}

function uniqueEvidence(evidence, limit = 6) {
  const merged = [];
  const seen = new Set();
  for (const item of Array.isArray(evidence) ? evidence : []) {
    const file = compactString(item?.file);
    const lines = compactString(item?.lines);
    const reason = compactString(item?.reason);
    const key = `${file}:${lines}:${reason}`;
    if (!key || seen.has(key)) {
      continue;
    }
    seen.add(key);
    merged.push({
      file,
      lines,
      reason,
    });
    if (merged.length >= limit) {
      break;
    }
  }
  return merged;
}

function rootCauseKeys(finding) {
  const keys = [];
  const brokenInvariant = normalizeToken(finding?.broken_invariant);
  const recommendation = normalizeToken(finding?.recommended_direction);
  if (brokenInvariant) {
    keys.push(`inv:${brokenInvariant.slice(0, 140)}`);
  }
  if (recommendation) {
    keys.push(`dir:${recommendation.slice(0, 140)}`);
  }
  return [...new Set(keys)];
}

function findingsShareRootCause(left, right) {
  const leftKeys = new Set(rootCauseKeys(left));
  const rightKeys = rootCauseKeys(right);
  return rightKeys.some((key) => leftKeys.has(key));
}

function strongerFinding(left, right) {
  const severityDelta =
    (FINDING_SEVERITY_RANK[
      String(right?.severity ?? "")
        .trim()
        .toLowerCase()
    ] ?? 0) -
    (FINDING_SEVERITY_RANK[
      String(left?.severity ?? "")
        .trim()
        .toLowerCase()
    ] ?? 0);
  if (severityDelta !== 0) {
    return severityDelta > 0 ? right : left;
  }
  const confidenceDelta =
    Number(right?.confidence ?? 0) - Number(left?.confidence ?? 0);
  if (confidenceDelta !== 0) {
    return confidenceDelta > 0 ? right : left;
  }
  return String(right?.finding_id ?? "").localeCompare(
    String(left?.finding_id ?? ""),
  ) < 0
    ? right
    : left;
}

function mergeRootCauseFindingGroup(left, right) {
  const primary = strongerFinding(left, right);
  const secondary = primary === left ? right : left;
  const primarySeverity = String(primary?.severity ?? "")
    .trim()
    .toLowerCase();
  const secondarySeverity = String(secondary?.severity ?? "")
    .trim()
    .toLowerCase();
  const severity =
    (FINDING_SEVERITY_RANK[secondarySeverity] ?? 0) >
    (FINDING_SEVERITY_RANK[primarySeverity] ?? 0)
      ? secondarySeverity
      : primarySeverity;

  return {
    ...primary,
    severity,
    confidence: Math.max(
      Number(left?.confidence ?? 0),
      Number(right?.confidence ?? 0),
    ),
    evidence: uniqueEvidence([
      ...(left?.evidence ?? []),
      ...(right?.evidence ?? []),
    ]),
    review_pass: mergePassLabels(left?.review_pass, right?.review_pass),
    hypothesis:
      compactString(primary?.hypothesis) ||
      compactString(secondary?.hypothesis),
    impact: compactString(primary?.impact) || compactString(secondary?.impact),
    recommended_direction:
      compactString(primary?.recommended_direction) ||
      compactString(secondary?.recommended_direction),
    broken_invariant:
      compactString(primary?.broken_invariant) ||
      compactString(secondary?.broken_invariant),
    failure_scenario:
      compactString(primary?.failure_scenario) ||
      compactString(secondary?.failure_scenario),
  };
}

export function resolveCommitMaxFindings(
  value = process.env.CODEX_REVIEW_MAX_FINDINGS,
  { isHighRisk = false } = {},
) {
  const parsed = parsePositiveInt(value, 0);
  if (parsed > 0) {
    return parsed;
  }
  return isHighRisk ? HIGH_RISK_MAX_FINDINGS : DEFAULT_MAX_FINDINGS;
}

export function resolveCodexReviewMaxAttempts(
  value = process.env.CODEX_REVIEW_CODEX_MAX_ATTEMPTS ??
    process.env.CODEX_REVIEW_MAX_ATTEMPTS,
) {
  return parsePositiveInt(value, 1);
}

export function isRetryableCodexExecError(errorCode) {
  return (
    String(errorCode ?? "")
      .trim()
      .toLowerCase() === "timeout"
  );
}

export function selectHighSignalCommitFindings(
  findings,
  {
    minConfidence = DEFAULT_FINDING_MIN_CONFIDENCE,
    maxFindings = DEFAULT_MAX_FINDINGS,
  } = {},
) {
  const filtered = (Array.isArray(findings) ? findings : [])
    .filter((finding) => {
      const severity = String(finding?.severity ?? "")
        .trim()
        .toLowerCase();
      if (severity !== "major" && severity !== "blocker") {
        return false;
      }
      const confidence = Number(finding?.confidence ?? 0);
      return Number.isFinite(confidence) && confidence >= minConfidence;
    })
    .sort((left, right) => {
      const stronger = strongerFinding(left, right);
      if (stronger === left && stronger !== right) {
        return -1;
      }
      if (stronger === right && stronger !== left) {
        return 1;
      }
      return String(left?.finding_id ?? "").localeCompare(
        String(right?.finding_id ?? ""),
      );
    });

  const groups = [];
  for (const finding of filtered) {
    const existingIndex = groups.findIndex((group) =>
      findingsShareRootCause(group, finding),
    );
    if (existingIndex === -1) {
      groups.push(finding);
      continue;
    }
    groups[existingIndex] = mergeRootCauseFindingGroup(
      groups[existingIndex],
      finding,
    );
  }

  return groups.slice(0, maxFindings);
}

async function runCodexExec({
  repoRoot,
  env,
  prompt,
  outputPath,
  schemaPath,
  timeoutSeconds,
  verbose,
  model = "",
}) {
  return await new Promise((resolve) => {
    const args = ["-a", "never", "-s", "workspace-write"];
    if (model) {
      args.push("--model", model);
    }
    args.push("exec", "--output-schema", schemaPath, "-o", outputPath, "-");
    const child = spawn("codex", args, {
      cwd: repoRoot,
      env,
      stdio: ["pipe", "pipe", "pipe"],
    });
    let stdoutText = "";
    let stderrText = "";
    let spawnError = null;
    let settled = false;
    let timedOut = false;
    let killTimer = null;
    const timeoutMs = timeoutSeconds * 1000;

    const finish = ({ code = null, signal = "" } = {}) => {
      if (settled) {
        return;
      }
      settled = true;
      if (timeoutId) {
        clearTimeout(timeoutId);
      }
      if (killTimer) {
        clearTimeout(killTimer);
      }
      const outputText = [stdoutText, stderrText]
        .filter(Boolean)
        .join("\n")
        .trim();
      let errorCode = "none";
      if (timedOut) {
        errorCode = "timeout";
      } else if (spawnError || code !== 0 || signal) {
        errorCode = classifyReviewFailure({
          message: spawnError instanceof Error ? spawnError.message : "",
          outputText,
          stderr: stderrText,
          stdout: stdoutText,
        }).code;
      }

      resolve({
        ok: !spawnError && !timedOut && code === 0,
        errorCode,
        outputText,
      });
    };

    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      stdoutText += chunk;
      if (verbose) {
        process.stdout.write(chunk);
      }
    });
    child.stderr.on("data", (chunk) => {
      stderrText += chunk;
      if (verbose) {
        process.stderr.write(chunk);
      }
    });
    child.on("error", (error) => {
      spawnError = error;
    });
    child.on("close", (code, signal) => {
      finish({ code, signal });
    });

    const timeoutId =
      timeoutMs > 0
        ? setTimeout(() => {
            timedOut = true;
            child.kill("SIGTERM");
            killTimer = setTimeout(() => {
              child.kill("SIGKILL");
            }, 5000);
            killTimer.unref?.();
          }, timeoutMs)
        : null;
    timeoutId?.unref?.();

    child.stdin.end(prompt);
  });
}

function fallbackPayload(summary) {
  return {
    schema_version: 2,
    summary,
    findings: [],
  };
}

export async function executeReviewPasses({
  prompts,
  repoRoot,
  env,
  schemaPath,
  timeoutSeconds,
  verbose,
  tempDir,
  sha,
  lane = "codex",
  reviewsDir = "",
  maxAttempts = 1,
  model = "",
  runCodexExecFn = null,
  readOutputFileFn = (outputPath) => readFileSync(outputPath, "utf8"),
  normalizeReviewPayloadFn = normalizeReviewPayload,
} = {}) {
  return executeSharedReviewPasses({
    prompts,
    repoRoot,
    env,
    schemaPath,
    timeoutSeconds,
    verbose,
    tempDir,
    sha,
    lane,
    reviewsDir,
    maxAttempts,
    runCodexExecFn:
      runCodexExecFn ??
      ((args) =>
        runCodexExec({
          ...args,
          model,
        })),
    readOutputFileFn,
    normalizeReviewPayloadFn,
  });
}

export async function executeCodexReviewCommit({
  sha,
  trigger,
  lane = "codex",
  queueEnqueuedAt = "",
  invocationCwd = process.cwd(),
  processEnv = process.env,
} = {}) {
  if (String(processEnv.CODEX_REVIEW_ENABLED ?? "1") === "0") {
    throw new Error("Skipping review: CODEX_REVIEW_ENABLED=0");
  }

  const env = extendPath(processEnv);
  const repoRoot = resolveRepoRoot(invocationCwd);
  const repoName = path.basename(repoRoot);

  if (lane !== "codex") {
    throw new Error(`Invalid --lane: ${lane}`);
  }

  if (!commitExists(repoRoot, sha)) {
    throw new Error(`Skipping review: commit not found: ${sha}`);
  }
  if (!commandAvailable("codex", env)) {
    throw new Error("Skipping review: codex CLI is not available in PATH");
  }
  if (!codexAuthenticated(env, repoRoot)) {
    throw new Error(
      "Skipping review: codex is not authenticated (run: codex login)",
    );
  }

  const reviewsDir = await resolveReviewsDir(
    repoRoot,
    processEnv.CODE_REVIEW_DIR ??
      processEnv.CODEX_REVIEW_DIR ??
      path.join(repoRoot, ".code-reviews"),
    repoRoot,
  );
  const actionFileJson = path.join(reviewsDir, `${sha}.json`);
  const actionFileMd = path.join(reviewsDir, `${sha}.md`);
  const schemaPath = path.join(
    repoRoot,
    "scripts",
    "codex-review-output.schema.json",
  );
  const openOnFindings =
    String(processEnv.CODEX_REVIEW_OPEN_ON_FINDINGS ?? "1") === "1";
  const reviewTimeoutSeconds = parsePositiveInt(
    processEnv.CODEX_REVIEW_CODEX_TIMEOUT_SECONDS ??
      processEnv.CODEX_REVIEW_TIMEOUT_SECONDS,
    600,
  );
  const reviewMaxAttempts = resolveCodexReviewMaxAttempts(
    processEnv.CODEX_REVIEW_CODEX_MAX_ATTEMPTS ??
      processEnv.CODEX_REVIEW_MAX_ATTEMPTS,
  );
  const reviewModel = resolveCommitReviewCodexModel(
    processEnv.CODEX_REVIEW_CODEX_MODEL,
  );
  const reviewVerbose = String(processEnv.CODEX_REVIEW_VERBOSE ?? "0") === "1";
  const findingModelLabel = String(
    processEnv.CODEX_REVIEW_CODEX_MODEL_LABEL ?? reviewModel,
  );
  const promptMaxChars = parsePositiveInt(
    processEnv.CODEX_REVIEW_PROMPT_MAX_CHARS,
    DEFAULT_PROMPT_MAX_CHARS,
  );
  const filePromptMaxChars = parsePositiveInt(
    processEnv.CODEX_REVIEW_PROMPT_MAX_FILE_CHARS,
    DEFAULT_PROMPT_MAX_FILE_CHARS,
  );
  const generalPrompt = buildReviewPrompt({
    repoRoot,
    sha,
    trigger,
    promptMaxChars,
    filePromptMaxChars,
    mode: "general",
  });
  const maxFindings = resolveCommitMaxFindings(
    processEnv.CODEX_REVIEW_MAX_FINDINGS,
    { isHighRisk: generalPrompt.context.isHighRisk },
  );
  const passModes = determinePassModes(generalPrompt.context);
  const prompts = passModes.map((mode) =>
    mode === "general"
      ? { mode, prompt: generalPrompt.prompt }
      : {
          mode,
          prompt: buildReviewPrompt({
            repoRoot,
            sha,
            trigger,
            promptMaxChars,
            filePromptMaxChars,
            mode,
            context: generalPrompt.context,
          }).prompt,
        },
  );

  const tempDir = mkdtempSync(path.join(reviewsDir, `${sha}.${trigger}.`));
  let reviewStatus = "ok";
  let codexStatus = "ok";
  let codexError = "none";
  let summaries = [];
  let aggregatedFindings = [];
  let completedModes = [];
  let codexAttempts = 0;

  try {
    ({
      reviewStatus,
      codexStatus,
      codexError,
      summaries,
      aggregatedFindings,
      completedModes,
      codexAttempts,
    } = await executeReviewPasses({
      prompts,
      repoRoot,
      env,
      schemaPath,
      timeoutSeconds: reviewTimeoutSeconds,
      verbose: reviewVerbose,
      tempDir,
      sha,
      lane,
      reviewsDir,
      maxAttempts: reviewMaxAttempts,
      model: reviewModel,
    }));
  } finally {
    rmSync(tempDir, { force: true, recursive: true });
  }

  let aggregateData;
  if (completedModes.length === 0) {
    aggregateData = fallbackPayload(
      summaries.join(" ").trim() || `Codex review failed for commit ${sha}.`,
    );
  } else {
    const mergedFindings = mergeReviewFindings(aggregatedFindings);
    aggregateData = {
      schema_version: 2,
      summary: summaries.filter(Boolean).join("\n").trim(),
      findings: selectHighSignalCommitFindings(mergedFindings, { maxFindings }),
    };
    verifyFindings(aggregateData, repoRoot);
  }

  const result = writeReviewArtifacts({
    newData: aggregateData,
    targetJson: actionFileJson,
    targetMd: actionFileMd,
    commitSha: sha,
    triggerName: trigger,
    reviewStatus,
    findingModelLabel,
    repoName,
    repoRoot,
    reviewLane: lane,
    queueEnqueuedAt,
    failureReason: reviewStatus !== "ok" ? codexError : "",
    reviewEngines: {
      codex: {
        status: codexStatus,
        error_code: codexError,
        attempts: codexAttempts,
        completed: true,
        completed_at: new Date().toISOString().replace(/\.\d{3}Z$/, "Z"),
        model: findingModelLabel,
        review_passes: completedModes,
      },
    },
  });
  const persistedSidecar = JSON.parse(readFileSync(actionFileJson, "utf8"));

  if (openOnFindings && result.actionRequired && actionFileMd) {
    const openResult = spawnSync("open", [actionFileMd], {
      cwd: repoRoot,
      stdio: ["ignore", "ignore", "ignore"],
    });
    void openResult;
  }

  return {
    ...result,
    sidecar: persistedSidecar,
    repoRoot,
    reviewsDir,
    actionFileJson,
    actionFileMd,
    reviewStatus,
    codexStatus,
    codexError,
    codexAttempts,
    completedModes,
  };
}

async function main() {
  const args = parseArgs(process.argv);
  const result = await executeCodexReviewCommit(args);

  if (!result) {
    return;
  }

  process.stderr.write(
    `[codex-review-commit] lane=codex sha=${args.sha} trigger=${args.trigger} review_status=${result.reviewStatus} codex_status=${result.codexStatus} codex_error=${result.codexError} codex_attempts=${result.codexAttempts} codex_findings=${result.findingsCount} wrote_action=${result.actionRequired ? 1 : 0}\n`,
  );

  if (result.reviewStatus === "infra_error") {
    process.exitCode = 1;
  }
}

if (
  import.meta.url === pathToFileURL(path.resolve(process.argv[1] ?? "")).href
) {
  main().catch((error) => {
    process.stderr.write(
      `${error instanceof Error ? error.message : String(error)}\n`,
    );
    process.exitCode = 1;
  });
}
