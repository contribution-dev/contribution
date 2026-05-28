const CONTROL_PLANE_PREFIXES = [
  ".github/workflows/",
  ".github/policy/",
  "scripts/risk-policy/",
];

const CONTROL_PLANE_DOC_PATH = "docs/tooling-validation.md";
const CONTROL_PLANE_HISTORY_DOC_PATH =
  "docs/reference/tooling-validation-history.md";

function hasAnyPrefix(file, prefixes) {
  return prefixes.some((prefix) => file.startsWith(prefix));
}

export function listControlPlaneFiles(files) {
  return files.filter((file) => hasAnyPrefix(file, CONTROL_PLANE_PREFIXES));
}

export function hasControlPlaneChanges(files) {
  return listControlPlaneFiles(files).length > 0;
}

export function isControlPlaneDocUpdated(files) {
  return files.includes(CONTROL_PLANE_DOC_PATH);
}

export function buildControlPlaneDocSyncErrorMessage({
  changedFiles,
  diffRange,
}) {
  const controlPlaneFiles = listControlPlaneFiles(changedFiles);
  const header = `Control-plane changes require updating ${CONTROL_PLANE_DOC_PATH}.`;
  const lines = [header];

  if (diffRange) {
    lines.push(`Diff range: ${diffRange}`);
  }

  if (controlPlaneFiles.length > 0) {
    lines.push("Control-plane files in range:");
    for (const file of controlPlaneFiles) {
      lines.push(`- ${file}`);
    }
  }

  const noteCommand = diffRange
    ? `pnpm tooling:control-plane:note --diff-range "${diffRange}"`
    : "pnpm tooling:control-plane:note --staged";

  lines.push(
    `Fix: update ${CONTROL_PLANE_DOC_PATH} for current workflow changes, then use ${noteCommand} for dated history notes when needed.`,
  );

  return lines.join("\n");
}

export {
  CONTROL_PLANE_DOC_PATH,
  CONTROL_PLANE_HISTORY_DOC_PATH,
  CONTROL_PLANE_PREFIXES,
};
