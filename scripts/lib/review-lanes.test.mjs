import assert from "node:assert/strict";
import test from "node:test";

import { REVIEW_QUEUE_LANES } from "./codex-review-state.mjs";

test("review queue lanes stay Codex-only", () => {
  assert.deepEqual(REVIEW_QUEUE_LANES, ["codex"]);
});
