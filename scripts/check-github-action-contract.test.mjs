import { readFile } from "node:fs/promises";
import assert from "node:assert/strict";
import test from "node:test";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

test("preflight action exposes the V2 CLI contract", async () => {
  const text = await readFile(path.join(ROOT, "action.yml"), "utf8");
  for (const input of [
    "base",
    "head",
    "output-dir",
    "coverage",
    "coverage-format",
    "fail-on-risk",
    "version",
  ]) {
    assert.match(text, new RegExp(`\\n  ${input}:\\n`, "u"));
  }
  for (const output of [
    "risk-level",
    "artifact-dir",
    "preflight-json",
    "preflight-markdown",
  ]) {
    assert.match(text, new RegExp(`\\n  ${output}:\\n`, "u"));
  }
  assert.match(text, /go build -trimpath -o "\$RUNNER_TEMP\/contribution"/u);
  assert.match(
    text,
    /go install "github\.com\/contribution-dev\/contribution\/cmd\/contribution@\$\{CONTRIBUTION_VERSION\}"/u,
  );
  assert.match(text, /GITHUB_STEP_SUMMARY/u);
});
