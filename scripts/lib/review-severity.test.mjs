import test from "node:test";
import assert from "node:assert/strict";
import {
  normalizeReviewSeverity,
  parseMinReviewSeverity,
  reviewSeverityRank,
} from "./review-severity.mjs";

test("ranks review severities consistently", () => {
  assert.equal(reviewSeverityRank("none"), 0);
  assert.equal(reviewSeverityRank("minor"), 1);
  assert.equal(reviewSeverityRank("major"), 2);
  assert.equal(reviewSeverityRank("blocker"), 3);
  assert.equal(reviewSeverityRank("unknown"), 0);
});

test("parses none as a real minimum severity", () => {
  assert.equal(parseMinReviewSeverity("none"), "none");
  assert.equal(reviewSeverityRank(parseMinReviewSeverity("none")), 0);
});

test("normalizes invalid severity with caller fallback", () => {
  assert.equal(normalizeReviewSeverity("bad", { fallback: "minor" }), "minor");
});
