#!/bin/sh

# Bootstrap PATH for GUI git clients that provide a minimal environment.
if [ -d ".tools/bin" ]; then
  PATH="$(pwd)/.tools/bin:$PATH"
fi
if [ -d ".tools/go/bin" ]; then
  PATH="$(pwd)/.tools/go/bin:$PATH"
fi
if [ -d "/opt/homebrew/bin" ]; then
  PATH="/opt/homebrew/bin:$PATH"
fi
if [ -d "/usr/local/bin" ]; then
  PATH="/usr/local/bin:$PATH"
fi
if [ -d "$HOME/.volta/bin" ]; then
  PATH="$HOME/.volta/bin:$PATH"
fi
if [ -d "$HOME/.asdf/shims" ]; then
  PATH="$HOME/.asdf/shims:$PATH"
fi

# If node is still missing, try nvm-installed versions based on .nvmrc.
if ! command -v node >/dev/null 2>&1 && [ -f ".nvmrc" ] && [ -d "$HOME/.nvm/versions/node" ]; then
  NVM_VERSION=$(tr -d '[:space:]' < .nvmrc)
  NVM_BIN_DIR=$(ls -d "$HOME/.nvm/versions/node/v$NVM_VERSION"*/bin 2>/dev/null | head -n 1)
  if [ -n "$NVM_BIN_DIR" ]; then
    PATH="$NVM_BIN_DIR:$PATH"
  fi
fi
