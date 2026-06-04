import test from "node:test";
import assert from "node:assert/strict";
import { normalizePath, pathMatchesGlob } from "./risk-policy/lib.mjs";
import {
  isTrustedFindingComment,
  normalizeActionableFindings,
  normalizeMaxFindings,
} from "./risk-policy/review-findings-normalize.mjs";
import { isSameRepositoryPullRequest } from "./risk-policy/review-automation-lib.mjs";

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
    user: { login: "coderabbitai[bot]" },
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

test("risk-policy findings require trusted comment provenance", () => {
  const body = `- Commit: \`abcdef123456\`

### [finding-1] [MAJOR] Finding 1 (confidence 0.9)

Evidence: \`internal/file.go:1\`
`;
  const trusted = normalizeActionableFindings({
    comments: [{ user: { login: "coderabbitai[bot]" }, body }],
    headSha: "abcdef123456",
    maxFindings: 5,
  });
  assert.equal(trusted.length, 1);

  const untrusted = normalizeActionableFindings({
    comments: [{ user: { login: "contributor" }, body }],
    headSha: "abcdef123456",
    maxFindings: 5,
  });
  assert.equal(untrusted.length, 0);
  assert.equal(
    isTrustedFindingComment({ user: { login: "contributor" } }, [
      "coderabbitai[bot]",
    ]),
    false,
  );
});

test("risk-policy detects same-repository pull requests", () => {
  assert.equal(
    isSameRepositoryPullRequest({
      head: { repo: { full_name: "Owner/Repo" } },
      base: { repo: { full_name: "owner/repo" } },
    }),
    true,
  );
  assert.equal(
    isSameRepositoryPullRequest({
      head: { repo: { full_name: "fork/repo" } },
      base: { repo: { full_name: "owner/repo" } },
    }),
    false,
  );
});
