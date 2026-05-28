import { execFileSync } from "node:child_process";
import { resolveBaseRef } from "./changed-files.mjs";

export const ZERO_SHA = "0000000000000000000000000000000000000000";

export function parseRefUpdates(text) {
  const lines = String(text ?? "")
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);

  return lines
    .map((line) => line.split(/\s+/))
    .filter((parts) => parts.length >= 4)
    .map(([localRef, localSha, remoteRef, remoteSha]) => ({
      localRef,
      localSha,
      remoteRef,
      remoteSha,
    }));
}

export function defaultGitExec(args, { cwd, timeoutMs } = {}) {
  return execFileSync("git", args, {
    cwd,
    timeout: timeoutMs,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
}

function parseLines(raw) {
  return String(raw ?? "")
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
}

function branchNameFromLocalRef(localRef) {
  const ref = String(localRef ?? "").trim();
  if (!ref.startsWith("refs/heads/")) return "";
  return ref.slice("refs/heads/".length).trim();
}

function safeGit(gitExec, args) {
  try {
    return String(gitExec(args) ?? "").trim();
  } catch {
    return "";
  }
}

function tryGit(gitExec, args) {
  try {
    return {
      ok: true,
      output: String(gitExec(args) ?? "").trim(),
      error: "",
    };
  } catch (error) {
    return {
      ok: false,
      output: "",
      error: String(error?.message ?? error),
    };
  }
}

function gitObjectExists(gitExec, revision) {
  const value = String(revision ?? "").trim();
  if (!value || value === ZERO_SHA) return false;
  try {
    gitExec(["rev-parse", "--verify", "--quiet", `${value}^{commit}`]);
    return true;
  } catch {
    return false;
  }
}

function fetchedRemoteRef(remoteName, branchName) {
  return `refs/remotes/${remoteName}/${branchName}`;
}

function buildFallbackRevArgs({ update, remoteRefs, gitExec }) {
  if (Array.isArray(remoteRefs) && remoteRefs.length > 0) {
    return ["rev-list", "--reverse", update.localSha, "--not", ...remoteRefs];
  }

  const localRefs = parseLines(
    gitExec(["for-each-ref", "--format=%(refname)", "refs/heads"]),
  ).filter((ref) => ref !== update.localRef);

  const revArgs = ["rev-list", "--reverse", update.localSha];
  if (localRefs.length > 0) {
    revArgs.push("--not", ...localRefs);
  }
  return revArgs;
}

function ensurePushTipIncluded(shas, update) {
  const ordered = Array.isArray(shas) ? [...shas] : [];
  if (
    update?.remoteSha !== ZERO_SHA &&
    update?.localSha &&
    !ordered.includes(update.localSha)
  ) {
    ordered.push(update.localSha);
  }
  return ordered;
}

export function collectOutgoingByUpdate({ updates, remoteName = "", gitExec }) {
  const seen = new Set();
  const orderedShas = [];
  const shaBranches = new Map();

  const remoteRefs =
    remoteName && remoteName.trim()
      ? parseLines(
          gitExec([
            "for-each-ref",
            "--format=%(refname)",
            `refs/remotes/${remoteName}/*`,
          ]),
        )
      : [];

  for (const update of Array.isArray(updates) ? updates : []) {
    if (!update.localRef || !update.localRef.startsWith("refs/heads/")) {
      continue;
    }
    if (update.localSha === ZERO_SHA) continue;
    const branchName = branchNameFromLocalRef(update.localRef);

    let revArgs;
    let usedFallback = false;
    if (update.remoteSha === ZERO_SHA) {
      usedFallback = true;
      revArgs = buildFallbackRevArgs({ update, remoteRefs, gitExec });
    } else {
      const hasLocalSha = gitObjectExists(gitExec, update.localSha);
      const hasRemoteSha = gitObjectExists(gitExec, update.remoteSha);
      usedFallback = !(hasLocalSha && hasRemoteSha);
      revArgs =
        hasLocalSha && hasRemoteSha
          ? ["rev-list", "--reverse", `${update.remoteSha}..${update.localSha}`]
          : buildFallbackRevArgs({ update, remoteRefs, gitExec });
    }

    let revOutput;
    try {
      revOutput = parseLines(gitExec(revArgs));
    } catch (error) {
      const message = String(error?.message ?? "");
      if (
        revArgs.length >= 3 &&
        revArgs[0] === "rev-list" &&
        revArgs[1] === "--reverse" &&
        message.includes("Invalid revision range")
      ) {
        usedFallback = true;
        revOutput = parseLines(
          gitExec(buildFallbackRevArgs({ update, remoteRefs, gitExec })),
        );
      } else {
        throw error;
      }
    }

    if (usedFallback) {
      revOutput = ensurePushTipIncluded(revOutput, update);
    }
    for (const sha of revOutput) {
      if (!seen.has(sha)) {
        seen.add(sha);
        orderedShas.push(sha);
      }
      if (!branchName) continue;
      const branches = shaBranches.get(sha) ?? new Set();
      branches.add(branchName);
      shaBranches.set(sha, branches);
    }
  }

  return { orderedShas, shaBranches };
}

export function computeOutgoingShas({ updates, remoteName = "", gitExec }) {
  return collectOutgoingByUpdate({ updates, remoteName, gitExec }).orderedShas;
}

function refExists(gitExec, ref) {
  return (
    safeGit(gitExec, ["rev-parse", "--verify", "--quiet", `${ref}^{commit}`])
      .length > 0
  );
}

function mergeBase(gitExec, left, right) {
  const base = safeGit(gitExec, ["merge-base", left, right]);
  return base || "";
}

function isAncestor(gitExec, ancestor, descendant) {
  if (!ancestor || !descendant) return false;
  try {
    gitExec(["merge-base", "--is-ancestor", ancestor, descendant]);
    return true;
  } catch {
    return false;
  }
}

function resolveFallbackBase({ repoRoot, remoteName, localSha, gitExec }) {
  const remoteMainRef = fetchedRemoteRef(remoteName, "main");
  if (remoteName && refExists(gitExec, remoteMainRef)) {
    const base = mergeBase(gitExec, remoteMainRef, localSha);
    if (base) return base;
  }
  const { baseRef } = resolveBaseRef(repoRoot, "", localSha);
  return baseRef;
}

function fetchRemoteBranch({ remoteName, branchName, gitExec }) {
  gitExec([
    "fetch",
    "--quiet",
    "--no-tags",
    remoteName,
    `refs/heads/${branchName}:${fetchedRemoteRef(remoteName, branchName)}`,
  ]);
}

export function summarizeBranchState(branchState) {
  return [
    `branch=${branchState.branchName}`,
    `local=${branchState.localSha}`,
    `advertised=${branchState.advertisedRemoteSha || ZERO_SHA}`,
    `fetched=${branchState.fetchedRemoteSha || ZERO_SHA}`,
    `relation=${branchState.relation}`,
    `base=${branchState.canonicalBaseSha || "none"}`,
  ].join(" ");
}

export function resolvePrePushBranchState({
  stdinText,
  remoteName,
  repoRoot = process.cwd(),
  fetchRemote = true,
  fetchTimeoutMs = 20_000,
  blockOnRemoteDivergence = true,
  gitExec = (args) =>
    defaultGitExec(args, {
      cwd: repoRoot,
      timeoutMs: fetchRemote ? fetchTimeoutMs : undefined,
    }),
} = {}) {
  const updates = parseRefUpdates(stdinText);
  const headBranchName =
    safeGit(gitExec, ["rev-parse", "--abbrev-ref", "HEAD"]) || "HEAD";
  const headSha = safeGit(gitExec, ["rev-parse", "HEAD"]);
  const hasNonBranchObjectUpdates = updates.some(
    (update) =>
      update.localSha !== ZERO_SHA &&
      String(update.localRef ?? "").trim().length > 0 &&
      !String(update.localRef ?? "")
        .trim()
        .startsWith("refs/heads/"),
  );
  const branches = [];

  for (const update of updates) {
    const branchName = branchNameFromLocalRef(update.localRef);
    if (!branchName || update.localSha === ZERO_SHA) continue;

    const advertisedRemoteSha =
      String(update.remoteSha ?? "").trim() || ZERO_SHA;
    let fetchedRemoteSha = "";
    let relation;
    let canonicalBaseSha = "";
    let fetchError = "";

    const remoteHeadResult = tryGit(gitExec, [
      "ls-remote",
      "--heads",
      remoteName,
      `refs/heads/${branchName}`,
    ]);
    if (!remoteHeadResult.ok) {
      fetchError = remoteHeadResult.error;
    } else {
      try {
        const remoteHead = remoteHeadResult.output;
        const remoteHeadSha = remoteHead.split(/\s+/)[0] ?? "";

        if (remoteHeadSha) {
          if (fetchRemote) {
            fetchRemoteBranch({ remoteName, branchName, gitExec });
          }
          fetchedRemoteSha =
            safeGit(gitExec, [
              "rev-parse",
              "--verify",
              "--quiet",
              `${fetchedRemoteRef(remoteName, branchName)}^{commit}`,
            ]) || remoteHeadSha;
        }
      } catch (error) {
        fetchError = String(error?.message ?? error);
      }
    }

    if (fetchError) {
      relation = "fetch_failed";
    } else if (!fetchedRemoteSha) {
      relation = "new_branch";
      canonicalBaseSha = resolveFallbackBase({
        repoRoot,
        remoteName,
        localSha: update.localSha,
        gitExec,
      });
    } else if (advertisedRemoteSha === fetchedRemoteSha) {
      relation = "clean";
      canonicalBaseSha =
        mergeBase(gitExec, fetchedRemoteSha, update.localSha) ||
        fetchedRemoteSha;
    } else if (isAncestor(gitExec, fetchedRemoteSha, update.localSha)) {
      relation = "remote_moved_but_local_contains_it";
      canonicalBaseSha =
        mergeBase(gitExec, fetchedRemoteSha, update.localSha) ||
        fetchedRemoteSha;
    } else {
      relation = "remote_moved_and_local_missing_it";
      canonicalBaseSha = blockOnRemoteDivergence
        ? ""
        : mergeBase(gitExec, fetchedRemoteSha, update.localSha) ||
          resolveFallbackBase({
            repoRoot,
            remoteName,
            localSha: update.localSha,
            gitExec,
          });
    }

    branches.push({
      branchName,
      localRef: update.localRef,
      remoteRef: update.remoteRef,
      localSha: update.localSha,
      advertisedRemoteSha,
      fetchedRemoteSha,
      relation,
      canonicalBaseSha,
      fetchError,
      isCurrentHead: branchName === headBranchName,
    });
  }

  const blocked = branches.filter(
    (branch) =>
      branch.relation === "fetch_failed" ||
      (blockOnRemoteDivergence &&
        branch.relation === "remote_moved_and_local_missing_it"),
  );
  const recommendedBranch =
    branches.find((branch) => branch.isCurrentHead) ?? branches[0] ?? null;
  let outgoingShas = [];
  let shaBranches = new Map();
  let outgoingError = "";
  try {
    const outgoing = collectOutgoingByUpdate({
      updates,
      remoteName,
      gitExec,
    });
    outgoingShas = outgoing.orderedShas;
    shaBranches = outgoing.shaBranches;
  } catch (error) {
    outgoingError = String(error?.message ?? error);
  }
  const pushedBranchNames = branches.map((branch) => branch.branchName);
  const pushTipShas = branches.map((branch) => branch.localSha);
  for (const branch of branches) {
    const branchesForSha = shaBranches.get(branch.localSha) ?? new Set();
    branchesForSha.add(branch.branchName);
    shaBranches.set(branch.localSha, branchesForSha);
  }

  return {
    remoteName,
    headBranchName,
    headSha,
    refUpdates: updates,
    hasBranchUpdates: branches.length > 0,
    hasNonBranchObjectUpdates,
    recommendedBaseSha: recommendedBranch?.canonicalBaseSha ?? "",
    status: blocked.length > 0 ? "blocked" : "ok",
    outgoingShas,
    outgoingError,
    pushTipShas,
    pushedBranchNames,
    shaBranches: Object.fromEntries(
      [...shaBranches.entries()].map(([sha, branchSet]) => [
        sha,
        [...branchSet],
      ]),
    ),
    branches,
  };
}
