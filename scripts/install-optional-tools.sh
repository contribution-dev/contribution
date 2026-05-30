#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# shellcheck disable=SC1091
source "$SCRIPT_DIR/codex-env.sh"

SEMGREP_VERSION="${SEMGREP_VERSION:-1.164.0}"
GITLEAKS_VERSION="${GITLEAKS_VERSION:-8.28.0}"
OSV_SCANNER_VERSION="${OSV_SCANNER_VERSION:-2.3.8}"
TRIVY_VERSION="${TRIVY_VERSION:-0.70.0}"

TOOLS_DIR="$REPO_ROOT/.tools"
BIN_DIR="$TOOLS_DIR/bin"
MODE="install"

usage() {
  cat <<EOF
Usage: scripts/install-optional-tools.sh [--check|--install]

Installs or verifies repo-local optional analyzer tools:
  semgrep       $SEMGREP_VERSION
  gitleaks      $GITLEAKS_VERSION
  osv-scanner   $OSV_SCANNER_VERSION
  trivy         $TRIVY_VERSION
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --check | --dry-run)
      MODE="check"
      ;;
    --install)
      MODE="install"
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      printf 'Unknown argument: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

mkdir -p "$BIN_DIR"

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

version_line() {
  "$@" 2>/dev/null | sed -n '1p' || true
}

tool_has_version() {
  local tool_name="$1"
  local expected="$2"
  shift 2
  if ! command -v "$tool_name" >/dev/null 2>&1; then
    return 1
  fi
  local output
  output="$(version_line "$@")"
  [ -n "$output" ] && printf '%s\n' "$output" | grep -F "$expected" >/dev/null 2>&1
}

download() {
  local url="$1"
  local output="$2"
  curl -fsSL "$url" -o "$output"
}

platform_os() {
  case "$(uname -s)" in
    Darwin) printf 'darwin' ;;
    Linux) printf 'linux' ;;
    *) return 1 ;;
  esac
}

platform_arch_gitleaks() {
  case "$(uname -m)" in
    arm64 | aarch64) printf 'arm64' ;;
    x86_64 | amd64) printf 'x64' ;;
    *) return 1 ;;
  esac
}

platform_os_trivy() {
  case "$(uname -s)" in
    Darwin) printf 'macOS' ;;
    Linux) printf 'Linux' ;;
    *) return 1 ;;
  esac
}

platform_arch_trivy() {
  case "$(uname -m)" in
    arm64 | aarch64) printf 'ARM64' ;;
    x86_64 | amd64) printf '64bit' ;;
    *) return 1 ;;
  esac
}

install_semgrep() {
  local venv="$TOOLS_DIR/semgrep-venv"
  python3 -m venv "$venv"
  "$venv/bin/python" -m pip install --upgrade pip >/dev/null
  "$venv/bin/python" -m pip install "semgrep==$SEMGREP_VERSION"
  ln -sf "../semgrep-venv/bin/semgrep" "$BIN_DIR/semgrep"
}

install_gitleaks() {
  local os_name arch archive tmpdir
  os_name="$(platform_os)"
  arch="$(platform_arch_gitleaks)"
  archive="gitleaks_${GITLEAKS_VERSION}_${os_name}_${arch}.tar.gz"
  tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/contribution-gitleaks.XXXXXX")"
  download "https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/${archive}" "$tmpdir/$archive"
  tar -xzf "$tmpdir/$archive" -C "$tmpdir" gitleaks
  cp "$tmpdir/gitleaks" "$BIN_DIR/gitleaks"
  chmod 0755 "$BIN_DIR/gitleaks"
  rm -rf "$tmpdir"
}

install_osv_scanner() {
  GOBIN="$BIN_DIR" go install "github.com/google/osv-scanner/v2/cmd/osv-scanner@v${OSV_SCANNER_VERSION}"
}

install_trivy() {
  local os_name arch archive tmpdir
  os_name="$(platform_os_trivy)"
  arch="$(platform_arch_trivy)"
  archive="trivy_${TRIVY_VERSION}_${os_name}-${arch}.tar.gz"
  tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/contribution-trivy.XXXXXX")"
  download "https://github.com/aquasecurity/trivy/releases/download/v${TRIVY_VERSION}/${archive}" "$tmpdir/$archive"
  tar -xzf "$tmpdir/$archive" -C "$tmpdir" trivy
  cp "$tmpdir/trivy" "$BIN_DIR/trivy"
  chmod 0755 "$BIN_DIR/trivy"
  rm -rf "$tmpdir"
}

check_tools() {
  if tool_has_version "semgrep" "$SEMGREP_VERSION" semgrep --version; then
    pass "semgrep $SEMGREP_VERSION"
  else
    fail "semgrep $SEMGREP_VERSION missing; run pnpm tools:install:optional"
  fi

  if tool_has_version "gitleaks" "$GITLEAKS_VERSION" gitleaks version; then
    pass "gitleaks $GITLEAKS_VERSION"
  elif command -v gitleaks >/dev/null 2>&1; then
    fail "gitleaks is available but not a pinned release binary; rerun pnpm tools:install:optional"
  else
    fail "gitleaks $GITLEAKS_VERSION missing; run pnpm tools:install:optional"
  fi

  if tool_has_version "osv-scanner" "$OSV_SCANNER_VERSION" osv-scanner --version; then
    pass "osv-scanner $OSV_SCANNER_VERSION"
  else
    fail "osv-scanner $OSV_SCANNER_VERSION missing; run pnpm tools:install:optional"
  fi

  if tool_has_version "trivy" "$TRIVY_VERSION" trivy --version; then
    pass "trivy $TRIVY_VERSION"
  else
    fail "trivy $TRIVY_VERSION missing; run pnpm tools:install:optional"
  fi
}

if [ "$MODE" = "check" ]; then
  check_tools
  exit "$status"
fi

printf 'Installing optional analyzers into %s\n' "$BIN_DIR"
install_semgrep
install_gitleaks
install_osv_scanner
install_trivy
check_tools

printf '\nOptional analyzer install step finished. Run `pnpm tools:preflight` to verify availability.\n'
