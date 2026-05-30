import test from "node:test";
import assert from "node:assert/strict";
import { normalizePath, pathMatchesGlob } from "./risk-policy/lib.mjs";

test("risk-policy normalizePath only removes explicit relative prefix", () => {
  assert.equal(normalizePath("./internal/app.go"), "internal/app.go");
  assert.equal(normalizePath("a/file.go"), "a/file.go");
  assert.equal(normalizePath("dir\\file.go"), "dir/file.go");
});

test("risk-policy glob matching keeps top-level directory names", () => {
  assert.equal(pathMatchesGlob("a/file.go", "a/*.go"), true);
  assert.equal(pathMatchesGlob("a/file.go", "file.go"), false);
});
