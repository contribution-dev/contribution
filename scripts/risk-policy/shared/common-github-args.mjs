import path from "node:path";

export function consumeCommonGithubArg(
  args,
  arg,
  next,
  defaultContractPath = "",
) {
  switch (arg) {
    case "--owner":
      args.owner = next ?? "";
      return 1;
    case "--repo":
      args.repo = next ?? "";
      return 1;
    case "--pr-number":
      args.pullNumber = Number.parseInt(next ?? "0", 10);
      return 1;
    case "--token":
      args.token = next ?? "";
      return 1;
    case "--head-sha":
      args.headSha = next ?? "";
      return 1;
    case "--contract":
      args.contractPath = path.resolve(next ?? defaultContractPath);
      return 1;
    default:
      return 0;
  }
}
