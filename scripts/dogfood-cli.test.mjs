import assert from "node:assert/strict";
import test from "node:test";
import { parseArgs } from "./dogfood-cli.mjs";

test("dogfood parser accepts pnpm argument separator", () => {
  assert.deepEqual(parseArgs(["--mode", "real", "--", "--keep-temp"]), {
    mode: "real",
    binary: "",
    keepTemp: true,
    skipBuild: false,
  });
});
