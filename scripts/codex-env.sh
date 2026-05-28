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

path_prepend "$REPO_ROOT/scripts"
path_prepend "$REPO_ROOT/node_modules/.bin"
path_prepend "$REPO_ROOT/.tools/go/bin"
path_prepend "$REPO_ROOT/.tools/bin"
path_prepend "$HOME/.local/bin"
path_prepend "/opt/homebrew/bin"
path_prepend "/usr/local/bin"

export GOPATH="${GOPATH:-$REPO_ROOT/.tools/go-path}"
export GOMODCACHE="${GOMODCACHE:-$REPO_ROOT/.tools/go/pkg/mod}"
export GOCACHE="${GOCACHE:-$REPO_ROOT/.tools/go-build}"
export GOBIN="${GOBIN:-$REPO_ROOT/.tools/bin}"
export GOLANGCI_LINT_CACHE="${GOLANGCI_LINT_CACHE:-$REPO_ROOT/.tools/golangci-lint-cache}"
