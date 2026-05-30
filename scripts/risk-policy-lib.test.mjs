import test from "node:test";
import assert from "node:assert/strict";
import { normalizePath, pathMatchesGlob } from "./risk-policy/lib.mjs";
import {
  normalizeActionableFindings,
  normalizeMaxFindings,
} from "./risk-policy/review-findings-normalize.mjs";

test("risk-policy normalizePath only removes explicit relative prefix", () => {
  assert.equal(normalizePath("./internal/app.go"), "internal/app.go");
  assert.equal(normalizePath("a/file.go"), "a/file.go");
  assert.equal(normalizePath("dir\\file.go"), "dir/file.go");
});

test("risk-policy glob matching keeps top-level directory names", () => {
  assert.equal(pathMatchesGlob("a/file.go", "a/*.go"), true);
  assert.equal(pathMatchesGlob("a/file.go", "file.go"), false);
});

test("risk-policy findings max uses bounded fallback for invalid input", () => {
  assert.equal(normalizeMaxFindings(Number.NaN), 5);
  assert.equal(normalizeMaxFindings("0"), 5);
  assert.equal(normalizeMaxFindings("1000"), 50);

  const comments = Array.from({ length: 6 }, (_, index) => ({
    body: `- Commit: \`abcdef123456\`

### [finding-${index}] [MAJOR] Finding ${index} (confidence 0.9)

Evidence: \`internal/file${index}.go:1\`
`,
  }));

  const findings = normalizeActionableFindings({
    comments,
    headSha: "abcdef123456",
    maxFindings: Number.NaN,
  });

  assert.equal(findings.length, 5);
});
