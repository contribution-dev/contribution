import assert from "node:assert/strict";
import test from "node:test";

import { buildReviewArtifacts } from "./codex-review-findings.mjs";

const SHA = "0123456789abcdef0123456789abcdef01234567";

function build(overrides = {}) {
  return buildReviewArtifacts({
    newData: {
      summary: "review stopped after completed passes",
      findings: [],
      ...overrides.newData,
    },
    targetJson: "/tmp/missing-review.json",
    targetMd: "/tmp/missing-review.md",
    commitSha: SHA,
    triggerName: "manual",
    reviewStatus: "partial_success",
    findingModelLabel: "codex",
    repoName: "contribution",
    repoRoot: "/tmp/contribution",
    ...overrides,
  });
}

test("partial success without findings does not require a remediation handoff", () => {
  const result = build();

  assert.equal(result.findingsCount, 0);
  assert.equal(result.actionRequired, false);
  assert.match(result.markdown, /^# Codex Review Report:/u);
  assert.doesNotMatch(result.markdown, /^# Codex Review Action Needed:/u);
});

test("partial success with findings still requires a remediation handoff", () => {
  const result = build({
    newData: {
      findings: [
        {
          finding_id: "finding-1",
          severity: "major",
          confidence: 0.98,
          title: "Regresses review output",
          hypothesis: "A reviewed behavior regressed.",
          impact: "The gate can miss a real defect.",
          recommended_direction: "Preserve the existing contract.",
        },
      ],
    },
  });

  assert.equal(result.findingsCount, 1);
  assert.equal(result.actionRequired, true);
  assert.match(result.markdown, /^# Codex Review Action Needed:/u);
});
