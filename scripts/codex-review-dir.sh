#!/bin/sh

default_code_reviews_dir() {
  repo_root="$1"
  printf '%s\n' "$repo_root/.code-reviews"
}

normalize_review_path() {
  target_path="$1"

  if command -v python3 >/dev/null 2>&1; then
    normalized_path="$(python3 - "$target_path" <<'PY'
import os
import sys

target = sys.argv[1]
absolute = os.path.abspath(target)
parent = os.path.dirname(absolute) or os.curdir
base = os.path.basename(absolute)
print(os.path.join(os.path.realpath(parent), base))
PY
)" && {
      printf '%s\n' "$normalized_path"
      return 0
    }
  fi

  if command -v node >/dev/null 2>&1; then
    normalized_path="$(node -e '
const fs = require("fs");
const path = require("path");
const absolute = path.resolve(process.argv[1]);
const parent = path.dirname(absolute);
const base = path.basename(absolute);
let normalizedParent;
try {
  normalizedParent = fs.realpathSync.native(parent);
} catch {
  normalizedParent = parent;
}
console.log(path.join(normalizedParent, base));
' "$target_path")" && {
      printf '%s\n' "$normalized_path"
      return 0
    }
  fi

  case "$target_path" in
    /*)
      absolute_path="$target_path"
      ;;
    *)
      absolute_path="$(pwd)/$target_path"
      ;;
  esac
  parent_dir="$(dirname "$absolute_path")"
  base_name="$(basename "$absolute_path")"
  if cd "$parent_dir" 2>/dev/null; then
    printf '%s/%s\n' "$(pwd -P)" "$base_name"
    return 0
  fi
  printf '%s\n' "$absolute_path"
}

legacy_codex_reviews_dir() {
  repo_root="$1"
  printf '%s\n' "$repo_root/.codex/reviews"
}

ack_lock_path() {
  printf '%s.lock\n' "$1"
}

ack_lock_stale_after_seconds() {
  printf '30\n'
}

ack_lock_missing_start_time_evict_after_seconds() {
  printf '900\n'
}

ack_lock_now_epoch() {
  date +%s 2>/dev/null || printf '0\n'
}

ack_lock_start_time() {
  ack_lock_start_time_for_pid "$$"
}

ack_lock_start_time_for_pid() {
  pid="$1"
  if command -v ps >/dev/null 2>&1; then
    ps -o lstart= -p "$pid" 2>/dev/null | tr -d '\n'
  fi
}

ack_lock_mtime_epoch() {
  lock_path="$1"
  if stat -f '%m' "$lock_path" >/dev/null 2>&1; then
    stat -f '%m' "$lock_path" 2>/dev/null
    return 0
  fi
  if stat -c '%Y' "$lock_path" >/dev/null 2>&1; then
    stat -c '%Y' "$lock_path" 2>/dev/null
    return 0
  fi
  return 1
}

ack_lock_owner_pid() {
  owner_path="$1"
  sed -n 's/.*"pid":[[:space:]]*\([0-9][0-9]*\).*/\1/p' "$owner_path" 2>/dev/null | head -n 1
}

ack_lock_owner_token() {
  owner_path="$1"
  sed -n 's/.*"token":"\([^"]*\)".*/\1/p' "$owner_path" 2>/dev/null | head -n 1
}

ack_lock_owner_start_time() {
  owner_path="$1"
  sed -n 's/.*"start_time":"\([^"]*\)".*/\1/p' "$owner_path" 2>/dev/null | head -n 1
}

ack_lock_token_marker_path() {
  lock_path="$1"
  owner_token="$2"
  printf '%s/.owner-token-%s\n' "$lock_path" "$owner_token"
}

ack_lock_owner_alive() {
  owner_pid="$1"
  if [ -z "$owner_pid" ]; then
    return 1
  fi
  kill -0 "$owner_pid" 2>/dev/null
}

ack_lock_is_stale() {
  lock_path="$1"
  mtime_epoch="$(ack_lock_mtime_epoch "$lock_path")" || return 1
  now_epoch="$(ack_lock_now_epoch)"
  age_seconds=$((now_epoch - mtime_epoch))
  stale_after_seconds="$(ack_lock_stale_after_seconds)"
  if [ "$age_seconds" -le "$stale_after_seconds" ]; then
    return 1
  fi

  owner_path="$lock_path/owner.json"
  owner_pid="$(ack_lock_owner_pid "$owner_path")"
  owner_start_time="$(ack_lock_owner_start_time "$owner_path")"

  if ! ack_lock_owner_alive "$owner_pid"; then
    return 0
  fi

  if [ -n "$owner_start_time" ]; then
    live_start_time="$(ack_lock_start_time_for_pid "$owner_pid")"
    if [ -n "$live_start_time" ] && [ "$live_start_time" != "$owner_start_time" ]; then
      return 0
    fi
    return 1
  fi

  missing_start_time_evict_after_seconds="$(ack_lock_missing_start_time_evict_after_seconds)"
  [ "$age_seconds" -gt "$missing_start_time_evict_after_seconds" ]
}

evict_stale_ack_lock() {
  lock_path="$1"
  owner_path="$lock_path/owner.json"
  claimed_lock_path="$lock_path.stale.$$"
  stale_token="$(ack_lock_owner_token "$owner_path")"
  if [ -z "$stale_token" ]; then
    if ! mv "$lock_path" "$claimed_lock_path" 2>/dev/null; then
      return 1
    fi
    rm -f "$claimed_lock_path/owner.json" "$claimed_lock_path"/.owner-token-* 2>/dev/null || true
    rmdir "$claimed_lock_path" 2>/dev/null
    return $?
  fi

  marker_path="$(ack_lock_token_marker_path "$lock_path" "$stale_token")"
  claimed_marker_path="$marker_path.stale.$$"

  # Claim the specific stale owner before removing anything under the lock path.
  if ! mv "$marker_path" "$claimed_marker_path" 2>/dev/null; then
    current_token="$(ack_lock_owner_token "$owner_path")"
    if [ "$current_token" != "$stale_token" ]; then
      return 1
    fi
    if ! mv "$lock_path" "$claimed_lock_path" 2>/dev/null; then
      return 1
    fi
    rm -f "$claimed_lock_path/owner.json" "$claimed_lock_path"/.owner-token-* 2>/dev/null || true
    rmdir "$claimed_lock_path" 2>/dev/null
    return $?
  fi

  rm -f "$owner_path" "$claimed_marker_path" 2>/dev/null || true
  rmdir "$lock_path" 2>/dev/null
}

acquire_ack_lock() {
  ack_path="$1"
  lock_path="$(ack_lock_path "$ack_path")"
  attempts=0
  while ! mkdir "$lock_path" 2>/dev/null; do
    if ack_lock_is_stale "$lock_path"; then
      if evict_stale_ack_lock "$lock_path" >/dev/null 2>&1; then
        continue
      fi
    fi

    attempts=$((attempts + 1))
    if [ "$attempts" -ge 125 ]; then
      return 1
    fi
    sleep 0.04
  done

  owner_path="$lock_path/owner.json"
  owner_token="$$.$(date +%s 2>/dev/null || printf '0')"
  marker_path="$(ack_lock_token_marker_path "$lock_path" "$owner_token")"
  owner_start_time="$(ack_lock_start_time)"
  umask 077
  if ! : >"$marker_path" || ! cat >"$owner_path" <<EOF
{"pid":$$,"token":"$owner_token","created_at":"","start_time":"$owner_start_time"}
EOF
  then
    rm -f "$marker_path" "$owner_path" 2>/dev/null || true
    rmdir "$lock_path" 2>/dev/null || true
    return 1
  fi
  printf '%s\n' "$owner_token"
}

release_ack_lock() {
  ack_path="$1"
  expected_token="$2"
  lock_path="$(ack_lock_path "$ack_path")"
  current_token="$(ack_lock_owner_token "$lock_path/owner.json")"
  if [ -z "$expected_token" ] || [ "$current_token" != "$expected_token" ]; then
    return 0
  fi
  rm -f "$(ack_lock_token_marker_path "$lock_path" "$expected_token")" "$lock_path/owner.json" 2>/dev/null || true
  rmdir "$lock_path" 2>/dev/null || true
}

merge_ack_files() {
  source_ack="$1"
  target_ack="$2"
  first_lock="$source_ack"
  second_lock="$target_ack"
  if [ "$target_ack" \< "$source_ack" ]; then
    first_lock="$target_ack"
    second_lock="$source_ack"
  fi

  first_token="$(acquire_ack_lock "$first_lock")" || return 1
  if ! second_token="$(acquire_ack_lock "$second_lock")"; then
    release_ack_lock "$first_lock" "$first_token"
    return 1
  fi

  if command -v python3 >/dev/null 2>&1; then
    python3 - "$source_ack" "$target_ack" <<'PY' >/dev/null 2>&1
import json
import os
import sys

source_path = sys.argv[1]
target_path = sys.argv[2]

def normalize(payload):
    if not isinstance(payload, dict):
        payload = {}
    raw_acks = payload.get("acks")
    normalized = {}
    if isinstance(raw_acks, dict):
        for sha, value in raw_acks.items():
            if not isinstance(value, dict):
                continue
            version_token = str(value.get("version_token", ""))
            if not version_token:
                continue
            normalized[str(sha)] = {
                "version_token": version_token,
                "acked_at": str(value.get("acked_at", "")),
                "reason": str(value.get("reason", "")),
                "source": str(value.get("source", "")),
            }
    return {
        "schema_version": 1,
        "updated_at": str(payload.get("updated_at", "")),
        "acks": normalized,
    }

def load(path):
    try:
        with open(path, "r", encoding="utf-8") as handle:
            return normalize(json.load(handle))
    except Exception:
        return normalize({})

def ack_key(entry):
    return (
        str(entry.get("acked_at", "")),
        str(entry.get("version_token", "")),
        str(entry.get("source", "")),
        str(entry.get("reason", "")),
    )

source_state = load(source_path)
target_state = load(target_path)
merged_acks = dict(target_state["acks"])
for sha, value in source_state["acks"].items():
    current = merged_acks.get(sha)
    if current is None or ack_key(value) > ack_key(current):
        merged_acks[sha] = value

updated_at = max(source_state["updated_at"], target_state["updated_at"])
payload = {
    "schema_version": 1,
    "updated_at": updated_at,
    "acks": merged_acks,
}
temp_path = f"{target_path}.tmp.{os.getpid()}"
with open(temp_path, "w", encoding="utf-8") as handle:
    json.dump(payload, handle, indent=2)
    handle.write("\n")
os.replace(temp_path, target_path)
try:
    os.remove(source_path)
except FileNotFoundError:
    pass
PY
    status=$?
    release_ack_lock "$second_lock" "$second_token"
    release_ack_lock "$first_lock" "$first_token"
    [ "$status" -eq 0 ] || return 1
    return 0
  fi

  if command -v node >/dev/null 2>&1; then
    node --input-type=module - "$source_ack" "$target_ack" <<'JS' >/dev/null 2>&1
import { readFileSync, rmSync, writeFileSync } from "node:fs";

const sourcePath = process.argv[2];
const targetPath = process.argv[3];

function normalize(payload) {
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) {
    payload = {};
  }
  const acks = {};
  const rawAcks = payload.acks;
  if (rawAcks && typeof rawAcks === "object" && !Array.isArray(rawAcks)) {
    for (const [sha, value] of Object.entries(rawAcks)) {
      if (!value || typeof value !== "object" || Array.isArray(value)) continue;
      const versionToken = String(value.version_token ?? "");
      if (!versionToken) continue;
      acks[String(sha)] = {
        version_token: versionToken,
        acked_at: String(value.acked_at ?? ""),
        reason: String(value.reason ?? ""),
        source: String(value.source ?? ""),
      };
    }
  }
  return {
    schema_version: 1,
    updated_at: String(payload.updated_at ?? ""),
    acks,
  };
}

function load(filePath) {
  try {
    return normalize(JSON.parse(readFileSync(filePath, "utf8")));
  } catch {
    return normalize({});
  }
}

function compare(left, right) {
  return [
    String(left?.acked_at ?? ""),
    String(left?.version_token ?? ""),
    String(left?.source ?? ""),
    String(left?.reason ?? ""),
  ].join("\u0000").localeCompare(
    [
      String(right?.acked_at ?? ""),
      String(right?.version_token ?? ""),
      String(right?.source ?? ""),
      String(right?.reason ?? ""),
    ].join("\u0000"),
  );
}

const sourceState = load(sourcePath);
const targetState = load(targetPath);
const mergedAcks = { ...targetState.acks };
for (const [sha, value] of Object.entries(sourceState.acks)) {
  const current = mergedAcks[sha];
  if (!current || compare(value, current) > 0) {
    mergedAcks[sha] = value;
  }
}

const updatedAt =
  sourceState.updated_at.localeCompare(targetState.updated_at) > 0
    ? sourceState.updated_at
    : targetState.updated_at;
writeFileSync(
  targetPath,
  `${JSON.stringify(
    {
      schema_version: 1,
      updated_at: updatedAt,
      acks: mergedAcks,
    },
    null,
    2,
  )}\n`,
  { encoding: "utf8", mode: 0o600 },
);
rmSync(sourcePath, { force: true });
JS
    status=$?
    release_ack_lock "$second_lock" "$second_token"
    release_ack_lock "$first_lock" "$first_token"
    [ "$status" -eq 0 ] || return 1
    return 0
  fi

  release_ack_lock "$second_lock" "$second_token"
  release_ack_lock "$first_lock" "$first_token"
  return 1
}

merge_legacy_reviews_dir() {
  legacy_dir="$1"
  preferred_dir="$2"

  if [ ! -d "$legacy_dir" ] || [ ! -d "$preferred_dir" ]; then
    return 0
  fi

  mkdir -p "$preferred_dir" 2>/dev/null || true
  for source_path in "$legacy_dir"/* "$legacy_dir"/.[!.]* "$legacy_dir"/..?*; do
    if [ ! -e "$source_path" ]; then
      continue
    fi
    target_path="$preferred_dir/$(basename "$source_path")"
    if [ ! -e "$target_path" ]; then
      mv "$source_path" "$target_path" 2>/dev/null || true
      continue
    fi
    if [ "$(basename "$source_path")" = ".ack.json" ] && [ -f "$source_path" ] && [ -f "$target_path" ]; then
      merge_ack_files "$source_path" "$target_path" || true
      continue
    fi
    if [ -d "$source_path" ] && [ -d "$target_path" ]; then
      merge_legacy_reviews_dir "$source_path" "$target_path"
      rmdir "$source_path" 2>/dev/null || true
    fi
  done
  rmdir "$legacy_dir" 2>/dev/null || true
}

resolve_codex_reviews_dir() {
  repo_root="$1"
  preferred_dir="$2"

  if [ -z "$preferred_dir" ]; then
    preferred_dir="${CODE_REVIEW_DIR:-${CODEX_REVIEW_DIR:-$(default_code_reviews_dir "$repo_root")}}"
  fi

  preferred_dir="$(normalize_review_path "$preferred_dir")"

  canonical_dir="$(normalize_review_path "$(default_code_reviews_dir "$repo_root")")"
  legacy_dir="$(normalize_review_path "$(legacy_codex_reviews_dir "$repo_root")")"
  for root_dir in "$canonical_dir" "$legacy_dir"; do
    case "$preferred_dir" in
      "$root_dir"/logs|"${root_dir}"/logs/*)
        collapsed_candidate="${preferred_dir#"$root_dir"/}"
        case "$collapsed_candidate" in
          logs|logs/*)
            collapse_ok=1
            old_ifs="${IFS:- }"
            IFS='/'
            set -- $collapsed_candidate
            IFS="$old_ifs"
            for segment in "$@"; do
              [ -z "$segment" ] && continue
              if [ "$segment" != "logs" ]; then
                collapse_ok=0
                break
              fi
            done
            if [ "$collapse_ok" -eq 1 ]; then
              preferred_dir="$root_dir"
            fi
            ;;
        esac
        ;;
    esac
  done

  if [ "$preferred_dir" != "$legacy_dir" ] && [ -d "$legacy_dir" ]; then
    if [ ! -e "$preferred_dir" ]; then
      mkdir -p "$(dirname "$preferred_dir")" 2>/dev/null || true
      mv "$legacy_dir" "$preferred_dir" 2>/dev/null || true
    else
      merge_legacy_reviews_dir "$legacy_dir" "$preferred_dir"
    fi
  fi

  if (umask 077 && mkdir -p "$preferred_dir/logs") 2>/dev/null; then
    chmod 700 "$preferred_dir" "$preferred_dir/logs" 2>/dev/null || true
    printf '%s\n' "$preferred_dir"
    return 0
  fi

  uid_value="unknown"
  if command -v id >/dev/null 2>&1; then
    uid_value="$(id -u 2>/dev/null || printf 'unknown')"
  fi

  if command -v shasum >/dev/null 2>&1; then
    repo_hash="$(printf '%s' "$repo_root" | shasum -a 256 | awk '{print $1}')"
  elif command -v sha256sum >/dev/null 2>&1; then
    repo_hash="$(printf '%s' "$repo_root" | sha256sum | awk '{print $1}')"
  else
    repo_hash="$(printf '%s' "$repo_root" | cksum | awk '{print $1}')"
  fi

  fallback_dir="/tmp/contribution-code-reviews-${uid_value}-${repo_hash}"
  if ! (umask 077 && mkdir -p "$fallback_dir/logs") 2>/dev/null; then
    return 1
  fi

  chmod 700 "$fallback_dir" "$fallback_dir/logs" 2>/dev/null || true
  printf '%s\n' "$fallback_dir"
}
