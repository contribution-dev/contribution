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
    authoritativeDocs: ["docs/reference/architecture.md"],
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
    ],
    pathPatterns: [
      /^cmd\//,
      /^internal\/cli\//,
      /^docs\/reference\/architecture\.md$/,
    ],
  },
];

function matchesDomain(domain, file) {
  const normalized = normalizePath(file);
  return domain.pathPatterns.some((pattern) => pattern.test(normalized));
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

export function getContractFastValidationCommands(files) {
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
        args: [...command.args],
      });
    }
  }

  return commands;
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
    normalized.startsWith("docs/reference/") ||
    normalized === "docs/tooling-validation.md"
  );
}
