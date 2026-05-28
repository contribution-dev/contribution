import { execFileSync } from "node:child_process";

export const CHANGED_FILES_DIFF_FILTER = "ACMRD";

function runGit(args, options = {}) {
  try {
    return execFileSync("git", args, {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
      ...options,
    }).trim();
  } catch {
    return "";
  }
}

function collectWorktreeFiles(cwd) {
  const output = runGit(["status", "--porcelain"], { cwd });
  return output
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const renamed = line.match(/^R.\s+(.+)\s+->\s+(.+)$/);
      if (renamed) {
        return renamed[2];
      }
      return line.slice(3);
    })
    .map((file) => file.replaceAll("\\", "/"))
    .filter(Boolean);
}

function unique(values) {
  return [...new Set(values)];
}

function hasRef(cwd, ref) {
  return runGit(["rev-parse", "--verify", "--quiet", ref], { cwd }).length > 0;
}

function getHeadRef(cwd, explicitHead) {
  return explicitHead?.trim() || "HEAD";
}

export function resolveBaseRef(cwd, explicitBase, headRef) {
  if (explicitBase?.trim()) {
    return { baseRef: explicitBase.trim(), collapsedFromNoHistory: false };
  }

  const upstream = runGit(
    ["rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"],
    { cwd },
  );
  if (upstream) {
    const base = runGit(["merge-base", upstream, headRef], { cwd });
    if (base) {
      return { baseRef: base, collapsedFromNoHistory: false };
    }
  }

  if (hasRef(cwd, "refs/remotes/origin/main")) {
    const base = runGit(["merge-base", "origin/main", headRef], { cwd });
    if (base) {
      return { baseRef: base, collapsedFromNoHistory: false };
    }
  }

  if (hasRef(cwd, "HEAD~1")) {
    return { baseRef: "HEAD~1", collapsedFromNoHistory: false };
  }

  return { baseRef: headRef, collapsedFromNoHistory: true };
}

export function resolveChangedFilesSource({
  baseRef,
  headRef,
  hasExplicitBase,
  collapsedFromNoHistory,
}) {
  if (baseRef !== headRef) {
    return "diff";
  }
  if (hasExplicitBase) {
    return "empty";
  }
  if (collapsedFromNoHistory) {
    return "show";
  }
  return "empty";
}

export function getChangedFiles({ cwd, explicitBase, explicitHead } = {}) {
  const workdir = cwd ?? process.cwd();
  const hasExplicitBase = Boolean(explicitBase?.trim());
  const headRef = getHeadRef(workdir, explicitHead);
  const { baseRef, collapsedFromNoHistory } = resolveBaseRef(
    workdir,
    explicitBase,
    headRef,
  );
  const source = resolveChangedFilesSource({
    baseRef,
    headRef,
    hasExplicitBase,
    collapsedFromNoHistory,
  });
  const diffRange = baseRef === headRef ? headRef : `${baseRef}...${headRef}`;
  const output =
    source === "show"
      ? runGit(
          [
            "show",
            "--name-only",
            `--diff-filter=${CHANGED_FILES_DIFF_FILTER}`,
            "--pretty=format:",
            headRef,
          ],
          { cwd: workdir },
        )
      : source === "empty"
        ? ""
        : runGit(
            [
              "diff",
              "--name-only",
              `--diff-filter=${CHANGED_FILES_DIFF_FILTER}`,
              diffRange,
            ],
            { cwd: workdir },
          );

  const files = output
    .split("\n")
    .map((value) => value.trim())
    .filter(Boolean);
  const worktreeFiles = hasExplicitBase ? [] : collectWorktreeFiles(workdir);

  return {
    baseRef,
    headRef,
    diffRange,
    source,
    collapsedFromNoHistory,
    files: unique([...files, ...worktreeFiles]).sort(),
  };
}

function hasPrefix(files, prefix) {
  return files.some((file) => file.startsWith(prefix));
}

function hasAnyPrefix(file, prefixes) {
  return prefixes.some((prefix) => file.startsWith(prefix));
}

function isRootConfigPath(file) {
  return (
    [
      "go.mod",
      "go.sum",
      "package.json",
      "pnpm-lock.yaml",
      "pnpm-workspace.yaml",
      "Makefile",
      ".golangci.yml",
      ".goreleaser.yml",
      ".go-version",
      ".editorconfig",
      ".prettierrc.json",
      ".prettierignore",
      ".gitignore",
      "lint-staged.config.js",
    ].includes(file) || file.startsWith(".github/workflows/")
  );
}

function isDocPath(file) {
  return file.startsWith("docs/") || /\.(md|mdx)$/i.test(file);
}

export function classifyChangedFiles(files) {
  const list = Array.isArray(files) ? files : [];
  const goRelevant = list.some(
    (file) =>
      hasAnyPrefix(file, ["cmd/", "internal/"]) ||
      file === "go.mod" ||
      file === "go.sum" ||
      file === ".golangci.yml" ||
      file === ".goreleaser.yml",
  );
  const tooling = list.some(
    (file) =>
      hasAnyPrefix(file, ["scripts/", ".husky/", ".github/"]) ||
      file === "package.json" ||
      file === "pnpm-lock.yaml" ||
      file === "lint-staged.config.js",
  );
  const docsOnly = list.length > 0 && list.every((file) => isDocPath(file));
  const rootConfig = list.some(isRootConfigPath);

  return {
    app: false,
    mobile: false,
    ui: false,
    tooling,
    tsLike: list.some((file) => /\.(js|mjs|cjs|ts|tsx)$/i.test(file)),
    docsOnly,
    rootConfig,
    appSource: false,
    mobileSource: false,
    uiSource: false,
    appRelevant: goRelevant,
    mobileRelevant: false,
    uiRelevant: false,
    goRelevant,
    docs: hasPrefix(list, "docs/"),
  };
}
