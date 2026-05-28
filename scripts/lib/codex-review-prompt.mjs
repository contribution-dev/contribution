#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { classifyChangedFiles } from "./changed-files.mjs";
import {
  getContractAgentPaths,
  getContractReferenceDocs,
  getContractReviewFocusLines,
  getContractReviewModes,
  isContractReviewMode,
} from "./contract-sensitive-domains.mjs";

export const DEFAULT_PROMPT_MAX_CHARS = 280000;
export const DEFAULT_PROMPT_MAX_FILE_CHARS = 40000;
export const DEFAULT_MAX_FINDINGS = 1;
export const HIGH_RISK_MAX_FINDINGS = 2;
export const DEFAULT_FINDING_MIN_CONFIDENCE = 0.95;
const DEFAULT_GIT_MAX_BUFFER = 8 * 1024 * 1024;
const MIN_DIFF_MAX_BUFFER = 256 * 1024;

const ROOT_AGENT_SECTION_NAMES = new Set([
  "Repo-wide rules",
  "Validation and completion",
  "Execution expectations",
]);

const LOCAL_AGENT_EXCLUDED_SECTION_NAMES = new Set(["Scope", "References"]);

function isMaxBufferError(error) {
  return (
    error?.code === "ENOBUFS" ||
    String(error?.message ?? "").includes("ENOBUFS") ||
    String(error?.message ?? "").includes("maxBuffer")
  );
}

function runGit(repoRoot, args, options = {}) {
  const maxBuffer = Math.max(
    1,
    Number.parseInt(String(options.maxBuffer ?? DEFAULT_GIT_MAX_BUFFER), 10) ||
      DEFAULT_GIT_MAX_BUFFER,
  );
  try {
    return execFileSync("git", args, {
      cwd: repoRoot,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
      maxBuffer,
    });
  } catch (error) {
    if (options.allowPartialOnMaxBuffer && isMaxBufferError(error)) {
      const partial = String(error?.stdout ?? "");
      const marker = String(options.partialMarker ?? "").trim();
      return marker ? `${partial.trimEnd()}\n${marker}\n` : partial;
    }
    throw error;
  }
}

function normalizePath(value) {
  return String(value ?? "").replaceAll("\\", "/");
}

function dedupe(values) {
  return [...new Set(values)];
}

function parsePositiveInt(value, fallback) {
  const parsed = Number.parseInt(String(value ?? ""), 10);
  if (!Number.isInteger(parsed) || parsed <= 0) {
    return fallback;
  }
  return parsed;
}

function resolveMaxFindings(
  value = process.env.CODEX_REVIEW_MAX_FINDINGS,
  context = {},
) {
  const override = parsePositiveInt(value, 0);
  if (override > 0) {
    return override;
  }
  return context?.isHighRisk ? HIGH_RISK_MAX_FINDINGS : DEFAULT_MAX_FINDINGS;
}

function parseTopLevelSections(markdown) {
  const lines = String(markdown ?? "").split(/\r?\n/);
  const sections = [];
  let current = null;

  for (const line of lines) {
    const headingMatch = /^##\s+(.+?)\s*$/.exec(line);
    if (headingMatch) {
      if (current) {
        sections.push(current);
      }
      current = {
        heading: headingMatch[1],
        lines: [],
      };
      continue;
    }

    if (current) {
      current.lines.push(line);
    }
  }

  if (current) {
    sections.push(current);
  }

  return sections.map((section) => ({
    heading: section.heading,
    body: section.lines.join("\n").trim(),
  }));
}

function renderAgentsFile(markdown, relativePath) {
  const sectionFilter =
    relativePath === "AGENTS.md"
      ? (section) => ROOT_AGENT_SECTION_NAMES.has(section.heading)
      : (section) => !LOCAL_AGENT_EXCLUDED_SECTION_NAMES.has(section.heading);
  const selectedSections =
    parseTopLevelSections(markdown).filter(sectionFilter);
  if (selectedSections.length === 0) {
    return "";
  }

  const renderedSections = selectedSections
    .map((section) => `### ${section.heading}\n${section.body}`)
    .join("\n\n");
  return `## Instructions from ${relativePath}\n${renderedSections}`;
}

function extractMarkdownSection(markdown, heading) {
  const sections = parseTopLevelSections(markdown);
  const match = sections.find((section) => section.heading === heading);
  return match?.body?.trim() ?? "";
}

function includeArchitectureSections(changedFiles) {
  const files = changedFiles.map(normalizePath);
  const lowerFiles = files.map((file) => file.toLowerCase());
  const hasCli = files.some(
    (file) => file.startsWith("cmd/") || file.startsWith("internal/cli/"),
  );
  const hasTooling = files.some(
    (file) => file.startsWith("scripts/") || file.startsWith(".github/"),
  );
  const hasRelease = lowerFiles.some((file) =>
    /(release|goreleaser|version|checksum|artifact)/.test(file),
  );

  const headings = new Set();
  if (hasCli) {
    headings.add("CLI layout");
    headings.add("Compatibility");
  }
  if (hasTooling || hasRelease) {
    headings.add("Tooling layout");
  }
  return [...headings];
}

function includeReferenceDocs(changedFiles) {
  return getContractReferenceDocs(changedFiles);
}

function summarizeArchitecture(markdown, headings) {
  const sections = headings
    .map((heading) => {
      const body = extractMarkdownSection(markdown, heading);
      if (!body) {
        return "";
      }
      return `## Architecture: ${heading}\n${body}`;
    })
    .filter(Boolean);
  return sections.join("\n\n");
}

function renderReferenceDoc(markdown, relativePath) {
  const body = String(markdown ?? "").trim();
  if (!body) {
    return "";
  }
  return `## Reference: ${relativePath}\n${body}`;
}

function resolveApplicableAgentsPaths(
  repoRoot,
  changedFiles,
  extraContextPaths = [],
) {
  const results = new Set(["AGENTS.md"]);
  for (const file of dedupe([...changedFiles, ...extraContextPaths])) {
    let current = path.dirname(path.join(repoRoot, file));
    while (current.startsWith(repoRoot)) {
      const candidate = path.join(current, "AGENTS.md");
      if (existsSync(candidate)) {
        results.add(normalizePath(path.relative(repoRoot, candidate)));
      }
      if (current === repoRoot) {
        break;
      }
      current = path.dirname(current);
    }
  }

  return [...results].sort((left, right) => {
    const leftDepth = left.split("/").length;
    const rightDepth = right.split("/").length;
    if (leftDepth !== rightDepth) {
      return leftDepth - rightDepth;
    }
    return left.localeCompare(right);
  });
}

function riskReasonsForFile(file) {
  const normalized = normalizePath(file);
  const lowerPath = normalized.toLowerCase();
  const reasons = [];
  if (normalized.startsWith("cmd/") || normalized.startsWith("internal/cli/")) {
    reasons.push("cli");
  }
  for (const mode of getContractReviewModes([normalized])) {
    if (!reasons.includes(mode)) {
      reasons.push(mode);
    }
  }
  if (normalized.startsWith("scripts/risk-policy/")) {
    reasons.push("risk-policy");
  }
  if (normalized.startsWith("scripts/codex-review")) {
    reasons.push("review-automation");
  }
  if (/(auth|token|session|secret|scope|privacy|credential)/.test(lowerPath)) {
    reasons.push("security");
  }
  if (/(queue|review|release|checksum|artifact|version)/.test(lowerPath)) {
    reasons.push("state");
  }
  return reasons;
}

export function isHighRiskFile(file) {
  return riskReasonsForFile(file).length > 0;
}

function rankFile(file) {
  const normalized = normalizePath(file);
  const reasons = riskReasonsForFile(normalized);
  let score = reasons.length * 100;
  if (normalized.startsWith("cmd/") || normalized.startsWith("internal/")) {
    score += 40;
  } else if (normalized.startsWith("scripts/")) {
    score += 30;
  } else if (normalized.endsWith(".md")) {
    score -= 20;
  }
  return score;
}

function orderedChangedFiles(changedFiles) {
  return [...changedFiles].sort((left, right) => {
    const delta = rankFile(right) - rankFile(left);
    if (delta !== 0) {
      return delta;
    }
    return left.localeCompare(right);
  });
}

function readInstructionContext(repoRoot, changedFiles) {
  const agentPaths = resolveApplicableAgentsPaths(
    repoRoot,
    changedFiles,
    getContractAgentPaths(changedFiles),
  );
  const renderedAgents = agentPaths
    .map((relativePath) => {
      const absolutePath = path.join(repoRoot, relativePath);
      return renderAgentsFile(readFileSync(absolutePath, "utf8"), relativePath);
    })
    .filter(Boolean)
    .join("\n\n");

  const architecturePath = path.join(
    repoRoot,
    "docs/reference/architecture.md",
  );
  const architectureHeadings = includeArchitectureSections(changedFiles);
  const renderedArchitecture =
    architectureHeadings.length > 0 && existsSync(architecturePath)
      ? summarizeArchitecture(
          readFileSync(architecturePath, "utf8"),
          architectureHeadings,
        )
      : "";
  const renderedReferenceDocs = includeReferenceDocs(changedFiles)
    .map((relativePath) => {
      const absolutePath = path.join(repoRoot, relativePath);
      if (!existsSync(absolutePath)) {
        return "";
      }
      return renderReferenceDoc(
        readFileSync(absolutePath, "utf8"),
        relativePath,
      );
    })
    .filter(Boolean)
    .join("\n\n");

  return {
    agentPaths,
    renderedAgents,
    renderedArchitecture,
    renderedReferenceDocs,
  };
}

function changedFilesForCommit(repoRoot, sha) {
  const output = runGit(repoRoot, [
    "show",
    "--name-only",
    "--find-renames",
    "--format=",
    sha,
  ]);
  return dedupe(
    output
      .split(/\r?\n/)
      .map((line) => line.trim())
      .filter(Boolean),
  );
}

function changedFilesSummary(repoRoot, sha) {
  return runGit(repoRoot, [
    "show",
    "--stat",
    "--summary",
    "--find-renames",
    "--format=fuller",
    sha,
  ]).trim();
}

function diffForFile(repoRoot, sha, file, fileBudget) {
  const maxBuffer = Math.max(
    MIN_DIFF_MAX_BUFFER,
    (Number.parseInt(String(fileBudget ?? ""), 10) ||
      DEFAULT_PROMPT_MAX_FILE_CHARS) * 4,
  );
  return runGit(
    repoRoot,
    ["show", "--find-renames", "--unified=3", "--format=", sha, "--", file],
    {
      maxBuffer,
      allowPartialOnMaxBuffer: true,
      partialMarker: `... [git diff output exceeded capture budget for ${file}]`,
    },
  );
}

function truncateText(value, limit, marker) {
  const text = String(value ?? "");
  if (text.length <= limit) {
    return { text, truncated: false };
  }
  const safeLimit = Math.max(0, limit - marker.length - 1);
  return {
    text: `${text.slice(0, safeLimit)}\n${marker}`,
    truncated: true,
  };
}

function buildDiffBundle(
  repoRoot,
  sha,
  orderedFiles,
  promptBudget,
  fileBudget,
  contextLength,
) {
  const truncatedFiles = [];
  const omittedFiles = [];
  const includedFiles = [];
  const parts = [];
  const reserved = Math.max(2000, Math.floor(promptBudget * 0.05));
  let remaining = Math.max(0, promptBudget - contextLength - reserved);

  for (const file of orderedFiles) {
    if (remaining <= 0) {
      omittedFiles.push(file);
      continue;
    }
    const marker = `... [truncated diff for ${file}]`;
    const { text, truncated } = truncateText(
      diffForFile(repoRoot, sha, file, fileBudget),
      fileBudget,
      marker,
    );
    const block = `### Patch: ${file}\n${text.trim()}\n`;
    if (block.length > remaining && parts.length > 0) {
      omittedFiles.push(file);
      continue;
    }
    if (block.length > remaining && parts.length === 0) {
      const forced = truncateText(
        block,
        remaining,
        `... [truncated prompt budget while including ${file}]`,
      );
      parts.push(forced.text.trim());
      includedFiles.push(file);
      truncatedFiles.push(file);
      remaining = 0;
      continue;
    }
    parts.push(block.trim());
    includedFiles.push(file);
    remaining -= block.length;
    if (truncated) {
      truncatedFiles.push(file);
    }
  }

  return {
    text: parts.join("\n\n"),
    includedFiles,
    truncatedFiles: dedupe(truncatedFiles),
    omittedFiles,
    truncated: truncatedFiles.length > 0 || omittedFiles.length > 0,
  };
}

function renderChangedFilesList(changedFiles) {
  return changedFiles
    .map((file) => {
      const reasons = riskReasonsForFile(file);
      return reasons.length > 0
        ? `- ${file} [risk: ${reasons.join(", ")}]`
        : `- ${file}`;
    })
    .join("\n");
}

function renderPromptBody({ mode, context }) {
  const maxFindings = resolveMaxFindings(undefined, context);
  const focusLines = isContractReviewMode(mode)
    ? [
        ...getContractReviewFocusLines(mode),
        "- do not infer durable safety contracts from CHANGELOG.md or commit messages",
      ]
    : mode === "focused"
      ? [
          "- CLI-visible command, flag, stdout, stderr, and exit-code regressions",
          "- release artifact, version injection, or checksum regressions",
          "- local review queue, retry, and remediation workflow regressions",
          "- token, secret, credential, and privacy regressions",
          "- partial-failure and rollback gaps",
          "- unchanged caller or consumer breakage caused by this commit",
        ]
      : [
          "- real bugs and behavioral regressions",
          "- broken assumptions in changed or unchanged callers",
          "- missing edge-case handling",
          "- incorrect error handling, cleanup, or state transitions",
        ];

  const truncationNotes = [];
  if (context.diffBundle.truncatedFiles.length > 0) {
    truncationNotes.push(
      `Some included file diffs were truncated to the per-file budget: ${context.diffBundle.truncatedFiles.join(", ")}.`,
    );
  }
  if (context.diffBundle.omittedFiles.length > 0) {
    truncationNotes.push(
      `Some lower-priority changed files were omitted from the inline diff due to prompt budget: ${context.diffBundle.omittedFiles.join(", ")}.`,
    );
  }
  if (context.promptTruncated) {
    truncationNotes.push(
      "The overall prompt hit the total prompt budget, so lower-priority prompt context was trimmed after assembly.",
    );
  }

  return [
    `Review this git commit and return only valid JSON matching the provided schema.`,
    ``,
    `This review stays commit-scoped, but you may inspect surrounding unchanged files and callers in the repository when needed to judge whether the commit is safe.`,
    `Report at most ${maxFindings} distinct root-cause findings. If more issues exist, return only the highest-signal ones first.`,
    `Only report the 80/20 issues that are both high-confidence and meaningfully risky. Skip nits, cleanup ideas, style comments, and concerns that are more likely to cause churn than prevent a real bug.`,
    `Only emit blocker or major findings that you would rate at confidence ${DEFAULT_FINDING_MIN_CONFIDENCE.toFixed(2)} or higher. Omit minor issues entirely.`,
    `Default to a single finding. Only use a second finding when this is a clearly high-risk commit and the second issue is independent, equally must-fix, and not a duplicate theme.`,
    `If multiple observations share the same broken invariant or recommended direction, merge them into one finding with multiple evidence points instead of emitting duplicates.`,
    `Do not treat similar titles by themselves as proof that two findings have the same root cause.`,
    `If one finding subsumes another or points to the same remediation direction, keep only the stronger finding.`,
    `Do not suggest style changes, broad cleanup, or speculative risks. Prefer precision over recall: try to disprove each concern from nearby code, AGENTS.md, docs/reference, and stable tests before emitting it.`,
    `Only emit a finding when you can cite the exact changed line(s), the affected invariant or unchanged caller, and a concrete failure trigger. If repository evidence is incomplete, mixed, or contradictory, omit the finding.`,
    `Before finalizing, make one second pass over the full included diff and changed-file list as if seeing it fresh; only emit a finding if that pass confirms a concrete changed-line trigger, broken invariant or caller, and meaningful runtime impact.`,
    isContractReviewMode(mode)
      ? `Treat AGENTS.md, docs/reference, and stable tests as the authoritative contract sources for this domain. Do not treat CHANGELOG.md or commit messages as authoritative contract evidence.`
      : "",
    ``,
    `Focus on:`,
    ...focusLines,
    ``,
    `For each finding, provide concrete evidence from the commit or nearby repository context, including the changed line(s) and the specific invariant, caller, or consumer that breaks. Always emit string values for failure_scenario, broken_invariant, and review_pass. Use an empty string when a narrative field is not needed, and use the active review mode as review_pass unless you have a more specific pass label.`,
    ``,
    `Repository: ${context.repoName}`,
    `Repository root: ${context.repoRoot}`,
    `Commit: ${context.sha}`,
    `Trigger: ${context.trigger}`,
    `Review mode: ${mode}`,
    `High risk commit: ${context.isHighRisk ? "yes" : "no"}`,
    `Prompt truncated: ${context.promptTruncated || context.diffBundle.truncated ? "yes" : "no"}`,
    ``,
    `## Changed files`,
    renderChangedFilesList(context.orderedFiles),
    ``,
    `## Commit summary`,
    context.commitSummary,
    ``,
    context.instructions.renderedAgents,
    context.instructions.renderedArchitecture
      ? `\n${context.instructions.renderedArchitecture}\n`
      : "",
    context.instructions.renderedReferenceDocs
      ? `\n${context.instructions.renderedReferenceDocs}\n`
      : "",
    truncationNotes.length > 0
      ? `## Diff budget notes\n${truncationNotes.map((note) => `- ${note}`).join("\n")}\n`
      : "",
    `## Inline diff`,
    context.diffBundle.text ||
      "No inline diff was captured. Inspect the commit directly if needed.",
  ]
    .filter(Boolean)
    .join("\n");
}

export function collectReviewContext({
  repoRoot,
  sha,
  trigger,
  promptMaxChars = DEFAULT_PROMPT_MAX_CHARS,
  filePromptMaxChars = DEFAULT_PROMPT_MAX_FILE_CHARS,
}) {
  const changedFiles = changedFilesForCommit(repoRoot, sha);
  const orderedFiles = orderedChangedFiles(changedFiles);
  const instructions = readInstructionContext(repoRoot, changedFiles);
  const commitSummary = changedFilesSummary(repoRoot, sha);
  const repoName = path.basename(repoRoot);
  const changedFileClassification = classifyChangedFiles(changedFiles);
  const contractReviewModes = getContractReviewModes(orderedFiles);
  const isHighRisk =
    orderedFiles.some((file) => isHighRiskFile(file)) ||
    Boolean(
      changedFileClassification.rootConfig && changedFileClassification.tsLike,
    );

  const contextSkeleton = [
    `Repository: ${repoName}`,
    `Repository root: ${repoRoot}`,
    `Commit: ${sha}`,
    `Trigger: ${trigger}`,
    instructions.renderedAgents,
    instructions.renderedArchitecture,
    instructions.renderedReferenceDocs,
    commitSummary,
    renderChangedFilesList(orderedFiles),
  ]
    .filter(Boolean)
    .join("\n").length;

  const diffBundle = buildDiffBundle(
    repoRoot,
    sha,
    orderedFiles,
    promptMaxChars,
    filePromptMaxChars,
    contextSkeleton,
  );

  return {
    repoRoot,
    repoName,
    sha,
    trigger,
    changedFiles,
    orderedFiles,
    instructions,
    commitSummary,
    diffBundle,
    promptMaxChars,
    filePromptMaxChars,
    changedFileClassification,
    contractReviewModes,
    isHighRisk,
    promptTruncated: false,
  };
}

export function buildReviewPrompt({
  repoRoot,
  sha,
  trigger,
  promptMaxChars = parsePositiveInt(
    process.env.CODEX_REVIEW_PROMPT_MAX_CHARS,
    DEFAULT_PROMPT_MAX_CHARS,
  ),
  filePromptMaxChars = parsePositiveInt(
    process.env.CODEX_REVIEW_PROMPT_MAX_FILE_CHARS,
    DEFAULT_PROMPT_MAX_FILE_CHARS,
  ),
  mode = "general",
  context = null,
}) {
  const baseContext =
    context ??
    collectReviewContext({
      repoRoot,
      sha,
      trigger,
      promptMaxChars,
      filePromptMaxChars,
    });

  let resolvedContext = baseContext;
  let prompt = renderPromptBody({ mode, context: resolvedContext });

  if (prompt.length > promptMaxChars) {
    resolvedContext = {
      ...baseContext,
      promptTruncated: true,
      diffBundle: {
        ...baseContext.diffBundle,
        truncated: true,
      },
    };
    prompt = renderPromptBody({ mode, context: resolvedContext });
    prompt = truncateText(
      prompt,
      promptMaxChars,
      "... [prompt truncated due to total budget]",
    ).text;
  }

  return {
    context: resolvedContext,
    prompt,
  };
}
