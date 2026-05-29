import { spawnSync } from "node:child_process";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { mkdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import {
  applyOperatorClosuresToReport,
  loadReviewedShas,
  readOperatorStateSync,
} from "../codex-review-push-gate-lib.mjs";
import {
  classifyBacklogArtifact,
  isActionableCodexReviewReport,
  reportSatisfiesCanonicalCodexLane,
  resolveRepoRoot,
} from "./codex-review-state.mjs";
import { readTerminalManualTakeoverSha } from "./codex-review-manual-takeover.mjs";

export {
  parseBacklogMeta,
  summarizeFindingPriorityFromMeta,
} from "./codex-review-state.mjs";

export const LIVE_REMEDIATION_STATUSES = new Set([
  "claimed",
  "in_progress",
  "retryable_error",
  "paused",
  "deferred",
]);

export function readLiveClaimState(rootDir, sha) {
  const statePath = liveClaimStatePath(rootDir, sha);
  if (!existsSync(statePath)) {
    return null;
  }
  try {
    const parsed = JSON.parse(readFileSync(statePath, "utf8"));
    const status = String(parsed?.status ?? "").trim();
    return LIVE_REMEDIATION_STATUSES.has(status) ? parsed : null;
  } catch {
    return null;
  }
}

export function liveClaimStatePath(rootDir, sha) {
  return path.join(rootDir, "backlog-remediation", "state", `${sha}.json`);
}

export function resolveArtifactOwnership(
  rootDir,
  sha,
  { manualTakeoverSha = "" } = {},
) {
  const liveClaimState = readLiveClaimState(rootDir, sha);
  if (liveClaimState) {
    return {
      ownership: "remediation",
      ownershipStatus: String(liveClaimState.status ?? "").trim(),
      liveClaimState,
    };
  }
  const normalizedSha = String(sha ?? "")
    .trim()
    .toLowerCase();
  const normalizedManualTakeoverSha = String(manualTakeoverSha ?? "")
    .trim()
    .toLowerCase();
  if (normalizedSha && normalizedSha === normalizedManualTakeoverSha) {
    return {
      ownership: "manual_takeover",
      ownershipStatus: "manual_takeover",
      liveClaimState: null,
    };
  }
  return {
    ownership: "unowned",
    ownershipStatus: "",
    liveClaimState: null,
  };
}

export function artifactRequiresAdjudication(artifact) {
  if (String(artifact?.category ?? "") === "contradictory") {
    return true;
  }
  return (
    String(artifact?.category ?? "") === "actionable" &&
    Number(artifact?.findingsCount ?? 0) === 0
  );
}

function listMarkdownFiles(rootDir) {
  const directFiles = readdirSync(rootDir, { withFileTypes: true })
    .filter((entry) => entry.isFile())
    .map((entry) => path.join(rootDir, entry.name));
  const claimsDir = path.join(rootDir, "backlog-remediation", "claims");
  const claimedFiles = existsSync(claimsDir)
    ? readdirSync(claimsDir, { withFileTypes: true })
        .filter((entry) => entry.isFile())
        .map((entry) => path.join(claimsDir, entry.name))
        .filter((filePath) => {
          const sha = path.basename(filePath, path.extname(filePath));
          return readLiveClaimState(rootDir, sha) !== null;
        })
    : [];
  return [...directFiles, ...claimedFiles]
    .filter((filePath) => path.basename(filePath).endsWith(".md"))
    .filter((filePath) => !path.basename(filePath).startsWith("."));
}

function safeGitOutput(repoRoot, args) {
  const result = spawnSync("git", args, {
    cwd: repoRoot,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "ignore"],
  });
  if (result.status !== 0) {
    return "";
  }
  return String(result.stdout ?? "").trim();
}

function gitExitStatus(repoRoot, args) {
  const result = spawnSync("git", args, {
    cwd: repoRoot,
    encoding: "utf8",
    stdio: ["ignore", "ignore", "ignore"],
  });
  return Number(result.status ?? 1);
}

function normalizeEvidenceFileForRepo(repoRoot, filePath) {
  const raw = String(filePath ?? "").trim();
  if (!raw) return "";
  const resolved = path.isAbsolute(raw) ? raw : path.join(repoRoot, raw);
  const relative = path.relative(repoRoot, resolved);
  if (!relative || relative.startsWith("..") || path.isAbsolute(relative)) {
    return "";
  }
  return relative.split(path.sep).join("/");
}

function evidenceFilesForReport(repoRoot, report) {
  const findings = Array.isArray(report?.findings) ? report.findings : [];
  const files = new Set();
  for (const finding of findings) {
    const findingFile = normalizeEvidenceFileForRepo(repoRoot, finding?.file);
    if (findingFile) files.add(findingFile);
    const evidence = Array.isArray(finding?.evidence) ? finding.evidence : [];
    for (const item of evidence) {
      const evidenceFile = normalizeEvidenceFileForRepo(repoRoot, item?.file);
      if (evidenceFile) files.add(evidenceFile);
    }
  }
  return [...files];
}

async function readOptionalFile(filePath) {
  try {
    return await readFile(filePath);
  } catch (error) {
    if (error?.code === "ENOENT") {
      return null;
    }
    throw error;
  }
}

async function writeArchivedArtifact(destinationPath, content) {
  await mkdir(path.dirname(destinationPath), { recursive: true, mode: 0o700 });
  await rm(destinationPath, { force: true }).catch(() => {});
  await writeFile(destinationPath, content, { mode: 0o600 });
}

export function archivedBacklogArtifactPath(reviewsDir, sha, extension) {
  return path.join(
    reviewsDir,
    "backlog-remediation",
    "results",
    `${sha}.archived.review.${String(extension ?? "").replace(/^\./, "")}`,
  );
}

function backlogMarkdownCandidates(reviewsDir, sha, liveClaimState = null) {
  const candidates = [
    path.join(reviewsDir, `${sha}.md`),
    String(liveClaimState?.source_path ?? "").trim(),
    String(liveClaimState?.claim_path ?? "").trim(),
  ].filter(Boolean);
  return [...new Set(candidates)];
}

export async function archiveBacklogArtifactsForSha(
  reviewsDir,
  sha,
  { liveClaimState = null } = {},
) {
  const normalizedSha = String(sha ?? "")
    .trim()
    .toLowerCase();
  if (!normalizedSha) {
    return false;
  }

  let changed = false;
  for (const extension of ["json", "md"]) {
    const destinationPath = archivedBacklogArtifactPath(
      reviewsDir,
      normalizedSha,
      extension,
    );
    const sourcePaths =
      extension === "md"
        ? backlogMarkdownCandidates(reviewsDir, normalizedSha, liveClaimState)
        : [path.join(reviewsDir, `${normalizedSha}.json`)];
    let sourceContent = null;
    for (const sourcePath of sourcePaths) {
      sourceContent = await readOptionalFile(sourcePath);
      if (sourceContent !== null) {
        break;
      }
    }
    if (sourceContent === null) continue;
    const destinationContent = await readOptionalFile(destinationPath);
    if (
      destinationContent === null ||
      !sourceContent.equals(destinationContent)
    ) {
      await writeArchivedArtifact(destinationPath, sourceContent);
    }
    for (const sourcePath of sourcePaths) {
      await rm(sourcePath, { force: true }).catch(() => {});
    }
    changed = true;
  }

  return changed;
}

export async function buildStaleSupersessionArchiver(
  reviewsDir,
  { headSha = "" } = {},
) {
  const normalizedHeadSha =
    String(headSha ?? "")
      .trim()
      .toLowerCase() ||
    safeGitOutput(resolveRepoRoot(reviewsDir), [
      "rev-parse",
      "HEAD",
    ]).toLowerCase();
  if (!/^[0-9a-f]{40}$/.test(normalizedHeadSha)) {
    return () => false;
  }

  const repoRoot = resolveRepoRoot(reviewsDir);
  const headReportPath = path.join(reviewsDir, `${normalizedHeadSha}.json`);
  let headReport = null;
  if (existsSync(headReportPath)) {
    try {
      headReport = JSON.parse(readFileSync(headReportPath, "utf8"));
    } catch {
      headReport = null;
    }
  }
  const reviewed = await loadReviewedShas({ repoRoot }).catch(() => null);
  const headReviewed = Boolean(
    (headReport &&
      reportSatisfiesCanonicalCodexLane(headReport) &&
      String(headReport?.last_reviewed ?? "").trim()) ||
    reviewed?.lanes?.codex?.clean?.has(normalizedHeadSha),
  );
  if (!headReviewed) {
    return () => false;
  }

  const ancestorCache = new Map();
  const changedCache = new Map();

  return async (artifact) => {
    const candidateSha = String(artifact?.sha ?? "")
      .trim()
      .toLowerCase();
    if (!candidateSha || candidateSha === normalizedHeadSha) {
      return false;
    }

    const liveClaimState =
      artifact?.liveClaimState ?? readLiveClaimState(reviewsDir, candidateSha);
    if (
      String(artifact?.ownership ?? "") === "remediation" ||
      (liveClaimState &&
        LIVE_REMEDIATION_STATUSES.has(
          String(liveClaimState.status ?? "").trim(),
        ))
    ) {
      return false;
    }

    const report = artifact?.report ?? artifact?.parsed ?? null;
    if (!report || !isActionableCodexReviewReport(report, true)) {
      return false;
    }

    if (!ancestorCache.has(candidateSha)) {
      ancestorCache.set(
        candidateSha,
        gitExitStatus(repoRoot, [
          "merge-base",
          "--is-ancestor",
          candidateSha,
          normalizedHeadSha,
        ]) === 0,
      );
    }
    if (ancestorCache.get(candidateSha) !== true) {
      return false;
    }

    const files = evidenceFilesForReport(repoRoot, report);
    if (files.length === 0) {
      return false;
    }

    for (const filePath of files) {
      const cacheKey = `${candidateSha}:${filePath}`;
      if (!changedCache.has(cacheKey)) {
        changedCache.set(
          cacheKey,
          gitExitStatus(repoRoot, [
            "diff",
            "--quiet",
            `${candidateSha}..${normalizedHeadSha}`,
            "--",
            filePath,
          ]) === 1,
        );
      }
      if (changedCache.get(cacheKey) !== true) {
        return false;
      }
    }

    const archived = await archiveBacklogArtifactsForSha(
      reviewsDir,
      candidateSha,
    );
    return archived
      ? {
          suppress: true,
          archivedArtifacts: true,
          reason: "superseded-by-current-tip",
        }
      : false;
  };
}

export function listBacklogArtifacts(reviewsDir, { headSha = "" } = {}) {
  const normalizedHeadSha = String(headSha ?? "")
    .trim()
    .toLowerCase();
  const manualTakeoverSha = readTerminalManualTakeoverSha(reviewsDir);
  const operatorState = readOperatorStateSync({ reviewsDir });
  const filePathsBySha = new Map();
  for (const filePath of listMarkdownFiles(reviewsDir)) {
    const sha = path.basename(filePath, path.extname(filePath));
    if (!filePathsBySha.has(sha)) {
      filePathsBySha.set(sha, filePath);
    }
  }

  return Array.from(filePathsBySha.entries())
    .map(([sha, filePath]) => {
      const markdown = readFileSync(filePath, "utf8").replace(/\s+$/, "");
      const jsonPath = path.join(path.dirname(filePath), `${sha}.json`);
      let reviewStatus = "";
      let report = null;
      if (existsSync(jsonPath)) {
        try {
          report = JSON.parse(readFileSync(jsonPath, "utf8"));
          report = applyOperatorClosuresToReport({
            operatorState,
            sha,
            report,
          });
          reviewStatus = String(report?.review_status ?? "").trim();
        } catch {
          reviewStatus = "";
          report = null;
        }
      }
      const classified = classifyBacklogArtifact({
        filePath,
        markdown,
        reviewStatus,
        report,
      });
      const ownership = resolveArtifactOwnership(reviewsDir, sha, {
        manualTakeoverSha,
      });
      const scope =
        normalizedHeadSha && sha.toLowerCase() === normalizedHeadSha
          ? "head"
          : "historical";
      return {
        ...classified,
        ...ownership,
        requiresAdjudication: artifactRequiresAdjudication(classified),
        sha,
        jsonPath,
        mdPath: filePath,
        markdown,
        report,
        reviewStatus,
        scope,
      };
    })
    .sort((left, right) => {
      if (right.highestSeverity !== left.highestSeverity) {
        return right.highestSeverity - left.highestSeverity;
      }
      if (right.highestConfidence !== left.highestConfidence) {
        return right.highestConfidence - left.highestConfidence;
      }
      if (right.findingsCount !== left.findingsCount) {
        return right.findingsCount - left.findingsCount;
      }
      return left.sha.localeCompare(right.sha);
    });
}

export async function collectLiveBacklogArtifacts(
  reviewsDir,
  { headSha = "" } = {},
) {
  const archiver = await buildStaleSupersessionArchiver(reviewsDir, {
    headSha,
  });
  const artifacts = listBacklogArtifacts(reviewsDir, { headSha });
  for (const artifact of artifacts) {
    await archiver(artifact);
  }
  return listBacklogArtifacts(reviewsDir, { headSha });
}
