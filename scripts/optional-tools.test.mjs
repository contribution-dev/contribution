import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

test("optional analyzer installer pins repo-local analyzer tools", () => {
  const script = readFileSync("scripts/install-optional-tools.sh", "utf8");
  assert.match(script, /SEMGREP_VERSION="\$\{SEMGREP_VERSION:-1\.164\.0\}"/);
  assert.match(script, /GITLEAKS_VERSION="\$\{GITLEAKS_VERSION:-8\.28\.0\}"/);
  assert.match(
    script,
    /OSV_SCANNER_VERSION="\$\{OSV_SCANNER_VERSION:-2\.3\.8\}"/,
  );
  assert.match(script, /TRIVY_VERSION="\$\{TRIVY_VERSION:-0\.70\.0\}"/);
  assert.match(script, /--check/);
});

test("package exposes optional analyzer install and check commands", () => {
  const pkg = JSON.parse(readFileSync("package.json", "utf8"));
  assert.equal(
    pkg.scripts["tools:install:optional"],
    "scripts/with-tools bash scripts/install-optional-tools.sh",
  );
  assert.equal(
    pkg.scripts["tools:optional:check"],
    "scripts/with-tools bash scripts/install-optional-tools.sh --check",
  );
  assert.equal(
    pkg.scripts["tools:check"],
    "scripts/with-tools bash scripts/preflight-tools.sh",
  );
  assert.equal(pkg.scripts["tools:preflight"], "pnpm tools:check");
});
