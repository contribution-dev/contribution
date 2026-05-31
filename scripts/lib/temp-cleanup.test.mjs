import test from "node:test";
import assert from "node:assert/strict";
import {
  chmodSync,
  mkdirSync,
  mkdtempSync,
  rmSync,
  utimesSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { cleanupProjectTempRoots } from "./temp-cleanup.mjs";

function touchOld(target) {
  const old = new Date(Date.now() - 48 * 60 * 60 * 1000);
  utimesSync(target, old, old);
}

test("cleans stale project temp paths while preserving fresh and review roots", () => {
  const parent = mkdtempSync(path.join(tmpdir(), "temp-cleanup-parent-"));
  try {
    const stale = path.join(parent, "contribution-old");
    const fresh = path.join(parent, "contribution-fresh");
    const preserved = path.join(parent, "contribution-code-reviews-123");
    mkdirSync(stale);
    writeFileSync(path.join(stale, "file.txt"), "old\n");
    touchOld(path.join(stale, "file.txt"));
    touchOld(stale);
    mkdirSync(fresh);
    mkdirSync(preserved);

    const result = cleanupProjectTempRoots({
      parents: [parent],
      olderThanHours: 24,
    });

    assert.deepEqual(result.removed, [stale]);
    assert.deepEqual(result.skippedFresh, [fresh]);
    assert.deepEqual(result.skippedPreserved, [preserved]);
    assert.deepEqual(result.warnings, []);
  } finally {
    rmSync(parent, { recursive: true, force: true });
  }
});

test("makes read-only module cache directories writable before cleanup", () => {
  const parent = mkdtempSync(path.join(tmpdir(), "temp-cleanup-parent-"));
  try {
    const stale = path.join(parent, "contribution-gomod-cache");
    const nested = path.join(stale, "pkg", "mod", "example.com", "pkg@v1.0.0");
    mkdirSync(nested, { recursive: true });
    writeFileSync(path.join(nested, "go.mod"), "module example.com/pkg\n");
    chmodSync(nested, 0o500);
    touchOld(stale);

    const result = cleanupProjectTempRoots({
      parents: [parent],
      olderThanHours: 24,
    });

    assert.deepEqual(result.removed, [stale]);
    assert.deepEqual(result.warnings, []);
  } finally {
    chmodSync(parent, 0o700);
    rmSync(parent, { recursive: true, force: true });
  }
});
