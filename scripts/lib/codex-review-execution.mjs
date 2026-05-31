import path from "node:path";
import {
  classifyReviewFailure,
  isRetryableReviewFailureKind,
  shouldRequeueReviewFailureKind,
} from "./codex-review-failure.mjs";
import {
  clearReviewCheckpoint,
  readReviewCheckpoint,
  writeReviewCheckpoint,
} from "./codex-review-checkpoint.mjs";

function annotatePass(payload, mode) {
  const findings = Array.isArray(payload?.findings) ? payload.findings : [];
  return {
    ...payload,
    findings: findings.map((finding) => ({
      ...finding,
      review_pass: String(finding?.review_pass ?? "").trim() || mode,
    })),
  };
}

function nextPendingMode(prompts, completedModes) {
  const completed = new Set(completedModes);
  const nextPrompt = (Array.isArray(prompts) ? prompts : []).find(
    (prompt) => !completed.has(String(prompt?.mode ?? "").trim()),
  );
  return String(nextPrompt?.mode ?? "").trim();
}

function summaryMode(summary) {
  const match = /^\[([^\]]+)]/.exec(String(summary ?? "").trim());
  return String(match?.[1] ?? "").trim();
}

function findingModes(finding) {
  return String(finding?.review_pass ?? "")
    .split(",")
    .map((mode) => mode.trim())
    .filter(Boolean);
}

function normalizeCheckpointState(checkpoint, prompts) {
  if (!checkpoint) {
    return {
      completedModes: [],
      summaries: [],
      aggregatedFindings: [],
      codexAttempts: 0,
    };
  }
  const orderedModes = [];
  const allowedModes = new Set(
    (Array.isArray(prompts) ? prompts : [])
      .map((prompt) => String(prompt?.mode ?? "").trim())
      .filter(Boolean),
  );
  const completedFromCheckpoint = new Set(
    (Array.isArray(checkpoint.completed_modes)
      ? checkpoint.completed_modes
      : []
    ).filter((mode) => allowedModes.has(mode)),
  );
  for (const prompt of Array.isArray(prompts) ? prompts : []) {
    const mode = String(prompt?.mode ?? "").trim();
    if (mode && completedFromCheckpoint.has(mode)) {
      orderedModes.push(mode);
    }
  }
  const completedModeSet = new Set(orderedModes);
  return {
    completedModes: orderedModes,
    summaries: Array.isArray(checkpoint.summaries)
      ? checkpoint.summaries.filter((summary) =>
          completedModeSet.has(summaryMode(summary)),
        )
      : [],
    aggregatedFindings: Array.isArray(checkpoint.aggregated_findings)
      ? checkpoint.aggregated_findings.filter((finding) =>
          findingModes(finding).some((mode) => completedModeSet.has(mode)),
        )
      : [],
    codexAttempts: Math.max(0, Number(checkpoint.codex_attempts ?? 0) || 0),
  };
}

function partialFailureContext(hasFindings) {
  return hasFindings
    ? "after earlier findings were collected"
    : "after earlier review passes completed";
}

function buildFailureSummary(
  kind,
  passMode,
  sha,
  { partial = false, hasFindings = false } = {},
) {
  const partialContext = partialFailureContext(hasFindings);
  if (kind === "timeout") {
    return partial
      ? `Codex review timed out during the ${passMode} pass ${partialContext}.`
      : `Codex review timed out during the ${passMode} pass for commit ${sha}.`;
  }
  if (kind === "exec_failed") {
    return partial
      ? `Codex review failed during the ${passMode} pass ${partialContext}.`
      : `Codex review failed during the ${passMode} pass for commit ${sha}.`;
  }
  if (kind === "rate_limit") {
    return `Codex review hit a rate limit during the ${passMode} pass for commit ${sha}.`;
  }
  if (kind === "hard_quota") {
    return `Codex review hit a hard quota limit during the ${passMode} pass for commit ${sha}.`;
  }
  if (kind === "system_error") {
    return `Codex review hit a system error during the ${passMode} pass for commit ${sha}.`;
  }
  if (kind === "transient_transport") {
    return `Codex review hit a transient transport error during the ${passMode} pass for commit ${sha}.`;
  }
  return `Codex review failed during the ${passMode} pass for commit ${sha}.`;
}

export class ReviewExecutionError extends Error {
  constructor(
    message,
    {
      failureKind = "exec_failed",
      code = "exec_failed",
      outputText = "",
      completedModes = [],
      summaries = [],
      aggregatedFindings = [],
      codexAttempts = 0,
      passMode = "",
      lane = "codex",
    } = {},
  ) {
    super(message);
    this.name = "ReviewExecutionError";
    this.failureKind = failureKind;
    this.codexError = code;
    this.outputText = String(outputText ?? "");
    this.completedModes = [...completedModes];
    this.summaries = [...summaries];
    this.aggregatedFindings = [...aggregatedFindings];
    this.codexAttempts = codexAttempts;
    this.passMode = passMode;
    this.lane = lane;
  }
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
  runCodexExecFn,
  readOutputFileFn,
  normalizeReviewPayloadFn,
  readCheckpointFn = readReviewCheckpoint,
  writeCheckpointFn = writeReviewCheckpoint,
  clearCheckpointFn = clearReviewCheckpoint,
} = {}) {
  let reviewStatus = "ok";
  let codexStatus = "ok";
  let codexError = "none";

  const checkpointState = normalizeCheckpointState(
    reviewsDir && sha ? await readCheckpointFn(reviewsDir, sha, lane) : null,
    prompts,
  );
  const summaries = [...checkpointState.summaries];
  const aggregatedFindings = [...checkpointState.aggregatedFindings];
  const completedModes = [...checkpointState.completedModes];
  const completedSet = new Set(completedModes);
  let codexAttempts = checkpointState.codexAttempts;

  for (const pass of Array.isArray(prompts) ? prompts : []) {
    const mode = String(pass?.mode ?? "").trim();
    if (!mode || completedSet.has(mode)) {
      continue;
    }

    const outputPath = path.join(tempDir, `${mode}.json`);
    let finalFailure = null;

    for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
      codexAttempts += 1;
      const outcome = await runCodexExecFn({
        repoRoot,
        env,
        prompt: pass.prompt,
        outputPath,
        schemaPath,
        timeoutSeconds,
        verbose,
      });

      if (outcome.ok) {
        try {
          const normalized = annotatePass(
            normalizeReviewPayloadFn(JSON.parse(readOutputFileFn(outputPath))),
            mode,
          );
          summaries.push(`[${mode}] ${normalized.summary}`.trim());
          aggregatedFindings.push(...normalized.findings);
          completedModes.push(mode);
          completedSet.add(mode);
          if (reviewsDir && sha) {
            await writeCheckpointFn(reviewsDir, {
              sha,
              lane,
              completedModes,
              summaries,
              aggregatedFindings,
              nextMode: nextPendingMode(prompts, completedModes),
              codexAttempts,
            });
          }
          finalFailure = null;
          break;
        } catch (error) {
          finalFailure = {
            ok: false,
            errorCode: "invalid_output",
            outputText: error instanceof Error ? error.message : String(error),
          };
          break;
        }
      }

      const classified = classifyReviewFailure(outcome);
      finalFailure = {
        ...outcome,
        classification: classified,
      };

      if (
        isRetryableReviewFailureKind(classified.kind) &&
        attempt < maxAttempts
      ) {
        continue;
      }
      break;
    }

    if (!finalFailure) {
      continue;
    }

    const classified =
      finalFailure.classification ?? classifyReviewFailure(finalFailure);
    codexError = classified.code;

    if (shouldRequeueReviewFailureKind(classified.kind)) {
      if (reviewsDir && sha && completedModes.length > 0) {
        await writeCheckpointFn(reviewsDir, {
          sha,
          lane,
          completedModes,
          summaries,
          aggregatedFindings,
          nextMode: mode,
          codexAttempts,
          lastErrorKind: classified.kind,
          lastErrorCode: classified.code,
        });
      }
      throw new ReviewExecutionError(
        buildFailureSummary(classified.kind, mode, sha),
        {
          failureKind: classified.kind,
          code: classified.code,
          outputText: finalFailure.outputText ?? "",
          completedModes,
          summaries,
          aggregatedFindings,
          codexAttempts,
          passMode: mode,
          lane,
        },
      );
    }

    if (completedModes.length === 0) {
      reviewStatus = "infra_error";
      codexStatus = "failed";
      summaries.push(buildFailureSummary(classified.kind, mode, sha));
      if (reviewsDir && sha) {
        await clearCheckpointFn(reviewsDir, sha, lane);
      }
      break;
    }

    reviewStatus = "partial_success";
    summaries.push(
      buildFailureSummary(classified.kind, mode, sha, {
        partial: true,
        hasFindings: aggregatedFindings.length > 0,
      }),
    );
    if (reviewsDir && sha) {
      await writeCheckpointFn(reviewsDir, {
        sha,
        lane,
        completedModes,
        summaries,
        aggregatedFindings,
        nextMode: mode,
        codexAttempts,
        lastErrorKind: classified.kind,
        lastErrorCode: classified.code,
      });
    }
    break;
  }

  if (reviewStatus === "ok" && reviewsDir && sha) {
    await clearCheckpointFn(reviewsDir, sha, lane);
  }

  return {
    reviewStatus,
    codexStatus,
    codexError,
    summaries,
    aggregatedFindings,
    completedModes,
    codexAttempts,
  };
}
