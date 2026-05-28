import test from "node:test";
import assert from "node:assert/strict";
import { validationInstallEnv } from "./validate-and-report.mjs";

test("validation install runs safely in headless environments", () => {
  const env = validationInstallEnv({ PATH: "/bin" });

  assert.equal(env.CI, "true");
  assert.equal(env.HUSKY, "0");
  assert.equal(env.PATH, "/bin");
});

test("validation install preserves explicit CI value", () => {
  const env = validationInstallEnv({ CI: "1", HUSKY: "1" });

  assert.equal(env.CI, "1");
  assert.equal(env.HUSKY, "0");
});
