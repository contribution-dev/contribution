#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  rmSync,
  statSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(SCRIPT_DIR, "..");
const TMP_PARENT = "/tmp";
const DEFAULT_BINARY = path.join(ROOT, "bin", "contribution");
const SECRET_VALUE = "dogfood-secret-value";
const SECRET_SENTINEL = `token=${SECRET_VALUE}`;
const MODES = new Set(["smoke", "release", "real"]);

function parseArgs(argv) {
  const args = {
    mode: "smoke",
    binary: "",
    keepTemp: false,
    skipBuild: false,
  };

  for (let i = 0; i < argv.length; i += 1) {
    const token = argv[i];
    if (token === "--mode") {
      args.mode = argv[i + 1] ?? "";
      i += 1;
      continue;
    }
    if (token === "--binary") {
      args.binary = argv[i + 1] ?? "";
      i += 1;
      continue;
    }
    if (token === "--keep-temp") {
      args.keepTemp = true;
      continue;
    }
    if (token === "--skip-build") {
      args.skipBuild = true;
      continue;
    }
    if (token === "-h" || token === "--help") {
      printHelp();
      process.exit(0);
    }
    throw new Error(`Unknown argument: ${token}`);
  }

  if (!MODES.has(args.mode)) {
    throw new Error(`Invalid --mode value: ${args.mode}`);
  }
  if (args.skipBuild && !args.binary) {
    throw new Error("--skip-build requires --binary");
  }

  return args;
}

function printHelp() {
  console.log(`Usage: node scripts/dogfood-cli.mjs [options]

Options:
  --mode smoke|release|real
                        Dogfood mode to run (default: smoke)
  --binary <path>       Binary path to build or test
  --skip-build          Use --binary without rebuilding it
  --keep-temp           Keep temporary workspaces for debugging
  -h, --help            Show help
`);
}

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

function run(command, args, options = {}) {
  const cwd = options.cwd ?? ROOT;
  const expectedStatus = options.expectedStatus ?? 0;
  const printable = [command, ...args].join(" ");
  console.log(`[dogfood-cli] START ${printable}`);
  const result = spawnSync(command, args, {
    cwd,
    env: options.env ?? process.env,
    encoding: "utf8",
  });
  const status = result.status ?? 1;
  if (result.error) {
    throw new Error(`${printable} failed: ${result.error.message}`);
  }
  if (expectedStatus === "nonzero") {
    if (status === 0) {
      throw new Error(`${printable} unexpectedly exited 0`);
    }
  } else if (status !== expectedStatus) {
    throw new Error(
      `${printable} failed with exit ${status}\nstdout:\n${result.stdout}\nstderr:\n${result.stderr}`,
    );
  }
  console.log(`[dogfood-cli] PASS ${printable}`);
  return result;
}

function buildBinary(binary) {
  mkdirSync(path.dirname(binary), { recursive: true });
  run("go", ["build", "-trimpath", "-o", binary, "./cmd/contribution"]);
}

function readJSON(file) {
  return JSON.parse(readFileSync(file, "utf8"));
}

function writeJSON(file, value) {
  mkdirSync(path.dirname(file), { recursive: true });
  writeFileSync(file, `${JSON.stringify(value, null, 2)}\n`);
}

function writeRepoFile(repo, relativePath, content) {
  const target = path.join(repo, relativePath);
  mkdirSync(path.dirname(target), { recursive: true });
  writeFileSync(target, content);
}

function git(repo, args) {
  return run("git", args, { cwd: repo });
}

function gitVisibleExistingFiles(repo) {
  const result = git(repo, [
    "ls-files",
    "--cached",
    "--others",
    "--exclude-standard",
    "-z",
  ]);
  return result.stdout
    .split("\0")
    .filter(Boolean)
    .filter((relativePath) => existsSync(path.join(repo, relativePath)))
    .sort();
}

function currentHeadSHA(repo) {
  return git(repo, ["rev-parse", "HEAD"]).stdout.trim();
}

function writeIgnoredArtifacts(repo) {
  writeRepoFile(repo, ".pnpm-store/store.json", "{}\n");
  writeRepoFile(repo, ".code-reviews/report.json", "{}\n");
  writeRepoFile(repo, ".tools/tool.json", "{}\n");
  writeRepoFile(repo, "bin/contribution", "binary\n");
  writeRepoFile(repo, "node_modules/pkg/index.js", "module.exports = {}\n");
  writeRepoFile(repo, "docs-shared/vision.md", "private\n");
}

function createGitRepo(tempRoot, name) {
  const repo = path.join(tempRoot, name);
  mkdirSync(repo, { recursive: true });
  git(repo, ["init", "-b", "main"]);
  git(repo, ["config", "user.email", "dogfood@example.test"]);
  git(repo, ["config", "user.name", "Dogfood User"]);
  writeRepoFile(repo, "README.md", "# Dogfood fixture\n");
  git(repo, ["add", "."]);
  git(repo, ["commit", "-m", "initial fixture"]);
  const sha = git(repo, ["rev-parse", "HEAD"]).stdout.trim();
  return { repo, sha };
}

function commitAll(repo, message) {
  git(repo, ["add", "."]);
  git(repo, ["commit", "-m", message]);
  return git(repo, ["rev-parse", "HEAD"]).stdout.trim();
}

function commandPath(name) {
  const result = spawnSync("which", [name], {
    cwd: ROOT,
    env: process.env,
    encoding: "utf8",
  });
  if (result.status !== 0) {
    throw new Error(`Unable to find ${name} on PATH`);
  }
  return result.stdout.trim().split(/\r?\n/u)[0];
}

function makeMinimalPath(tempRoot, binary) {
  const toolDir = path.join(tempRoot, "minimal-path");
  mkdirSync(toolDir, { recursive: true });
  for (const [name, target] of [
    ["git", commandPath("git")],
    ["contribution", binary],
  ]) {
    const link = path.join(toolDir, name);
    try {
      if (!existsSync(link)) {
        symlinkSync(target, link);
      }
    } catch {
      return `${path.dirname(commandPath("git"))}${path.delimiter}${path.dirname(
        binary,
      )}`;
    }
  }
  return toolDir;
}

function dogfoodEnv({ home, pathValue, includeTokenSentinel }) {
  const env = {
    ...process.env,
    HOME: home,
    USERPROFILE: home,
    PATH: pathValue ?? process.env.PATH,
  };
  if (includeTokenSentinel) {
    env.GITHUB_TOKEN = SECRET_SENTINEL;
    env.GH_TOKEN = "";
  } else {
    delete env.GITHUB_TOKEN;
    delete env.GH_TOKEN;
  }
  return env;
}

function runCli(binary, args, options = {}) {
  return run(options.byName ? "contribution" : binary, args, {
    cwd: options.cwd,
    env: options.env,
    expectedStatus: options.expectedStatus,
  });
}

function assertReferencedPathsExist(result, tempRoot) {
  const text = `${result.stdout}\n${result.stderr}`;
  const escapedRoot = tempRoot.replaceAll("/", "\\/");
  const pathPattern = new RegExp(`${escapedRoot}[^\\s)'",]+`, "gu");
  for (const raw of text.match(pathPattern) ?? []) {
    const candidate = raw.replace(/[.,:;]+$/u, "");
    assert(
      existsSync(candidate),
      `stdout/stderr referenced missing path: ${candidate}`,
    );
  }
}

function listDirs(root) {
  return readdirSync(root)
    .map((entry) => path.join(root, entry))
    .filter((entry) => statSync(entry).isDirectory())
    .sort();
}

function latestRunDir(root) {
  const dirs = listDirs(root);
  assert(dirs.length > 0, `expected at least one output directory in ${root}`);
  return dirs[dirs.length - 1];
}

function assertFilesExist(root, files) {
  for (const file of files) {
    assert(existsSync(path.join(root, file)), `missing expected file ${file}`);
  }
}

function assertFilesAbsent(root, files) {
  for (const file of files) {
    assert(
      !existsSync(path.join(root, file)),
      `unexpected file exists: ${file}`,
    );
  }
}

function collectFiles(root) {
  const out = [];
  for (const entry of readdirSync(root)) {
    const full = path.join(root, entry);
    const stat = statSync(full);
    if (stat.isDirectory()) {
      out.push(...collectFiles(full));
    } else {
      out.push(full);
    }
  }
  return out;
}

function assertNoTextInFiles(files, patterns) {
  for (const file of files) {
    const text = readFileSync(file, "utf8");
    for (const pattern of patterns) {
      if (typeof pattern === "string") {
        assert(!text.includes(pattern), `${file} contains forbidden text`);
      } else {
        assert(
          !pattern.test(text),
          `${file} matches forbidden pattern ${pattern}`,
        );
      }
    }
  }
}

function assertPublicSafeFiles(files, privateRoot) {
  assertNoTextInFiles(files, [
    SECRET_SENTINEL,
    SECRET_VALUE,
    privateRoot,
    /authorization:/iu,
    /bearer\s+/iu,
    /api_?key=/iu,
    /password=/iu,
    /secret=/iu,
    /[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}/iu,
  ]);
}

function assertPublicSafeReportQuality(file) {
  const text = readFileSync(file, "utf8");
  assert(!text.includes("V1"), `${file} retained phase-stale V1 wording`);
  let checkedRows = 0;
  for (const line of text.split(/\r?\n/u)) {
    if (
      !line.startsWith("| ") ||
      line.startsWith("| PR ") ||
      line.startsWith("| ---")
    ) {
      continue;
    }
    const cells = line
      .split("|")
      .slice(1, -1)
      .map((cell) => cell.trim());
    if (cells.length < 10) {
      continue;
    }
    checkedRows += 1;
    assert(cells[7], `${file} has empty ledger main risk cell: ${line}`);
    assert(cells[9], `${file} has empty ledger next action cell: ${line}`);
  }
  assert(checkedRows > 0, `${file} has no PR quality ledger rows`);
}

function assertResultNoSecrets(result) {
  const text = `${result.stdout}\n${result.stderr}`;
  assert(
    !text.includes(SECRET_SENTINEL),
    "command output leaked token sentinel",
  );
  assert(!text.includes(SECRET_VALUE), "command output leaked token value");
}

function assertAnalysisPublicSafe(file, privateRoot) {
  const analysis = readJSON(file);
  assert(
    analysis.privacy?.public_safe === true,
    "analysis.json public_safe mismatch",
  );
  assert(
    analysis.privacy?.raw_code_included === false,
    "analysis includes raw code",
  );
  assert(
    analysis.privacy?.raw_diffs_included === false,
    "analysis includes raw diffs",
  );
  assert(
    analysis.config?.public_safe === true,
    "analysis config public_safe mismatch",
  );
  assert(
    analysis.config?.output_directory === "",
    "public-safe analysis retained output directory",
  );
  assert(!analysis.repo?.root, "public-safe analysis retained repo root");
  assert(
    !analysis.repo?.remote_url,
    "public-safe analysis retained repo remote",
  );
  assert(!analysis.repo?.head_sha, "public-safe analysis retained head SHA");
  assertPublicSafeFiles([file], privateRoot);
  return analysis;
}

function findPacketOutput(root) {
  const matches = collectFiles(root).filter(
    (file) => path.basename(file) === "friend-review-packet.json",
  );
  assert(
    matches.length === 1,
    `expected one friend-review-packet.json, found ${matches.length}`,
  );
  return matches[0];
}

function analysisFixture(privateRoot) {
  return {
    version: 1,
    generated_at: "2026-05-28T00:00:00Z",
    repo: {
      id: "local:dogfood",
      name: "Private Dogfood Repo",
      root: privateRoot,
      remote_url: "https://example.test/private/repo.git",
      default_branch: "main",
      head_sha: "abc123",
      is_remote_clone: false,
    },
    config: {
      since_days: 90,
      max_prs: 20,
      public_safe: false,
      no_external_tools: true,
      output_directory: privateRoot,
      github_metadata_configured: false,
    },
    tooling: {
      generated_at: "2026-05-28T00:00:00Z",
      tools: [],
    },
    inventory: {
      total_files: 1,
      by_class: {},
      by_language: {},
      test_files: 0,
      source_files: 1,
      docs_files: 0,
      dependency_files: 0,
      config_files: 0,
      generated_files: 0,
      vendor_files: 0,
      risky_files: 0,
    },
    signals: [],
    pr_quality_cards: [
      {
        pr_number: 123,
        title: "Private PR",
        url: "https://example.test/private/pull/123",
        quality_label: "solid",
        confidence: "medium",
        summary: "Small focused change with test evidence.",
        scope: "1 file",
        test_evidence: "Tests touched.",
        review_burden: "Low.",
        durability: "Stable.",
        main_risk: "No private details in packet.",
        strengths: [],
        risks: [
          {
            label: "Private risk",
            evidence: "Private detail",
            confidence: "medium",
          },
        ],
        evidence: [],
        next_action: "Keep changes focused.",
      },
    ],
    weakness_map: {
      strengths: [],
      weaknesses: [],
      watch_items: [],
      next_actions: [],
      confidence: "medium",
    },
    profile: {
      headline: "AI-native contribution profile",
      analyzed_prs: 1,
      analysis_window_days: 90,
      confidence: "medium",
      strengths: [],
      improvement_trends: [],
      badge_candidates: [],
    },
    limitations: [],
    privacy: {
      public_safe: false,
      raw_code_included: false,
      raw_diffs_included: false,
      private_paths_included_in_public_export: false,
      author_emails_included: false,
    },
  };
}

function runSmoke(binary, tempRoot, options = {}) {
  const home = path.join(tempRoot, "home");
  mkdirSync(home, { recursive: true });
  const minimalPath = makeMinimalPath(tempRoot, binary);
  const env = dogfoodEnv({
    home,
    pathValue: options.pathValue ?? minimalPath,
    includeTokenSentinel: true,
  });

  const version = runCli(binary, ["version"], { env, byName: options.byName });
  assert(
    version.stdout.includes("contribution "),
    "version output missing binary name",
  );
  assert(
    version.stdout.includes("commit:"),
    "version output missing commit field",
  );
  assert(version.stdout.includes("date:"), "version output missing date field");

  const help = runCli(binary, [], { env, byName: options.byName });
  assert(
    help.stdout.includes(
      "Analyze contribution quality from local repo evidence.",
    ),
    "root help output missing summary",
  );

  const unknownRepo = createGitRepo(tempRoot, "unknown-command-repo").repo;
  const unknown = runCli(binary, ["not-a-command"], {
    cwd: unknownRepo,
    env,
    expectedStatus: "nonzero",
    byName: options.byName,
  });
  assert(
    unknown.stderr.trim().length > 0,
    "unknown command did not write stderr",
  );
  assert(
    !existsSync(path.join(unknownRepo, ".contribution")),
    "unknown command created report artifacts",
  );

  const initRepo = createGitRepo(tempRoot, "init-repo").repo;
  const init = runCli(binary, ["init"], {
    cwd: initRepo,
    env,
    byName: options.byName,
  });
  assertReferencedPathsExist(init, tempRoot);
  assert(
    existsSync(path.join(initRepo, ".contribution.yml")),
    "init did not write config",
  );
  const initAgain = runCli(binary, ["init"], {
    cwd: initRepo,
    env,
    byName: options.byName,
  });
  assert(
    initAgain.stdout.includes("already exists"),
    "init was not idempotent",
  );

  const doctor = runCli(binary, ["doctor"], {
    cwd: initRepo,
    env,
    byName: options.byName,
  });
  assert(
    doctor.stdout.includes("Contribution.dev doctor"),
    "doctor output missing title",
  );
  assert(
    !doctor.stdout.includes(SECRET_SENTINEL),
    "doctor leaked token sentinel",
  );
  assert(
    /missing \(optional\)/u.test(doctor.stdout),
    "doctor did not exercise missing optional tool handling",
  );

  const cloneFailure = runCli(
    binary,
    [
      "analyze",
      "--repo",
      `https://127.0.0.1/repo.git?token=${SECRET_VALUE}`,
      "--format",
      "json",
      "--public-safe",
      "--no-external-tools",
    ],
    {
      cwd: initRepo,
      env: { ...env, GIT_TERMINAL_PROMPT: "0" },
      expectedStatus: "nonzero",
      byName: options.byName,
    },
  );
  assertResultNoSecrets(cloneFailure);

  const analysisRepo = createGitRepo(tempRoot, "analysis-repo").repo;
  git(analysisRepo, [
    "remote",
    "add",
    "origin",
    `https://token=${SECRET_VALUE}@example.test/private/repo.git`,
  ]);
  writeRepoFile(
    analysisRepo,
    ".gitignore",
    ".contribution/\n.pnpm-store/\n.code-reviews/\n.tools/\nbin/\nnode_modules/\ndocs-shared/\n",
  );
  writeRepoFile(
    analysisRepo,
    "internal/app.go",
    "package app\n\nfunc Value() int { return 1 }\n",
  );
  writeRepoFile(
    analysisRepo,
    "internal/app_test.go",
    'package app\n\nimport "testing"\n\nfunc TestValue(t *testing.T) { if Value() != 1 { t.Fatal(Value()) } }\n',
  );
  writeRepoFile(analysisRepo, "docs/guide.md", "# Guide\n");
  writeRepoFile(analysisRepo, "lint-staged.config.js", "export default [];\n");
  writeRepoFile(analysisRepo, "scripts/review-worker", "#!/bin/sh\n");
  commitAll(analysisRepo, "add app code and tests");
  writeRepoFile(analysisRepo, "internal/untracked.go", "package app\n");
  writeIgnoredArtifacts(analysisRepo);
  const analysisRepoVisibleFiles = gitVisibleExistingFiles(analysisRepo);
  const analysisCoverage = path.join(tempRoot, "analysis-coverage.out");
  writeRepoFile(
    tempRoot,
    "analysis-coverage.out",
    "mode: set\ninternal/app.go:3.1,3.30 1 1\n",
  );

  const privateRemoteRoot = path.join(tempRoot, "analyze-private-remote");
  runCli(
    binary,
    [
      "analyze",
      "--repo",
      ".",
      "--output",
      privateRemoteRoot,
      "--format",
      "json",
      "--no-external-tools",
    ],
    { cwd: analysisRepo, env, byName: options.byName },
  );
  const privateRemoteAnalysis = readJSON(
    path.join(latestRunDir(privateRemoteRoot), "analysis.json"),
  );
  assert(
    privateRemoteAnalysis.repo?.remote_url?.includes("REDACTED"),
    "private analysis did not retain a redacted remote URL",
  );
  assertNoTextInFiles(collectFiles(privateRemoteRoot), [
    SECRET_SENTINEL,
    SECRET_VALUE,
  ]);

  const jsonRoot = path.join(tempRoot, "analyze-json");
  const analyzeJSON = runCli(
    binary,
    [
      "analyze",
      "--repo",
      ".",
      "--output",
      jsonRoot,
      "--format",
      "json",
      "--public-safe",
      "--no-external-tools",
      "--coverage",
      analysisCoverage,
      "--coverage-format",
      "go",
    ],
    { cwd: analysisRepo, env, byName: options.byName },
  );
  assertReferencedPathsExist(analyzeJSON, tempRoot);
  const jsonRun = latestRunDir(jsonRoot);
  assertFilesExist(jsonRun, [
    "analysis.json",
    "profile.export.json",
    "share-card.json",
    "tooling.json",
  ]);
  assertFilesAbsent(jsonRun, ["report.md"]);
  const analysis = assertAnalysisPublicSafe(
    path.join(jsonRun, "analysis.json"),
    analysisRepo,
  );
  assert(analysis.version === 1, "analysis.json version mismatch");
  assert(
    analysis.coverage?.status === "available",
    "analyze did not import coverage",
  );
  assert(analysis.deep_dives, "analysis missing deep_dive evidence object");
  assert(
    Array.isArray(analysis.analyzer_findings),
    "analysis missing analyzer findings array",
  );
  assert(
    Array.isArray(analysis.setup_actions) && analysis.setup_actions.length > 0,
    "analysis missing confidence setup actions",
  );
  assert(
    analysis.inventory?.total_files === analysisRepoVisibleFiles.length,
    `inventory counted ${analysis.inventory?.total_files} files, want ${analysisRepoVisibleFiles.length} Git-visible files`,
  );
  assert(
    analysis.inventory?.config_files > 0,
    "inventory did not classify tracked config files",
  );
  assert(
    analysis.inventory?.source_files >= 3,
    "inventory did not classify tracked and untracked source files",
  );
  assertNoTextInFiles(
    [path.join(jsonRun, "analysis.json")],
    [".pnpm-store", ".code-reviews", ".tools", "node_modules", "docs-shared"],
  );
  assertPublicSafeFiles(
    [
      path.join(jsonRun, "profile.export.json"),
      path.join(jsonRun, "share-card.json"),
    ],
    analysisRepo,
  );
  assertNoTextInFiles(collectFiles(jsonRun), [SECRET_SENTINEL]);

  const defaultOutput = runCli(
    binary,
    [
      "analyze",
      "--repo",
      ".",
      "--format",
      "json",
      "--public-safe",
      "--no-external-tools",
    ],
    { cwd: analysisRepo, env, byName: options.byName },
  );
  assertReferencedPathsExist(defaultOutput, tempRoot);
  const defaultRun = latestRunDir(
    path.join(analysisRepo, ".contribution", "reports"),
  );
  const defaultAnalysis = path.join(defaultRun, "analysis.json");
  assertFilesExist(defaultRun, [
    "analysis.json",
    "profile.export.json",
    "share-card.json",
    "tooling.json",
  ]);
  assertFilesAbsent(defaultRun, ["report.md"]);
  assertAnalysisPublicSafe(defaultAnalysis, analysisRepo);

  const allRoot = path.join(tempRoot, "analyze-all");
  const analyzeAll = runCli(
    binary,
    [
      "analyze",
      "--repo",
      ".",
      "--output",
      allRoot,
      "--format",
      "all",
      "--public-safe",
      "--no-external-tools",
    ],
    { cwd: analysisRepo, env, byName: options.byName },
  );
  assertReferencedPathsExist(analyzeAll, tempRoot);
  const allRun = latestRunDir(allRoot);
  assertFilesExist(allRun, [
    "analysis.json",
    "report.md",
    "profile.export.json",
    "share-card.json",
    "tooling.json",
  ]);
  assertAnalysisPublicSafe(path.join(allRun, "analysis.json"), analysisRepo);
  assertPublicSafeFiles(collectFiles(allRun), analysisRepo);
  assertPublicSafeReportQuality(path.join(allRun, "report.md"));

  const reportRoot = path.join(tempRoot, "report-output");
  const report = runCli(
    binary,
    [
      "report",
      "--input",
      path.join(jsonRun, "analysis.json"),
      "--output",
      reportRoot,
      "--format",
      "markdown",
      "--public-safe",
    ],
    { cwd: analysisRepo, env, byName: options.byName },
  );
  assertReferencedPathsExist(report, tempRoot);
  assertFilesExist(reportRoot, [
    "report.md",
    "profile.export.json",
    "share-card.json",
  ]);
  assertPublicSafeFiles(
    [
      path.join(reportRoot, "profile.export.json"),
      path.join(reportRoot, "share-card.json"),
    ],
    analysisRepo,
  );
  assertPublicSafeReportQuality(path.join(reportRoot, "report.md"));

  const exportRoot = path.join(tempRoot, "export-profile-output");
  const exportProfile = runCli(
    binary,
    [
      "export-profile",
      "--input",
      path.join(jsonRun, "analysis.json"),
      "--output",
      exportRoot,
    ],
    { cwd: analysisRepo, env, byName: options.byName },
  );
  assertReferencedPathsExist(exportProfile, tempRoot);
  assertFilesExist(exportRoot, ["profile.export.json", "share-card.json"]);
  assertFilesAbsent(exportRoot, ["analysis.json", "report.md", "tooling.json"]);
  assertPublicSafeFiles(collectFiles(exportRoot), analysisRepo);

  const redactRoot = path.join(tempRoot, "redact-output");
  const redact = runCli(
    binary,
    [
      "redact",
      "--input",
      path.join(jsonRun, "analysis.json"),
      "--output",
      redactRoot,
      "--format",
      "all",
    ],
    { cwd: analysisRepo, env, byName: options.byName },
  );
  assertReferencedPathsExist(redact, tempRoot);
  assertFilesExist(redactRoot, [
    "analysis.json",
    "report.md",
    "profile.export.json",
    "share-card.json",
  ]);
  assertAnalysisPublicSafe(
    path.join(redactRoot, "analysis.json"),
    analysisRepo,
  );
  assertPublicSafeFiles(collectFiles(redactRoot), analysisRepo);
  assertPublicSafeReportQuality(path.join(redactRoot, "report.md"));

  const privateReportRoot = path.join(tempRoot, "report-private-input");
  const privateReportFixture = path.join(tempRoot, "report-private-fixture");
  writeJSON(
    path.join(privateReportFixture, "analysis.json"),
    analysisFixture(analysisRepo),
  );
  const privateReport = runCli(
    binary,
    [
      "report",
      "--input",
      path.join(privateReportFixture, "analysis.json"),
      "--output",
      privateReportRoot,
      "--format",
      "all",
      "--public-safe",
    ],
    { cwd: analysisRepo, env, byName: options.byName },
  );
  assertReferencedPathsExist(privateReport, tempRoot);
  assertFilesExist(privateReportRoot, [
    "analysis.json",
    "report.md",
    "profile.export.json",
    "share-card.json",
  ]);
  assertAnalysisPublicSafe(
    path.join(privateReportRoot, "analysis.json"),
    analysisRepo,
  );
  assertPublicSafeFiles(collectFiles(privateReportRoot), analysisRepo);
  assertPublicSafeReportQuality(path.join(privateReportRoot, "report.md"));

  const preflightRepoInfo = createGitRepo(tempRoot, "preflight-repo");
  writeRepoFile(
    preflightRepoInfo.repo,
    "internal/auth/session.go",
    "package auth\n\nfunc ValidateSession() bool { return true }\n",
  );
  commitAll(preflightRepoInfo.repo, "change auth session");
  const preflightCoverage = path.join(preflightRepoInfo.repo, "coverage.out");
  writeRepoFile(
    preflightRepoInfo.repo,
    "coverage.out",
    "mode: set\ninternal/auth/session.go:3.1,3.50 1 1\n",
  );
  const preflightRoot = path.join(tempRoot, "preflight-output");
  const preflight = runCli(
    binary,
    [
      "preflight",
      "--base",
      preflightRepoInfo.sha,
      "--head",
      "HEAD",
      "--output",
      preflightRoot,
      "--format",
      "json",
      "--coverage",
      preflightCoverage,
      "--coverage-format",
      "go",
    ],
    { cwd: preflightRepoInfo.repo, env, byName: options.byName },
  );
  assertReferencedPathsExist(preflight, tempRoot);
  const preflightJSON = readJSON(
    path.join(latestRunDir(preflightRoot), "preflight.json"),
  );
  assert(preflightJSON.version === 2, "preflight did not emit V2 schema");
  assert(
    preflightJSON.changed_files?.some(
      (file) => file.path === "internal/auth/session.go",
    ),
    "preflight missing changed risky file",
  );
  assert(
    preflightJSON.changed_files?.some((file) => file.line_ranges?.length > 0),
    "preflight missing changed line ranges",
  );
  assert(
    preflightJSON.risk_level === "high",
    "preflight did not flag risky source change",
  );
  assert(
    preflightJSON.file_summary?.risky_files > 0,
    "preflight risky file count missing",
  );
  assert(
    preflightJSON.coverage?.status === "available",
    "preflight did not import changed-line coverage",
  );
  assert(
    preflightJSON.rubric?.some((item) => item.id === "changed_line_coverage"),
    "preflight missing changed-line coverage rubric",
  );
  assert(
    preflightJSON.personal_context?.artifacts_analyzed > 0,
    "preflight missing personal context",
  );
  assert(
    preflightJSON.rubric?.some((item) => item.id === "personal_no_test_repeat"),
    "preflight missing personal no-test rubric",
  );

  const packetRepo = createGitRepo(tempRoot, "packet-repo").repo;
  const packetRoot = path.join(tempRoot, "packet-output");
  const fixtureDir = path.join(packetRoot, "fixture");
  writeJSON(
    path.join(fixtureDir, "analysis.json"),
    analysisFixture(packetRepo),
  );
  const packet = runCli(
    binary,
    ["packet", "--pr", "123", "--output", packetRoot],
    { cwd: packetRepo, env, byName: options.byName },
  );
  assertReferencedPathsExist(packet, tempRoot);
  const packetJSON = readJSON(findPacketOutput(packetRoot));
  assert(packetJSON.version === 2, "packet did not emit V2 schema");
  assert(packetJSON.packet_id, "packet missing stable packet_id");
  assert(
    packetJSON.public_safe === true,
    "packet is not public-safe by default",
  );
  assert(!packetJSON.repo?.root, "packet did not redact repo root");
  assert(!packetJSON.repo?.remote_url, "packet did not redact repo remote");
  assert(!packetJSON.card?.url, "packet did not redact PR URL");
  assert(
    (packetJSON.card?.risks ?? []).length === 0,
    "packet did not redact risks",
  );
  assert(
    packetJSON.artifact_label === "PR #123",
    "packet did not use neutral public artifact label",
  );
  assert(packetJSON.rubric?.length > 0, "packet missing structured rubric");
  assertPublicSafeFiles([findPacketOutput(packetRoot)], packetRepo);

  const feedbackRoot = path.join(tempRoot, "feedback-output");
  const feedbackFixture = path.join(tempRoot, "friend-feedback.export.json");
  writeJSON(feedbackFixture, {
    version: 1,
    packet_id: packetJSON.packet_id,
    submitted_at: "2026-05-28T00:00:00Z",
    reviewer_label: "Reviewer 1",
    overall_trust: "high",
    confidence: "medium",
    answers: [
      {
        question_id: "problem_fit",
        question: "Does this change solve the intended problem cleanly?",
        answer:
          "The change is focused and solves the stated problem with enough context.",
      },
      {
        question_id: "test_evidence",
        question: "Are tests appropriate for the changed behavior?",
        answer:
          "The tests cover the changed behavior and make the review easier.",
      },
    ],
    public_safe: true,
  });
  const feedbackImport = runCli(
    binary,
    [
      "import-feedback",
      "--analysis",
      path.join(fixtureDir, "analysis.json"),
      "--feedback",
      feedbackFixture,
      "--output",
      feedbackRoot,
      "--format",
      "all",
      "--public-safe",
    ],
    { cwd: packetRepo, env, byName: options.byName },
  );
  assertReferencedPathsExist(feedbackImport, tempRoot);
  assertFilesExist(feedbackRoot, [
    "analysis.json",
    "report.md",
    "profile.export.json",
    "share-card.json",
    "tooling.json",
  ]);
  const importedFeedback = assertAnalysisPublicSafe(
    path.join(feedbackRoot, "analysis.json"),
    packetRepo,
  );
  assert(
    importedFeedback.signals?.some(
      (signal) => signal.source === "friend_feedback",
    ),
    "feedback import did not add friend_feedback signals",
  );
  assertPublicSafeFiles(collectFiles(feedbackRoot), packetRepo);
  assertPublicSafeReportQuality(path.join(feedbackRoot, "report.md"));
}

function currentGoTarget() {
  const goos = {
    darwin: "darwin",
    linux: "linux",
    win32: "windows",
  }[process.platform];
  const goarch = {
    arm64: "arm64",
    x64: "amd64",
  }[process.arch];
  assert(
    goos && goarch,
    `unsupported release smoke target ${process.platform}/${process.arch}`,
  );
  return { goos, goarch };
}

function findCurrentRunnerArchive() {
  const dist = path.join(ROOT, "dist");
  assert(existsSync(dist), "dist directory missing after GoReleaser snapshot");
  const { goos, goarch } = currentGoTarget();
  const archives = collectFiles(dist).filter((file) => {
    const lower = path.basename(file).toLowerCase();
    return (
      (lower.endsWith(".tar.gz") || lower.endsWith(".zip")) &&
      lower.includes(goos) &&
      lower.includes(goarch)
    );
  });
  assert(
    archives.length > 0,
    `no archive found for ${goos}/${goarch}; found ${collectFiles(dist).join(", ")}`,
  );
  return archives[0];
}

function unpackArchive(archive, targetDir) {
  mkdirSync(targetDir, { recursive: true });
  if (archive.endsWith(".zip")) {
    run("unzip", ["-q", archive, "-d", targetDir]);
    return;
  }
  run("tar", ["-xzf", archive, "-C", targetDir]);
}

function findContributionBinary(root) {
  const binaryName =
    process.platform === "win32" ? "contribution.exe" : "contribution";
  const matches = collectFiles(root).filter(
    (file) => path.basename(file) === binaryName,
  );
  assert(matches.length > 0, `no ${binaryName} binary found under ${root}`);
  return matches[0];
}

function runReleaseArtifactSmoke(tempRoot) {
  run("goreleaser", ["release", "--snapshot", "--clean"]);
  const archive = findCurrentRunnerArchive();
  const extractDir = path.join(tempRoot, "release-artifact");
  unpackArchive(archive, extractDir);
  const binary = findContributionBinary(extractDir);
  const home = path.join(tempRoot, "release-home");
  mkdirSync(home, { recursive: true });
  const env = dogfoodEnv({
    home,
    pathValue: `${path.dirname(binary)}${path.delimiter}${process.env.PATH}`,
    includeTokenSentinel: false,
  });

  const version = runCli(binary, ["version"], { env, byName: true });
  assert(
    !version.stdout.includes("contribution dev"),
    "release artifact uses dev version",
  );
  assert(
    !version.stdout.includes("commit: none"),
    "release artifact uses default commit",
  );
  assert(
    !version.stdout.includes("date: unknown"),
    "release artifact uses default date",
  );

  const repo = createGitRepo(tempRoot, "release-clean-repo").repo;
  runCli(binary, ["doctor"], { cwd: repo, env, byName: true });
  runCli(binary, ["init"], { cwd: repo, env, byName: true });
  const output = path.join(tempRoot, "release-analyze");
  runCli(
    binary,
    [
      "analyze",
      "--repo",
      ".",
      "--output",
      output,
      "--format",
      "json",
      "--public-safe",
      "--no-external-tools",
    ],
    { cwd: repo, env, byName: true },
  );
  const releaseRun = latestRunDir(output);
  assertFilesExist(releaseRun, [
    "analysis.json",
    "profile.export.json",
    "share-card.json",
    "tooling.json",
  ]);
  assertAnalysisPublicSafe(path.join(releaseRun, "analysis.json"), repo);
  assertPublicSafeFiles(collectFiles(releaseRun), repo);
}

function runRealRepoDogfood(binary, tempRoot) {
  const home = path.join(tempRoot, "real-home");
  mkdirSync(home, { recursive: true });
  const env = dogfoodEnv({
    home,
    pathValue: process.env.PATH,
    includeTokenSentinel: false,
  });
  const output = path.join(tempRoot, "real-repo-analysis");
  const beforeVisibleFiles = gitVisibleExistingFiles(ROOT);
  const headSHA = currentHeadSHA(ROOT);

  const analyze = runCli(
    binary,
    [
      "analyze",
      "--repo",
      ROOT,
      "--output",
      output,
      "--format",
      "all",
      "--public-safe",
      "--no-external-tools",
    ],
    { cwd: ROOT, env },
  );
  assertReferencedPathsExist(analyze, tempRoot);
  const runDir = latestRunDir(output);
  assertFilesExist(runDir, [
    "analysis.json",
    "report.md",
    "profile.export.json",
    "share-card.json",
    "tooling.json",
  ]);
  const analysisPath = path.join(runDir, "analysis.json");
  const analysis = assertAnalysisPublicSafe(analysisPath, ROOT);
  assert(
    analysis.inventory?.total_files === beforeVisibleFiles.length,
    `real repo inventory counted ${analysis.inventory?.total_files} files, want ${beforeVisibleFiles.length} Git-visible files`,
  );
  assert(
    analysis.weakness_map?.confidence !== "high",
    "local-only weakness map confidence was high",
  );
  assert(
    analysis.profile?.confidence !== "high",
    "local-only profile confidence was high",
  );
  assertPublicSafeFiles(collectFiles(runDir), ROOT);
  assertPublicSafeReportQuality(path.join(runDir, "report.md"));
  assertNoTextInFiles(collectFiles(runDir), [
    ROOT,
    headSHA,
    headSHA.slice(0, 8),
    /https?:\/\/[^\s"']+/iu,
    ".pnpm-store",
    ".code-reviews",
    ".tools",
    "node_modules",
    "docs-shared",
    "/Users/gabe",
  ]);

  const redactRoot = path.join(tempRoot, "real-redact-output");
  runCli(
    binary,
    [
      "redact",
      "--input",
      analysisPath,
      "--output",
      redactRoot,
      "--format",
      "all",
    ],
    { cwd: ROOT, env },
  );
  assertAnalysisPublicSafe(path.join(redactRoot, "analysis.json"), ROOT);
  assertPublicSafeFiles(collectFiles(redactRoot), ROOT);
  assertPublicSafeReportQuality(path.join(redactRoot, "report.md"));
  assertNoTextInFiles(collectFiles(redactRoot), [
    ROOT,
    headSHA,
    headSHA.slice(0, 8),
    "/Users/gabe",
  ]);
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  const binary = path.resolve(args.binary || DEFAULT_BINARY);
  const tempRoot = mkdtempSync(path.join(TMP_PARENT, "contribution-dogfood-"));

  try {
    if (!args.skipBuild) {
      buildBinary(binary);
    } else {
      assert(existsSync(binary), `binary does not exist: ${binary}`);
    }

    if (args.mode === "real") {
      runRealRepoDogfood(binary, tempRoot);
    } else {
      runSmoke(binary, tempRoot);
    }
    if (args.mode === "release") {
      runReleaseArtifactSmoke(tempRoot);
    }
    console.log(`[dogfood-cli] PASS mode=${args.mode}`);
  } finally {
    if (args.keepTemp) {
      console.log(`[dogfood-cli] kept temp workspace: ${tempRoot}`);
    } else {
      rmSync(tempRoot, { recursive: true, force: true });
    }
  }
}

try {
  main();
} catch (error) {
  console.error(`[dogfood-cli] ${error.message}`);
  process.exit(1);
}
