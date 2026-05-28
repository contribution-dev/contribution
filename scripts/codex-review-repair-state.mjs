#!/usr/bin/env node

import {
  readdir,
  readFile,
  rename,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import {
  reportsDirForLane,
  resolveRepoRoot,
  resolveReviewsDir,
} from "./codex-review-inbox-lib.mjs";
import { writeReviewMarkdownProjection } from "./lib/codex-review-findings.mjs";
import {
  isActionableCodexReviewReport,
  hasDurableUiRuntimeReviewEvidence,
  hasDurableReviewEvidence,
  isQueueRecoveryReport,
  isSyntheticFailurePlaceholder,
  reportSatisfiesLane,
} from "./lib/codex-review-state.mjs";
import { renderMarkdownReport as renderUiRuntimeMarkdown } from "./ui-runtime-review-report";
import {
  enqueueReviewJob,
  ensureReviewQueue,
} from "./codex-review-queue-lib.mjs";

function usage() {
  console.log(`Usage: scripts/codex-review-repair-state [options]

Options:
  --repo-root <path>         Repository root (default: cwd repo root)
  --reviews-dir <path>       Override review artifacts directory
  --dry-run                  Report planned actions without mutating
  -h, --help                 Show help
`);
}

function parseArgs(argv) {
  const args = {
    repoRoot: "",
    reviewsDir: "",
    dryRun: false,
  };
  for (let index = 2; index < argv.length; index += 1) {
    const arg = argv[index];
    const next = argv[index + 1];
    switch (arg) {
      case "--repo-root":
        args.repoRoot = String(next ?? "").trim();
        index += 1;
        break;
      case "--reviews-dir":
        args.reviewsDir = String(next ?? "").trim();
        index += 1;
        break;
      case "--dry-run":
        args.dryRun = true;
        break;
      case "-h":
      case "--help":
        usage();
        process.exit(0);
        break;
      default:
        throw new Error(`Unknown argument: ${arg}`);
    }
  }
  return args;
}

function parseArtifactName(fileName) {
  const match = /^([0-9a-f]{40})(\.ui-runtime)?\.(json|md)$/.exec(
    String(fileName ?? "").trim(),
  );
  if (!match) {
    return null;
  }
  return {
    sha: match[1],
    kind: match[2] ? "ui-runtime" : "codex",
    extension: match[3],
    baseName: `${match[1]}${match[2] ? ".ui-runtime" : ""}`,
  };
}

async function readJsonIfPresent(filePath) {
  try {
    return JSON.parse(await readFile(filePath, "utf8"));
  } catch {
    return null;
  }
}

async function fileHasNonWhitespace(filePath) {
  try {
    const raw = await readFile(filePath, "utf8");
    return /[^\s]/.test(raw);
  } catch {
    return false;
  }
}

function firstFindingModel(report) {
  return Array.isArray(report?.finding_models)
    ? String(report.finding_models[0] ?? "").trim()
    : "";
}

function reportKindFromPayload(report, fallbackKind = "codex") {
  const targetType = String(report?.review_target?.type ?? "")
    .trim()
    .toLowerCase();
  if (targetType === "ui-runtime") {
    return "ui-runtime";
  }
  const lane = String(report?.lane ?? "")
    .trim()
    .toLowerCase();
  if (lane === "codex" || lane === "ui-runtime") {
    return lane;
  }
  const codexStatus = String(report?.review_engines?.codex?.status ?? "")
    .trim()
    .toLowerCase();
  if (codexStatus) {
    return "codex";
  }
  if (isQueueRecoveryReport(report)) {
    return "codex";
  }
  const reportSha = String(report?.sha ?? "")
    .trim()
    .toLowerCase();
  if (reportSha) {
    return "codex";
  }
  return fallbackKind;
}

function isRenderableUiRuntimeReport(report, sha = "") {
  if (!report || typeof report !== "object" || Array.isArray(report)) {
    return false;
  }
  const targetSha = String(report?.review_target?.sha ?? "")
    .trim()
    .toLowerCase();
  if (!targetSha) {
    return false;
  }
  if (sha && targetSha !== String(sha).trim().toLowerCase()) {
    return false;
  }
  return true;
}

async function regenerateMarkdownProjection({
  repoRoot,
  reviewsDir,
  sha,
  report,
  dryRun,
}) {
  const targetMd = path.join(reviewsDir, `${sha}.md`);
  if (dryRun) {
    return;
  }
  const sidecar = {
    ...report,
    sha,
    lane: String(report?.lane ?? "codex"),
    repository: {
      name: String(report?.repository?.name ?? path.basename(repoRoot)),
      root: String(report?.repository?.root ?? repoRoot),
    },
    trigger_last: String(report?.trigger_last ?? "manual"),
    triggers_seen: Array.isArray(report?.triggers_seen)
      ? report.triggers_seen.map(String).filter(Boolean)
      : [String(report?.trigger_last ?? "manual")],
    review_status: String(report?.review_status ?? "ok"),
    summary: String(report?.summary ?? ""),
    finding_models: Array.isArray(report?.finding_models)
      ? report.finding_models.map(String).filter(Boolean)
      : firstFindingModel(report)
        ? [firstFindingModel(report)]
        : [],
    findings: Array.isArray(report?.findings) ? report.findings : [],
  };
  writeReviewMarkdownProjection({
    sidecar,
    targetMd,
  });
}

async function regenerateUiRuntimeMarkdownProjection({
  reviewsDir,
  baseName,
  report,
  dryRun,
}) {
  if (dryRun) {
    return;
  }
  await writeFile(
    path.join(reviewsDir, `${baseName}.md`),
    renderUiRuntimeMarkdown(report),
    "utf8",
  );
}

async function relocateCanonicalRuntimeArtifact({
  reviewsDir,
  currentBaseName,
  sha,
  currentReport,
  dryRun,
}) {
  const currentJsonPath = path.join(reviewsDir, `${currentBaseName}.json`);
  const currentMdPath = path.join(reviewsDir, `${currentBaseName}.md`);
  const targetBaseName = `${sha}.ui-runtime`;
  const targetJsonPath = path.join(reviewsDir, `${targetBaseName}.json`);
  const targetMdPath = path.join(reviewsDir, `${targetBaseName}.md`);

  if (dryRun) {
    const targetReport =
      currentBaseName === targetBaseName
        ? currentReport
        : await readJsonIfPresent(targetJsonPath);
    return {
      baseName: targetBaseName,
      report:
        currentBaseName === targetBaseName
          ? currentReport
          : isRenderableUiRuntimeReport(targetReport, sha)
            ? targetReport
            : currentReport,
    };
  }

  if (currentBaseName === targetBaseName) {
    return {
      baseName: targetBaseName,
      report: currentReport,
    };
  }

  const targetExists = await stat(targetJsonPath).catch(() => null);
  if (targetExists?.isFile()) {
    const targetReport = await readJsonIfPresent(targetJsonPath);
    if (isRenderableUiRuntimeReport(targetReport, sha)) {
      await rm(currentJsonPath, { force: true });
      await rm(currentMdPath, { force: true });
      return {
        baseName: targetBaseName,
        report: targetReport,
      };
    }
    await rm(targetJsonPath, { force: true });
    await rm(targetMdPath, { force: true });
  }

  await rename(currentJsonPath, targetJsonPath).catch(async () => {
    await rm(targetJsonPath, { force: true });
    await rename(currentJsonPath, targetJsonPath);
  });
  await rename(currentMdPath, targetMdPath).catch(async () => {
    const mdExists = await stat(currentMdPath).catch(() => null);
    if (!mdExists?.isFile()) {
      return;
    }
    await rm(targetMdPath, { force: true });
    await rename(currentMdPath, targetMdPath);
  });

  return {
    baseName: targetBaseName,
    report: currentReport,
  };
}

async function relocateCanonicalCodexArtifact({
  reviewsDir,
  currentBaseName,
  sha,
  currentReport,
  dryRun,
}) {
  const currentJsonPath = path.join(reviewsDir, `${currentBaseName}.json`);
  const currentMdPath = path.join(reviewsDir, `${currentBaseName}.md`);
  const targetBaseName = sha;
  const targetJsonPath = path.join(reviewsDir, `${targetBaseName}.json`);
  const targetMdPath = path.join(reviewsDir, `${targetBaseName}.md`);

  if (dryRun) {
    const targetReport =
      currentBaseName === targetBaseName
        ? currentReport
        : await readJsonIfPresent(targetJsonPath);
    return {
      baseName: targetBaseName,
      report:
        currentBaseName === targetBaseName
          ? currentReport
          : reportKindFromPayload(targetReport, "codex") === "codex"
            ? targetReport
            : currentReport,
    };
  }

  if (currentBaseName === targetBaseName) {
    return {
      baseName: targetBaseName,
      report: currentReport,
    };
  }

  const targetExists = await stat(targetJsonPath).catch(() => null);
  if (targetExists?.isFile()) {
    const targetReport = await readJsonIfPresent(targetJsonPath);
    if (reportKindFromPayload(targetReport, "codex") === "codex") {
      const currentBeatsTarget =
        hasDurableReviewEvidence(currentReport, "codex") &&
        (isQueueRecoveryReport(targetReport) ||
          isSyntheticFailurePlaceholder(targetReport, "codex") ||
          !hasDurableReviewEvidence(targetReport, "codex"));
      if (currentBeatsTarget) {
        await rm(targetJsonPath, { force: true });
        await rm(targetMdPath, { force: true });
      } else {
        await rm(currentJsonPath, { force: true });
        await rm(currentMdPath, { force: true });
        return {
          baseName: targetBaseName,
          report: targetReport,
        };
      }
    }
    if (!(reportKindFromPayload(targetReport, "codex") === "codex")) {
      await rm(targetJsonPath, { force: true });
      await rm(targetMdPath, { force: true });
    }
  }

  await rename(currentJsonPath, targetJsonPath).catch(async () => {
    await rm(targetJsonPath, { force: true });
    await rename(currentJsonPath, targetJsonPath);
  });
  await rename(currentMdPath, targetMdPath).catch(async () => {
    const mdExists = await stat(currentMdPath).catch(() => null);
    if (!mdExists?.isFile()) {
      return;
    }
    await rm(targetMdPath, { force: true });
    await rename(currentMdPath, targetMdPath);
  });

  return {
    baseName: targetBaseName,
    report: currentReport,
  };
}

function hasPromotableMisplacedReport(report, lane) {
  if (lane === "ui-runtime") {
    return hasDurableUiRuntimeReviewEvidence(report);
  }
  return hasDurableReviewEvidence(report, lane);
}

function isSyntheticUiRuntimePlaceholder(report) {
  if (!report || typeof report !== "object" || Array.isArray(report)) {
    return false;
  }
  const reviewStatus = String(report?.review_status ?? "")
    .trim()
    .toLowerCase();
  const lastReviewed = String(report?.last_reviewed ?? "").trim();
  const findings = Array.isArray(report?.findings) ? report.findings : [];
  return Boolean(
    reviewStatus &&
    reviewStatus !== "ok" &&
    !lastReviewed &&
    findings.length === 0,
  );
}

async function repairMisplacedLogReports({ repoRoot, reviewsDir, dryRun }) {
  const logsDir = path.join(reviewsDir, "logs");
  const entries = await readdir(logsDir, { withFileTypes: true }).catch(
    () => [],
  );
  let moved = 0;
  let deleted = 0;

  for (const entry of entries) {
    if (!entry.isFile() || !entry.name.endsWith(".json")) continue;
    const artifact = parseArtifactName(entry.name);
    if (!artifact) continue;

    const misplacedJsonPath = path.join(logsDir, `${artifact.baseName}.json`);
    const misplacedMdPath = path.join(logsDir, `${artifact.baseName}.md`);
    const targetJsonPath = path.join(reviewsDir, `${artifact.baseName}.json`);
    const targetMdPath = path.join(reviewsDir, `${artifact.baseName}.md`);
    const [misplacedReport, canonicalReport] = await Promise.all([
      readJsonIfPresent(misplacedJsonPath),
      readJsonIfPresent(targetJsonPath),
    ]);
    if (!misplacedReport) continue;

    const artifactLane =
      artifact.kind === "ui-runtime" ? "ui-runtime" : "codex";
    const canonicalIsSyntheticPlaceholder =
      isSyntheticFailurePlaceholder(canonicalReport, artifactLane) ||
      (artifactLane === "ui-runtime" &&
        isSyntheticUiRuntimePlaceholder(canonicalReport));
    const shouldPromote =
      !canonicalReport ||
      isQueueRecoveryReport(canonicalReport) ||
      (canonicalIsSyntheticPlaceholder &&
        hasPromotableMisplacedReport(misplacedReport, artifactLane));
    if (shouldPromote) {
      if (!dryRun) {
        if (canonicalReport) {
          await rm(targetJsonPath, { force: true });
          await rm(targetMdPath, { force: true });
        }
        await rename(misplacedJsonPath, targetJsonPath).catch(async () => {
          await rm(targetJsonPath, { force: true });
          await rename(misplacedJsonPath, targetJsonPath);
        });
        await rename(misplacedMdPath, targetMdPath).catch(() => {});
      }
      moved += 1;
      continue;
    }

    if (!dryRun) {
      await rm(misplacedJsonPath, { force: true });
      await rm(misplacedMdPath, { force: true });
    }
    deleted += 1;
  }

  return { moved, deleted };
}

async function repairCanonicalReports({ repoRoot, reviewsDir, dryRun }) {
  const reportsDir = reportsDirForLane(reviewsDir, "codex");
  const entries = await readdir(reportsDir, { withFileTypes: true }).catch(
    () => [],
  );
  const rerunShas = new Set();
  let regeneratedMarkdown = 0;
  let removedQueueRecovery = 0;
  let removedOrphanMarkdown = 0;
  let relocatedCanonicalRuntimeArtifacts = 0;
  let invariantWarnings = 0;

  for (const entry of entries) {
    if (
      !entry.isFile() ||
      !entry.name.endsWith(".json") ||
      entry.name === ".ack.json"
    )
      continue;
    const artifact = parseArtifactName(entry.name);
    if (!artifact) continue;
    const sha = artifact.sha;
    let baseName = artifact.baseName;
    let jsonPath = path.join(reportsDir, `${baseName}.json`);
    let mdPath = path.join(reportsDir, `${baseName}.md`);
    let report = await readJsonIfPresent(jsonPath);
    if (!report) continue;
    const payloadKind = reportKindFromPayload(report, artifact.kind);

    if (payloadKind === "ui-runtime") {
      const relocation = await relocateCanonicalRuntimeArtifact({
        reviewsDir,
        currentBaseName: baseName,
        sha,
        currentReport: report,
        dryRun,
      });
      if (relocation.baseName !== baseName) {
        relocatedCanonicalRuntimeArtifacts += 1;
        baseName = relocation.baseName;
        mdPath = path.join(reportsDir, `${baseName}.md`);
      }
      report = relocation.report;

      const mdInfo = await stat(mdPath).catch(() => null);
      const mdHasText = mdInfo?.isFile()
        ? await fileHasNonWhitespace(mdPath)
        : false;
      if (!mdHasText) {
        if (!isRenderableUiRuntimeReport(report, sha)) {
          invariantWarnings += 1;
          continue;
        }
        await regenerateUiRuntimeMarkdownProjection({
          reviewsDir,
          baseName,
          report,
          dryRun,
        });
        regeneratedMarkdown += 1;
      }
      continue;
    }

    if (payloadKind !== "codex") {
      continue;
    }

    const relocation = await relocateCanonicalCodexArtifact({
      reviewsDir,
      currentBaseName: baseName,
      sha,
      currentReport: report,
      dryRun,
    });
    baseName = relocation.baseName;
    jsonPath = path.join(reportsDir, `${baseName}.json`);
    mdPath = path.join(reportsDir, `${baseName}.md`);
    report = relocation.report;

    if (isQueueRecoveryReport(report)) {
      if (!dryRun) {
        await rm(jsonPath, { force: true });
        await rm(mdPath, { force: true });
      }
      removedQueueRecovery += 1;
      rerunShas.add(sha);
      continue;
    }

    const actionable = isActionableCodexReviewReport(report, true);
    const mdInfo = await stat(mdPath).catch(() => null);
    const mdHasText = mdInfo?.isFile()
      ? await fileHasNonWhitespace(mdPath)
      : false;

    if (actionable && !mdHasText) {
      await regenerateMarkdownProjection({
        repoRoot,
        reviewsDir,
        sha,
        report,
        dryRun,
      });
      regeneratedMarkdown += 1;
      continue;
    }

    if (!actionable && mdInfo?.isFile()) {
      if (!dryRun) {
        await rm(mdPath, { force: true });
      }
      removedOrphanMarkdown += 1;
    }

    if (!actionable && isQueueRecoveryReport(report)) {
      invariantWarnings += 1;
    }
  }

  return {
    regeneratedMarkdown,
    removedQueueRecovery,
    removedOrphanMarkdown,
    relocatedCanonicalRuntimeArtifacts,
    rerunShas,
    invariantWarnings,
  };
}

async function repairLegacyQueueOutcomeFiles({ reviewsDir, dryRun }) {
  const rerunShas = new Set();
  let removedLegacyOutcomeJobs = 0;
  const legacyOutcomePathsBySha = new Map();

  for (const status of ["completed", "failed"]) {
    const candidateDirs = [
      path.join(reviewsDir, "queue", "codex", status),
      path.join(reviewsDir, "queue", status),
    ];
    for (const dirPath of candidateDirs) {
      const entries = await readdir(dirPath, { withFileTypes: true }).catch(
        () => [],
      );
      for (const entry of entries) {
        if (!entry.isFile() || !entry.name.endsWith(".json")) continue;
        const sourcePath = path.join(dirPath, entry.name);
        const artifact = parseArtifactName(entry.name);
        if (!artifact || artifact.kind !== "codex") continue;
        const paths = legacyOutcomePathsBySha.get(artifact.sha) ?? [];
        paths.push(sourcePath);
        legacyOutcomePathsBySha.set(artifact.sha, paths);
      }
    }
  }

  for (const [sha, sourcePaths] of legacyOutcomePathsBySha.entries()) {
    const canonicalReport = await readJsonIfPresent(
      path.join(reviewsDir, `${sha}.json`),
    );
    if (!canonicalReport || isQueueRecoveryReport(canonicalReport)) {
      rerunShas.add(sha);
    }
    if (dryRun) {
      removedLegacyOutcomeJobs += sourcePaths.length;
    }
  }

  return { removedLegacyOutcomeJobs, rerunShas, legacyOutcomePathsBySha };
}

export async function repairLegacyReviewState({
  repoRoot = resolveRepoRoot(process.cwd()),
  reviewsDir = "",
  dryRun = false,
} = {}) {
  const resolvedReviewsDir = await resolveReviewsDir(
    repoRoot,
    reviewsDir,
    repoRoot,
  );
  const movedLogs = await repairMisplacedLogReports({
    repoRoot,
    reviewsDir: resolvedReviewsDir,
    dryRun,
  });
  const canonical = await repairCanonicalReports({
    repoRoot,
    reviewsDir: resolvedReviewsDir,
    dryRun,
  });
  const legacyQueue = await repairLegacyQueueOutcomeFiles({
    reviewsDir: resolvedReviewsDir,
    dryRun,
  });

  const rerunShas = new Set([...canonical.rerunShas, ...legacyQueue.rerunShas]);
  let removedLegacyOutcomeJobs = legacyQueue.removedLegacyOutcomeJobs;
  if (!dryRun) {
    for (const [
      sha,
      sourcePaths,
    ] of legacyQueue.legacyOutcomePathsBySha.entries()) {
      if (legacyQueue.rerunShas.has(sha)) {
        await enqueueReviewJob({
          reviewsDir: resolvedReviewsDir,
          sha,
          lane: "codex",
          trigger: "manual",
          source: "repair",
          reason: "legacy-outcome-without-canonical-report",
          force: true,
        });
      }
      for (const sourcePath of sourcePaths) {
        await rm(sourcePath, { force: true });
        removedLegacyOutcomeJobs += 1;
      }
    }
    for (const sha of rerunShas) {
      if (legacyQueue.rerunShas.has(sha)) {
        continue;
      }
      await enqueueReviewJob({
        reviewsDir: resolvedReviewsDir,
        sha,
        lane: "codex",
        trigger: "manual",
        source: "repair",
        reason: "missing-canonical-report",
        force: true,
      });
    }
  }

  return {
    reviewsDir: resolvedReviewsDir,
    movedMisplacedLogReports: movedLogs.moved,
    deletedMisplacedLogReports: movedLogs.deleted,
    regeneratedMarkdown: canonical.regeneratedMarkdown,
    removedQueueRecoveryReports: canonical.removedQueueRecovery,
    removedOrphanMarkdown: canonical.removedOrphanMarkdown,
    relocatedCanonicalRuntimeArtifacts:
      canonical.relocatedCanonicalRuntimeArtifacts,
    removedLegacyOutcomeJobs,
    requeuedMissingCanonicalReports: rerunShas.size,
    invariantWarnings: canonical.invariantWarnings,
  };
}

async function main() {
  const args = parseArgs(process.argv);
  const repoRoot = args.repoRoot
    ? resolveRepoRoot(path.resolve(args.repoRoot))
    : resolveRepoRoot(process.cwd());
  const result = await repairLegacyReviewState({
    repoRoot,
    reviewsDir: args.reviewsDir,
    dryRun: args.dryRun,
  });
  console.log(
    `[codex-review-repair-state] ${args.dryRun ? "would_repair" : "repaired"} moved_logs=${result.movedMisplacedLogReports} deleted_logs=${result.deletedMisplacedLogReports} relocated_runtime=${result.relocatedCanonicalRuntimeArtifacts} regenerated_markdown=${result.regeneratedMarkdown} removed_queue_recovery=${result.removedQueueRecoveryReports} removed_legacy_outcomes=${result.removedLegacyOutcomeJobs} requeued=${result.requeuedMissingCanonicalReports} invariant_warnings=${result.invariantWarnings}`,
  );
}

if (
  import.meta.url === pathToFileURL(path.resolve(process.argv[1] ?? "")).href
) {
  main().catch((error) => {
    console.error(`[codex-review-repair-state] Failed: ${error.message}`);
    process.exit(1);
  });
}
