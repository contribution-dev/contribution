import assert from "node:assert/strict";
import test from "node:test";

import { parseArgs } from "./codex-review-queue-backlog";

test("queue backlog defaults to read-only status", () => {
  assert.deepEqual(parseArgs(["node", "script"]), {
    freezeExistingPending: false,
    enqueueAfter: "",
    reason: "",
    clear: false,
    status: true,
    lane: "codex",
    reviewsDir: "",
  });
  assert.equal(parseArgs(["node", "script", "--status"]).status, true);
});

test("queue backlog mutating options remain explicit", () => {
  const freeze = parseArgs(["node", "script", "--freeze-existing-pending"]);
  assert.equal(freeze.freezeExistingPending, true);
  assert.equal(freeze.status, false);

  const enqueue = parseArgs([
    "node",
    "script",
    "--enqueue-after",
    "2026-01-01T00:00:00.000Z",
    "--reason",
    "audit",
  ]);
  assert.equal(enqueue.enqueueAfter, "2026-01-01T00:00:00.000Z");
  assert.equal(enqueue.reason, "audit");
  assert.equal(enqueue.status, false);

  const clear = parseArgs(["node", "script", "--clear"]);
  assert.equal(clear.clear, true);
  assert.equal(clear.status, false);
});

test("queue backlog status rejects mutating combinations", () => {
  assert.throws(
    () => parseArgs(["node", "script", "--status", "--clear"]),
    /--status cannot be combined with mutating options/,
  );
  assert.throws(
    () =>
      parseArgs(["node", "script", "--status", "--freeze-existing-pending"]),
    /--status cannot be combined with mutating options/,
  );
  assert.throws(
    () =>
      parseArgs([
        "node",
        "script",
        "--status",
        "--enqueue-after",
        "2026-01-01T00:00:00.000Z",
      ]),
    /--status cannot be combined with mutating options/,
  );
  assert.throws(
    () => parseArgs(["node", "script", "--reason", "audit"]),
    /--reason requires --freeze-existing-pending or --enqueue-after/,
  );
});
