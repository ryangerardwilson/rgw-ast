#!/usr/bin/env bash
set -euo pipefail

APP=rgw-ast
REPO="ryangerardwilson/rgw-ast"
REPO_URL="https://github.com/${REPO}.git"
INSTALL_DIR="${RGW_AST_INSTALL_DIR:-$HOME/.local/bin}"
INSTALL_TMP_DIR=""
MODULE_PATH="github.com/ryangerardwilson/rgw-ast"
ASSET_NAME="rgw-ast-linux-x64.tar.gz"

usage() {
  cat <<EOF
${APP} installer

Usage:
  install.sh                 Install latest GitHub release (fallback: source)
  install.sh version         Print latest release version
  install.sh version <ver>   Install a specific release version
  install.sh upgrade         Same as install latest
  install.sh from <path>     Install from local source checkout (git-stamped)
  install.sh help            Show this help

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

normalize_version() {
  local version="$1"
  version="${version#v}"
  [[ -n "$version" ]] || die "empty version"
  printf '%s\n' "$version"
}

get_latest_version() {
  require_command curl
  local release_url tag
  release_url="$(curl -fsSL -o /dev/null -w "%{url_effective}" "https://github.com/${REPO}/releases/latest")" \
    || return 1
  tag="${release_url##*/}"
  tag="${tag#v}"
  [[ -n "$tag" && "$tag" != "latest" ]] || return 1
  printf '%s\n' "$tag"
}

build_ldflags() {
  local version="$1"
  local commit="$2"
  local build_time
  build_time="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf '%s' "-s -w -X ${MODULE_PATH}/internal/version.Version=${version} -X ${MODULE_PATH}/internal/version.Commit=${commit} -X ${MODULE_PATH}/internal/version.BuildTime=${build_time}"
}

source_version_meta() {
  local source_path="$1"
  local version commit
  commit="$(git -C "$source_path" rev-parse --short HEAD 2>/dev/null || echo unknown)"
  if version="$(git -C "$source_path" describe --tags --exact-match 2>/dev/null)"; then
    version="${version#v}"
  elif version="$(git -C "$source_path" describe --tags --match 'v*' 2>/dev/null)"; then
    version="${version#v}"
  else
    version="0.0.0-dev"
  fi
  printf '%s %s\n' "$version" "$commit"
}

install_from_source() {
  local source_path=$1
  [[ -d "$source_path" ]] || die "source path does not exist: $source_path"
  require_command go
  mkdir -p "$INSTALL_DIR"
  local version commit
  read -r version commit < <(source_version_meta "$source_path")
  local ldflags
  ldflags="$(build_ldflags "$version" "$commit")"
  (cd "$source_path" && CGO_ENABLED=0 go build -ldflags "$ldflags" -o "$INSTALL_DIR/$APP" ./cmd/rgw-ast)
  chmod 755 "$INSTALL_DIR/$APP"
  "$INSTALL_DIR/$APP" version
}

install_from_release() {
  local version
  version="$(normalize_version "$1")"
  require_command curl
  require_command tar
  INSTALL_TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/${APP}_install_XXXXXX")"
  trap cleanup_install_tmp EXIT
  local url="https://github.com/${REPO}/releases/download/v${version}/${ASSET_NAME}"
  curl -fsSL "$url" -o "$INSTALL_TMP_DIR/$ASSET_NAME" \
    || die "failed to download release asset: $url"
  tar -C "$INSTALL_TMP_DIR" -xzf "$INSTALL_TMP_DIR/$ASSET_NAME"
  [[ -f "$INSTALL_TMP_DIR/$APP" ]] || die "release archive missing ${APP} binary"
  mkdir -p "$INSTALL_DIR"
  install -m 755 "$INSTALL_TMP_DIR/$APP" "$INSTALL_DIR/$APP"
  cleanup_install_tmp
  trap - EXIT
  "$INSTALL_DIR/$APP" version
}

install_latest() {
  local version
  if version="$(get_latest_version 2>/dev/null)"; then
    install_from_release "$version"
    return
  fi
  # No releases yet: clone source and stamp from git.
  require_command git
  require_command go
  INSTALL_TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/${APP}_install_XXXXXX")"
  trap cleanup_install_tmp EXIT
  git clone --depth 1 "$REPO_URL" "$INSTALL_TMP_DIR/$APP" >/dev/null
  install_from_source "$INSTALL_TMP_DIR/$APP"
  cleanup_install_tmp
  trap - EXIT
}

case "${1:-install}" in
  install|upgrade)
    install_latest
    ;;
  version)
    if [[ -n "${2:-}" ]]; then
      install_from_release "$2"
    else
      get_latest_version || die "no releases found"
    fi
    ;;
  from)
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
