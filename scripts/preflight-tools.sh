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
    warn "durable review workers missing or stopped. Attempting automatic repair."
    "$REPO_ROOT/scripts/codex-review-launchctl" install --lane all >/dev/null 2>&1 || {
      fail "durable review worker auto-repair failed. Run 'pnpm review:recover'."
      return
    }
  fi

  pass "durable review workers installed"
}

echo "Preflight: checking local agent tooling in $REPO_ROOT"
echo

check_required_tool "node" "Install Node.js 22.x."
check_required_tool "pnpm" "Install pnpm >= 10.20.0."
check_required_tool "go" "Install Go 1.26.3."
check_required_tool "git" "Install Git."
check_optional_command "golangci-lint" "Install golangci-lint for local lint parity." golangci-lint --version
check_optional_command "govulncheck" "Install govulncheck with 'go install golang.org/x/vuln/cmd/govulncheck@latest'." govulncheck -version
check_optional_command "gh CLI" "Install GitHub CLI for PR/check automation." gh --version
check_optional_command "gh auth" "Run 'gh auth login' to enable GitHub operations." gh auth status
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
  pnpm tools:preflight
EOF
  exit "$status"
fi

cat <<EOF
Tool preflight passed.

Recommended shell bootstrap:
  source $REPO_ROOT/scripts/codex-env.sh
EOF
