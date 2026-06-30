#!/usr/bin/env bash
# One-command installer for llm-guard.
#
#   curl -fsSL https://raw.githubusercontent.com/densub/llm-guard/main/scripts/install.sh | bash
#
# Environment overrides:
#   LLM_GUARD_REPO    git remote (default: https://github.com/densub/llm-guard.git)
#   LLM_GUARD_BRANCH  branch to clone (default: main)
#   LLM_GUARD_BIN_DIR install directory (default: ~/.local/bin)

set -euo pipefail

REPO="${LLM_GUARD_REPO:-https://github.com/densub/llm-guard.git}"
BRANCH="${LLM_GUARD_BRANCH:-main}"
BIN_DIR="${LLM_GUARD_BIN_DIR:-${HOME}/.local/bin}"

bold() { printf '\033[1m%s\033[0m\n' "$*"; }
dim()  { printf '\033[2m%s\033[0m\n' "$*"; }

bold "llm-guard installer"
echo

# If the script lives inside a checkout, build from there (local dev).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -f "${SCRIPT_DIR}/../go.mod" ]]; then
  SRC_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
  CLEANUP_SRC=false
else
  if ! command -v git >/dev/null 2>&1; then
    echo "Error: git is required to download llm-guard." >&2
    exit 1
  fi
  SRC_DIR="$(mktemp -d)"
  CLEANUP_SRC=true
  trap 'rm -rf "${SRC_DIR}"' EXIT
  dim "Cloning ${REPO} (${BRANCH})..."
  git clone --depth 1 --branch "${BRANCH}" "${REPO}" "${SRC_DIR}"
fi

if ! command -v go >/dev/null 2>&1; then
  echo "Error: Go is required to build llm-guard." >&2
  echo "Install from https://go.dev/dl/ and re-run this script." >&2
  exit 1
fi

dim "Building llmguard..."
(cd "${SRC_DIR}" && go build -ldflags="-s -w" -o llmguard ./cmd/llmguard)

mkdir -p "${BIN_DIR}"
install -m 0755 "${SRC_DIR}/llmguard" "${BIN_DIR}/llmguard"

if [[ ":${PATH}:" != *":${BIN_DIR}:"* ]]; then
  echo
  echo "Note: ${BIN_DIR} is not on your PATH."
  echo "Add this to your shell profile:"
  echo "  export PATH=\"\${HOME}/.local/bin:\${PATH}\""
  echo
fi

"${BIN_DIR}/llmguard" install "$@"

if [[ "${CLEANUP_SRC}" == true ]]; then
  rm -rf "${SRC_DIR}"
  trap - EXIT
fi
