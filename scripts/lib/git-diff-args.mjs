export function validateDiffRange(value) {
  const range = String(value ?? "").trim();
  if (!range) return "";
  if (range.startsWith("-")) {
    throw new Error(
      "--diff-range must name a git revision or range, not an option",
    );
  }
  if (/[\0\s;&|`$<>()]/u.test(range)) {
    throw new Error("--diff-range contains unsupported characters");
  }
  return range;
}

export function gitChangedFilesArgs({ staged = false, diffRange = "" } = {}) {
  const args = ["diff", "--name-only", "--diff-filter=ACMR"];
  if (staged) {
    args.push("--cached");
    return args;
  }
  const range = validateDiffRange(diffRange);
  if (range) {
    args.push(range);
  }
  return args;
}
