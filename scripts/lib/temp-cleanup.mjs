import {
  chmodSync,
  existsSync,
  lstatSync,
  readdirSync,
  realpathSync,
  rmSync,
} from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";

export const PROJECT_TEMP_PREFIX = "contribution-";
export const DEFAULT_STALE_TEMP_HOURS = 24;
export const PRESERVED_PROJECT_TEMP_PREFIXES = ["contribution-code-reviews-"];

function normalizeHours(value, fallback) {
  const number = Number(value);
  if (!Number.isFinite(number) || number < 0) {
    return fallback;
  }
  return number;
}

function normalizedParents(candidates) {
  const seen = new Set();
  const parents = [];
  for (const candidate of candidates) {
    const parent = String(candidate ?? "").trim();
    if (!parent || !existsSync(parent)) {
      continue;
    }
    let key;
    try {
      key = realpathSync.native(parent);
    } catch {
      key = path.resolve(parent);
    }
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    parents.push(parent);
  }
  return parents;
}

export function projectTempParents() {
  return normalizedParents(["/tmp", "/private/tmp", tmpdir()]);
}

function isPreservedProjectTempName(name) {
  return PRESERVED_PROJECT_TEMP_PREFIXES.some((prefix) =>
    name.startsWith(prefix),
  );
}

function isEligibleProjectTempName(name) {
  return (
    name.startsWith(PROJECT_TEMP_PREFIX) && !isPreservedProjectTempName(name)
  );
}

function makeWritableRecursive(target) {
  let stats;
  try {
    stats = lstatSync(target);
  } catch {
    return;
  }

  if (stats.isSymbolicLink()) {
    return;
  }

  try {
    chmodSync(target, stats.mode | (stats.isDirectory() ? 0o700 : 0o600));
  } catch {}

  if (!stats.isDirectory()) {
    return;
  }

  let entries;
  try {
    entries = readdirSync(target);
  } catch {
    return;
  }

  for (const entry of entries) {
    makeWritableRecursive(path.join(target, entry));
  }
}

export function cleanupProjectTempRoots(options = {}) {
  const now = Number.isFinite(Number(options.now))
    ? Number(options.now)
    : Date.now();
  const olderThanHours = normalizeHours(
    options.olderThanHours,
    DEFAULT_STALE_TEMP_HOURS,
  );
  const olderThanMs = Number.isFinite(Number(options.olderThanMs))
    ? Number(options.olderThanMs)
    : olderThanHours * 60 * 60 * 1000;
  const parents = normalizedParents(options.parents ?? projectTempParents());
  const result = {
    parents,
    removed: [],
    skippedFresh: [],
    skippedPreserved: [],
    warnings: [],
  };

  for (const parent of parents) {
    let entries;
    try {
      entries = readdirSync(parent, { withFileTypes: true });
    } catch (error) {
      result.warnings.push(`${parent}: ${error.message}`);
      continue;
    }

    for (const entry of entries) {
      if (!entry.name.startsWith(PROJECT_TEMP_PREFIX)) {
        continue;
      }
      const target = path.join(parent, entry.name);
      if (isPreservedProjectTempName(entry.name)) {
        result.skippedPreserved.push(target);
        continue;
      }
      if (!isEligibleProjectTempName(entry.name)) {
        continue;
      }

      let stats;
      try {
        stats = lstatSync(target);
      } catch {
        continue;
      }

      if (olderThanMs > 0 && now - stats.mtimeMs < olderThanMs) {
        result.skippedFresh.push(target);
        continue;
      }

      if (!options.dryRun) {
        try {
          makeWritableRecursive(target);
          rmSync(target, {
            recursive: true,
            force: true,
            maxRetries: 3,
            retryDelay: 100,
          });
        } catch (error) {
          result.warnings.push(`${target}: ${error.message}`);
          continue;
        }
      }
      result.removed.push(target);
    }
  }

  return result;
}

export function cleanupStaleProjectTempRoots(options = {}) {
  const result = cleanupProjectTempRoots(options);
  if (!options.quiet && result.removed.length > 0) {
    console.log(
      `[temp-cleanup] removed stale project temp paths count=${result.removed.length}`,
    );
  }
  if (!options.quiet) {
    for (const warning of result.warnings) {
      console.warn(`[temp-cleanup] warning ${warning}`);
    }
  }
  return result;
}
