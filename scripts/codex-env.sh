#!/usr/bin/env bash

if [ -n "${BASH_VERSION:-}" ]; then
  SCRIPT_SOURCE="${BASH_SOURCE[0]}"
elif [ -n "${ZSH_VERSION:-}" ]; then
  SCRIPT_SOURCE="${(%):-%N}"
else
  SCRIPT_SOURCE="$0"
fi

SCRIPT_DIR="$(cd "$(dirname "$SCRIPT_SOURCE")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

path_prepend() {
  local path_to_add="$1"

  if [ ! -d "$path_to_add" ]; then
    return
  fi

  case ":${PATH:-}:" in
    *":$path_to_add:"*) ;;
    *) export PATH="$path_to_add:${PATH:-}" ;;
  esac
}

activate_fnm_node() {
  if [ -n "${CONTRIBUTION_DISABLE_FNM:-}" ]; then
    return
  fi

  local node_version=""
  local version_file="$REPO_ROOT/.nvmrc"
  if [ -f "$version_file" ]; then
    IFS= read -r node_version <"$version_file" || true
    node_version="${node_version#v}"
    if [ -n "$node_version" ]; then
      local fnm_node_bin="${FNM_DIR:-$HOME/.local/share/fnm}/node-versions/v$node_version/installation/bin"
      if [ -d "$fnm_node_bin" ]; then
        path_prepend "$fnm_node_bin"
        return
      fi
    fi
  fi

  if ! command -v fnm >/dev/null 2>&1; then
    return
  fi

  local previous_dir
  previous_dir="$(pwd)"
  # fnm emits shell code that prepends the selected Node version to PATH.
  unset FNM_MULTISHELL_PATH
  eval "$(fnm env --shell bash --log-level quiet)"
  if cd "$REPO_ROOT" 2>/dev/null; then
    fnm use --log-level quiet >/dev/null 2>&1 || true
    cd "$previous_dir" >/dev/null 2>&1 || true
  fi
}

path_prepend "$REPO_ROOT/scripts"
path_prepend "$REPO_ROOT/node_modules/.bin"
path_prepend "$REPO_ROOT/.tools/go/bin"
path_prepend "$REPO_ROOT/.tools/bin"
path_prepend "$HOME/.local/bin"
path_prepend "/opt/homebrew/bin"
path_prepend "/usr/local/bin"
activate_fnm_node

export GOPATH="${GOPATH:-$REPO_ROOT/.tools/go-path}"
export GOMODCACHE="${GOMODCACHE:-$REPO_ROOT/.tools/go/pkg/mod}"
export GOCACHE="${GOCACHE:-$REPO_ROOT/.tools/go-build}"
export GOBIN="${GOBIN:-$REPO_ROOT/.tools/bin}"
export GOLANGCI_LINT_CACHE="${GOLANGCI_LINT_CACHE:-$REPO_ROOT/.tools/golangci-lint-cache}"
export SEMGREP_LOG_FILE="${SEMGREP_LOG_FILE:-$REPO_ROOT/.tools/semgrep/semgrep.log}"
export SEMGREP_SETTINGS_FILE="${SEMGREP_SETTINGS_FILE:-$REPO_ROOT/.tools/semgrep/settings.yml}"
export SEMGREP_VERSION_CACHE_PATH="${SEMGREP_VERSION_CACHE_PATH:-$REPO_ROOT/.tools/semgrep/semgrep_version}"
export TRIVY_CACHE_DIR="${TRIVY_CACHE_DIR:-$REPO_ROOT/.tools/trivy-cache}"
