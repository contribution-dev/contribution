import test from "node:test";
import assert from "node:assert/strict";
import {
  buildChangedCommands,
  buildFullCommands,
} from "./run-changed-checks.mjs";

function hasCommand(commands, command, args) {
  const expectedArgs = JSON.stringify(args);
  return commands.some(
    ([candidateCommand, candidateArgs]) =>
      candidateCommand === command &&
      JSON.stringify(candidateArgs) === expectedArgs,
  );
}

test("full test plans include Node script regression tests", () => {
  assert.deepEqual(buildFullCommands("test"), [
    ["pnpm", ["test:scripts"]],
    ["go", ["test", "./..."]],
  ]);

  assert.equal(
    hasCommand(buildFullCommands("all"), "pnpm", ["test:scripts"]),
    true,
  );
});

test("changed tooling test plans include Node script regression tests", () => {
  assert.deepEqual(
    buildChangedCommands("test", ["scripts/example.mjs"], {
      tooling: true,
      goRelevant: false,
      rootConfig: false,
    }),
    [["pnpm", ["test:scripts"]]],
  );
});
