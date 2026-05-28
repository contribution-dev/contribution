import test from "node:test";
import assert from "node:assert/strict";
import { classifyChangedFiles } from "./changed-files.mjs";

test("classifies Go CLI changes as app-relevant for changed checks", () => {
  const result = classifyChangedFiles(["cmd/contribution/main.go"]);
  assert.equal(result.goRelevant, true);
  assert.equal(result.appRelevant, true);
});

test("classifies docs-only changes", () => {
  const result = classifyChangedFiles([
    "docs/tooling-validation.md",
    "README.md",
  ]);
  assert.equal(result.docsOnly, true);
  assert.equal(result.goRelevant, false);
});
