import test from "node:test";
import assert from "node:assert/strict";
import {
  getContractCoverageValidationCommands,
  getContractFastValidationCommands,
  hasContractSensitiveDomainChanges,
} from "./contract-sensitive-domains.mjs";

test("contract validation commands can read changed files from a file list", () => {
  const commands = getContractFastValidationCommands(["internal/cli/root.go"], {
    filesFrom: "/tmp/files.txt",
  });
  const coverageCommand = commands.find(
    (command) =>
      command.cmd === "node" &&
      command.args.includes("scripts/check-cli-contract-coverage.mjs"),
  );

  assert.deepEqual(coverageCommand.args, [
    "scripts/check-cli-contract-coverage.mjs",
    "--files-from",
    "/tmp/files.txt",
  ]);
});

test("docs-only contract coverage commands can read from a file list", () => {
  assert.deepEqual(
    getContractCoverageValidationCommands(["docs/cli-contract.md"], {
      filesFrom: "/tmp/files.txt",
    }),
    [
      {
        cmd: "node",
        args: [
          "scripts/check-cli-contract-coverage.mjs",
          "--files-from",
          "/tmp/files.txt",
        ],
      },
    ],
  );
});

test("CLI contract matching covers all command behavior packages", () => {
  for (const file of [
    "internal/preflight/preflight.go",
    "internal/coverage/parser.go",
    "internal/friend/friend.go",
    "internal/fileclass/classify.go",
    "internal/receipt/scoring.go",
  ]) {
    assert.equal(
      hasContractSensitiveDomainChanges([file], "cli-contract"),
      true,
    );
  }
});
