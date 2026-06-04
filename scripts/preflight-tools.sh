#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# shellcheck disable=SC1091
source "$SCRIPT_DIR/codex-env.sh"

status=0

pass() {
  printf 'PASS %s\n' "$1"
}

warn() {
  printf 'WARN %s\n' "$1"
}

fail() {
  printf 'FAIL %s\n' "$1"
  status=1
}

check_required_tool() {
  local tool_name="$1"
  local install_hint="$2"

  if command -v "$tool_name" >/dev/null 2>&1; then
    pass "$tool_name found at $(command -v "$tool_name")"
  else
    fail "$tool_name is missing. $install_hint"
  fi
}

extract_semver() {
  local raw="$1"
  local version=""
  if [[ "$raw" =~ go([0-9]+(\.[0-9]+){1,2}) ]]; then
    version="${BASH_REMATCH[1]}"
  elif [[ "$raw" =~ v?([0-9]+(\.[0-9]+){1,2}) ]]; then
    version="${BASH_REMATCH[1]}"
  fi
  printf '%s\n' "$version"
}

semver_at_least() {
  local current="$1"
  local required="$2"
  local current_major current_minor current_patch
  local required_major required_minor required_patch

  IFS=. read -r current_major current_minor current_patch _ <<<"$current"
  IFS=. read -r required_major required_minor required_patch _ <<<"$required"
  current_patch="${current_patch:-0}"
  required_patch="${required_patch:-0}"

  [[ "$current_major" =~ ^[0-9]+$ && "$current_minor" =~ ^[0-9]+$ && "$current_patch" =~ ^[0-9]+$ ]] || return 1
  [[ "$required_major" =~ ^[0-9]+$ && "$required_minor" =~ ^[0-9]+$ && "$required_patch" =~ ^[0-9]+$ ]] || return 1

  if ((current_major != required_major)); then
    ((current_major > required_major))
    return
  fi
  if ((current_minor != required_minor)); then
    ((current_minor > required_minor))
    return
  fi
  ((current_patch >= required_patch))
}

semver_major_below() {
  local current="$1"
  local max_major="$2"
  local current_major

  IFS=. read -r current_major _ <<<"$current"
  [[ "$current_major" =~ ^[0-9]+$ && "$max_major" =~ ^[0-9]+$ ]] || return 1
  ((current_major < max_major))
}

check_required_version() {
  local tool_name="$1"
  local install_hint="$2"
  local min_version="$3"
  local max_major="$4"
  shift 4

  if ! command -v "$tool_name" >/dev/null 2>&1; then
    fail "$tool_name is missing. $install_hint"
    return
  fi

  local output first_line version range
  if ! output="$("$@" 2>&1)"; then
    fail "$tool_name version check failed. $install_hint"
    return
  fi

  first_line="${output%%$'\n'*}"
  version="$(extract_semver "$first_line")"
  range=">= $min_version"
  if [ -n "$max_major" ]; then
    range="$range < $max_major"
  fi

  if [ -z "$version" ] || ! semver_at_least "$version" "$min_version"; then
    fail "$tool_name $version does not satisfy $range. $install_hint"
    return
  fi

  if [ -n "$max_major" ] && ! semver_major_below "$version" "$max_major"; then
    fail "$tool_name $version does not satisfy $range. $install_hint"
    return
  fi

  pass "$tool_name $version satisfies $range"
}

check_optional_command() {
  local label="$1"
  local hint="$2"
  shift 2

  if "$@" >/dev/null 2>&1; then
    pass "$label is ready"
  else
    warn "$label not ready. $hint"
  fi
}

launchctl_status_count() {
  local launchctl_status="$1"
  local lane="$2"
  local field="$3"
  printf '%s\n' "$launchctl_status" | awk -v lane="$lane" -v field="$field" '
    index($0, "[codex-review-launchctl]") == 1 && $0 ~ ("lane=" lane " ") {
      for (field_index = 1; field_index <= NF; field_index += 1) {
        if ($field_index == field "=1") {
          count += 1
        }
      }
    }
    END {
      print count + 0
    }
  '
}

check_durable_review_workers() {
  if [ "$(uname -s)" != "Darwin" ]; then
    return
  fi

  local expected_codex_workers=2
  local launchctl_status=""
  if ! launchctl_status="$("$REPO_ROOT/scripts/codex-review-launchctl" status --lane all 2>/dev/null)"; then
    fail "durable review worker status failed. Run 'pnpm review:status' after fixing launchctl access."
    return
  fi

  local codex_installed
  codex_installed="$(launchctl_status_count "$launchctl_status" "codex" "installed")"
  local codex_running
  codex_running="$(launchctl_status_count "$launchctl_status" "codex" "running")"
  local remediation_running
  remediation_running="$(launchctl_status_count "$launchctl_status" "remediation" "running")"
  local watchdog_running
  watchdog_running="$(launchctl_status_count "$launchctl_status" "watchdog" "running")"

  if [ "$codex_installed" -lt "$expected_codex_workers" ] || [ "$codex_running" -lt "$expected_codex_workers" ] || [ "$remediation_running" -lt 1 ] || [ "$watchdog_running" -lt 1 ]; then
    fail "durable review workers missing or stopped. Run 'pnpm review:recover' to install or repair them."
    return
  fi

  pass "durable review workers installed"
}

echo "Preflight: checking local agent tooling in $REPO_ROOT"
echo

check_required_version "node" "Install Node.js 24.16.0." "24.16.0" "25" node --version
check_required_version "pnpm" "Install pnpm >= 11.4.0." "11.4.0" "" pnpm --version
check_required_version "go" "Install Go 1.26.4." "1.26.4" "" go version
check_required_tool "git" "Install Git."
check_optional_command "golangci-lint" "Install golangci-lint for local lint parity." golangci-lint --version
check_optional_command "govulncheck" "Install govulncheck with 'go install golang.org/x/vuln/cmd/govulncheck@latest'." govulncheck -version
check_optional_command "scc" "Install scc for richer language inventory." scc --version
check_optional_command "gh CLI" "Install GitHub CLI for PR/check automation." gh --version
check_optional_command "gh auth" "Run 'gh auth login' to enable GitHub operations." gh auth status
check_optional_command "semgrep" "Run 'pnpm tools:install:optional' for repo-local analyzer tools." semgrep --version
check_optional_command "gitleaks" "Run 'pnpm tools:install:optional' for repo-local analyzer tools." gitleaks version
check_optional_command "osv-scanner" "Run 'pnpm tools:install:optional' for repo-local analyzer tools." osv-scanner --version
check_optional_command "trivy" "Run 'pnpm tools:install:optional' for repo-local analyzer tools." trivy --version
check_optional_command "codex CLI" "Install Codex CLI." codex --version
check_optional_command "codex auth" "Run 'codex login' for Codex review workflows." codex login status
check_durable_review_workers

echo
if [ "$status" -ne 0 ]; then
  cat <<EOF
Tool preflight failed.

To keep tooling available in every shell session, add this to ~/.zshrc:
  source $REPO_ROOT/scripts/codex-env.sh

Then rerun:
  pnpm tools:check
EOF
  exit "$status"
fi

cat <<EOF
Tool preflight passed.

Recommended shell bootstrap:
  source $REPO_ROOT/scripts/codex-env.sh
EOF
