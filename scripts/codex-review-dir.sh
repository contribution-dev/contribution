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

resolve_codex_reviews_dir() {
  repo_root="$1"
  preferred_dir="$2"

  if [ -z "$preferred_dir" ]; then
    preferred_dir="${CODE_REVIEW_DIR:-$(default_code_reviews_dir "$repo_root")}"
  fi

  preferred_dir="$(normalize_review_path "$preferred_dir")"

  canonical_dir="$(normalize_review_path "$(default_code_reviews_dir "$repo_root")")"
  case "$preferred_dir" in
    "$canonical_dir"/logs|"${canonical_dir}"/logs/*)
      collapsed_candidate="${preferred_dir#"$canonical_dir"/}"
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
            preferred_dir="$canonical_dir"
          fi
          ;;
      esac
      ;;
  esac

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
