function normalizePath(value) {
  return String(value ?? "").replaceAll("\\", "/");
}

function dedupe(values) {
  return [...new Set(values)];
}

const CONTRACT_SENSITIVE_DOMAINS = [
  {
    key: "cli-contract",
    reviewMode: "cli-contract",
    authoritativeDocs: [
      "docs/reference/architecture.md",
      "docs/cli-contract.md",
      "docs/tooling-validation.md",
    ],
    agentPaths: ["AGENTS.md"],
    reviewFocusLines: [
      "- did this change command names, flags, stdout, stderr, or exit behavior?",
      "- did this make command behavior depend on hidden process-global state?",
      "- did this remove a regression test for CLI-visible behavior?",
    ],
    fastValidationCommands: [
      {
        cmd: "go",
        args: ["test", "./..."],
      },
      {
        cmd: "pnpm",
        args: ["dogfood:smoke"],
      },
      {
        cmd: "node",
        args: ["scripts/check-cli-contract-coverage.mjs"],
        receivesChangedFiles: true,
      },
    ],
    pathPatterns: [
      /^cmd\//,
      /^internal\/analysis\//,
      /^internal\/cli\//,
      /^internal\/coverage\//,
      /^internal\/fileclass\//,
      /^internal\/friend\//,
      /^internal\/config\//,
      /^internal\/git\//,
      /^internal\/github\//,
      /^internal\/preflight\//,
      /^internal\/privacy\//,
      /^internal\/publicsafe\//,
      /^internal\/receipt\//,
      /^internal\/report\//,
      /^internal\/signals\//,
      /^internal\/tools\//,
      /^\.goreleaser\.yml$/,
      /^README\.md$/,
      /^docs\/cli-contract\.md$/,
      /^docs\/prd-cli\.md$/,
      /^docs\/product-architecture\.md$/,
      /^docs\/reference\/architecture\.md$/,
      /^scripts\/dogfood-cli\.mjs$/,
      /^scripts\/check-cli-contract-coverage\.mjs$/,
      /^scripts\/lib\/contract-sensitive-domains\.mjs$/,
      /^scripts\/run-changed-checks\.mjs$/,
      /^\.github\/workflows\/ci\.yml$/,
      /^\.github\/workflows\/release\.yml$/,
    ],
  },
];

function matchesDomain(domain, file) {
  const normalized = normalizePath(file);
  return domain.pathPatterns.some((pattern) => pattern.test(normalized));
}

export function isCliContractPath(file) {
  return matchesDomain(CONTRACT_SENSITIVE_DOMAINS[0], file);
}

export function isCliContractCoveragePath(file) {
  const normalized = normalizePath(file);
  if (/^internal\/.*_test\.go$/u.test(normalized)) {
    return true;
  }
  return [
    "scripts/dogfood-cli.mjs",
    "scripts/check-cli-contract-coverage.mjs",
    "scripts/lib/contract-sensitive-domains.mjs",
    "docs/cli-contract.md",
    "docs/reference/architecture.md",
    ".github/workflows/ci.yml",
    ".github/workflows/release.yml",
    "docs/tooling-validation.md",
  ].includes(normalized);
}

export function getContractSensitiveDomains() {
  return CONTRACT_SENSITIVE_DOMAINS.map((domain) => ({
    ...domain,
    authoritativeDocs: [...domain.authoritativeDocs],
    agentPaths: [...domain.agentPaths],
    reviewFocusLines: [...domain.reviewFocusLines],
    fastValidationCommands: domain.fastValidationCommands.map((command) => ({
      cmd: command.cmd,
      args: [...command.args],
      receivesChangedFiles: command.receivesChangedFiles === true,
    })),
  }));
}

export function getMatchingContractDomains(files) {
  const normalizedFiles = files.map(normalizePath);
  return getContractSensitiveDomains().filter((domain) =>
    normalizedFiles.some((file) => matchesDomain(domain, file)),
  );
}

export function hasContractSensitiveDomainChanges(files, domainKey) {
  return getMatchingContractDomains(files).some(
    (domain) => domain.key === domainKey,
  );
}

export function getContractReviewModes(files) {
  return getMatchingContractDomains(files).flatMap((domain) =>
    domain.reviewMode ? [domain.reviewMode] : [],
  );
}

export function getContractReferenceDocs(files) {
  return dedupe(
    getMatchingContractDomains(files).flatMap(
      (domain) => domain.authoritativeDocs,
    ),
  );
}

export function getContractAgentPaths(files) {
  return dedupe(
    getMatchingContractDomains(files).flatMap((domain) => domain.agentPaths),
  );
}

export function getContractFastValidationCommands(files, options = {}) {
  const commands = [];
  const seen = new Set();

  for (const domain of getMatchingContractDomains(files)) {
    for (const command of domain.fastValidationCommands) {
      const key = [command.cmd, ...command.args].join("\u0000");
      if (seen.has(key)) {
        continue;
      }
      seen.add(key);
      commands.push({
        cmd: command.cmd,
        args:
          command.receivesChangedFiles === true
            ? options.filesFrom
              ? [...command.args, "--files-from", options.filesFrom]
              : [...command.args, "--files", ...files.map(normalizePath)]
            : [...command.args],
      });
    }
  }

  return commands;
}

export function getContractCoverageValidationCommands(files, options = {}) {
  if (!hasContractSensitiveDomainChanges(files, "cli-contract")) {
    return [];
  }
  return [
    {
      cmd: "node",
      args: options.filesFrom
        ? [
            "scripts/check-cli-contract-coverage.mjs",
            "--files-from",
            options.filesFrom,
          ]
        : [
            "scripts/check-cli-contract-coverage.mjs",
            "--files",
            ...files.map(normalizePath),
          ],
    },
  ];
}

export function getContractReviewFocusLines(mode) {
  const domain = CONTRACT_SENSITIVE_DOMAINS.find(
    (entry) => entry.reviewMode === mode,
  );
  return domain ? [...domain.reviewFocusLines] : [];
}

export function isContractReviewMode(mode) {
  return CONTRACT_SENSITIVE_DOMAINS.some(
    (domain) => domain.reviewMode === mode,
  );
}

export function isAuthoritativeContractEvidencePath(filePath) {
  const normalized = normalizePath(filePath);
  return (
    normalized === "AGENTS.md" ||
    normalized.endsWith("/AGENTS.md") ||
    normalized === "docs/cli-contract.md" ||
    normalized.startsWith("docs/reference/") ||
    normalized === "docs/tooling-validation.md"
  );
}
