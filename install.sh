#!/usr/bin/env bash
set -euo pipefail

APP=rgw-ast
REPO_URL="https://github.com/ryangerardwilson/rgw-ast.git"
INSTALL_DIR="${RGW_AST_INSTALL_DIR:-$HOME/.local/bin}"
INSTALL_TMP_DIR=""

usage() {
  cat <<EOF
${APP} installer

Usage:
  install.sh              Install latest from GitHub source
  install.sh from <path>  Install from local source checkout
  install.sh help         Show this help

EOF
}

die() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "'$1' is required"
}

cleanup_install_tmp() {
  if [[ -n "${INSTALL_TMP_DIR:-}" ]]; then
    rm -rf "$INSTALL_TMP_DIR"
    INSTALL_TMP_DIR=""
  fi
}

install_from_source() {
  local source_path=$1
  [[ -d "$source_path" ]] || die "source path does not exist: $source_path"
  mkdir -p "$INSTALL_DIR"
  (cd "$source_path" && go build -o "$INSTALL_DIR/$APP" ./cmd/rgw-ast)
  chmod 755 "$INSTALL_DIR/$APP"
  "$INSTALL_DIR/$APP" version
}

install_latest() {
  require_command git
  INSTALL_TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/${APP}_install_XXXXXX")"
  trap cleanup_install_tmp EXIT
  git clone --depth 1 "$REPO_URL" "$INSTALL_TMP_DIR/$APP" >/dev/null
  install_from_source "$INSTALL_TMP_DIR/$APP"
  cleanup_install_tmp
  trap - EXIT
}

case "${1:-install}" in
  install)
    require_command go
    install_latest
    ;;
  from)
    require_command go
    [[ -n "${2:-}" ]] || die "from requires a path"
    install_from_source "$2"
    ;;
  help|-h|--help)
    usage
    ;;
  *)
    die "unknown command: $1"
    ;;
esac
