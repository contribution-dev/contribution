import crypto from "node:crypto";
import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import path from "node:path";

export async function readJsonFile(filePath, readFileFn = readFile) {
  const raw = await readFileFn(filePath, "utf8");
  return JSON.parse(raw);
}

export async function writeJsonFileAtomic(
  filePath,
  payload,
  {
    mkdirFn = mkdir,
    renameFn = rename,
    writeFileFn = writeFile,
    tempPrefix = "json-state",
  } = {},
) {
  const directory = path.dirname(filePath);
  await mkdirFn(directory, { recursive: true, mode: 0o700 });
  const tempPath = path.join(
    directory,
    `.${tempPrefix}.${process.pid}.${Date.now()}.${crypto
      .randomBytes(4)
      .toString("hex")}.tmp`,
  );
  await writeFileFn(tempPath, `${JSON.stringify(payload, null, 2)}\n`, {
    encoding: "utf8",
    mode: 0o600,
  });
  await renameFn(tempPath, filePath);
}
