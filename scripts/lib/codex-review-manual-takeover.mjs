import { readFileSync } from "node:fs";
import path from "node:path";

function normalizeAutomationConfig(value) {
  return {
    paused: value?.paused === true,
    pauseReason: String(value?.pause_reason ?? value?.pauseReason ?? "").trim(),
    lastOpenedSha: String(value?.last_opened_sha ?? value?.lastOpenedSha ?? "")
      .trim()
      .toLowerCase(),
  };
}

function readAutomationConfig(reviewsDir) {
  try {
    const raw = readFileSync(
      path.join(reviewsDir, "backlog-remediation", "automation.json"),
      "utf8",
    );
    return normalizeAutomationConfig(JSON.parse(raw));
  } catch {
    return normalizeAutomationConfig(null);
  }
}

export function readTerminalManualTakeoverSha(reviewsDir) {
  const config = readAutomationConfig(reviewsDir);
  if (!config.paused || config.pauseReason !== "manual_takeover") {
    return "";
  }
  return config.lastOpenedSha;
}

export function isTerminalManualTakeoverSha(reviewsDir, sha) {
  const normalizedSha = String(sha ?? "")
    .trim()
    .toLowerCase();
  return (
    normalizedSha !== "" &&
    readTerminalManualTakeoverSha(reviewsDir) === normalizedSha
  );
}
