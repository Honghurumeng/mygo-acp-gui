#!/usr/bin/env bash
#
# Build the local pi-acp fork and vendor the result into resources.
#
# Usage:
#   scripts/vendor-pi-acp-local.sh [fork-dir]
#
# Defaults:
#   fork dir   ./pi-acp-main (override with $PI_ACP_SRC or the first argument)
#
# Env overrides:
#   PI_ACP_SRC             local fork directory
#   PI_ACP_SKIP_INSTALL=1  reuse an existing node_modules (faster rebuilds)
#   ACP_SDK_VERSION        @agentclientprotocol/sdk version (default 0.26.0)
#   ZOD_VERSION            zod version                      (default 3.25.76)
#   ESBUILD_VERSION        esbuild version                  (default 0.28.2)
#
# What it does:
#   1. npm ci (unless PI_ACP_SKIP_INSTALL=1)
#   2. npm run build (tsup -> dist/index.js)
#   3. npm pack to a temp dir
#   4. scripts/vendor-pi-acp.sh with PI_ACP_SOURCE=<tarball>
#
# The resulting bundle, metadata and checksums are written atomically to
# resources/agents/pi-acp/. The old bundle stays intact if any step fails.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC_DIR="${1:-${PI_ACP_SRC:-$ROOT/pi-acp-main}}"
SRC_DIR="$(cd "$SRC_DIR" && pwd)"

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

command -v node >/dev/null 2>&1 || die "node not found in PATH"
command -v npm >/dev/null 2>&1 || die "npm not found in PATH"

[[ -f "$SRC_DIR/package.json" ]] || die "not a pi-acp source tree: $SRC_DIR"
[[ -f "$SRC_DIR/package-lock.json" ]] || die "package-lock.json missing in $SRC_DIR"

NODE_MAJOR="$(node -p 'Number(process.versions.node.split(".")[0])')"
if (( NODE_MAJOR < 20 )); then
  die "Node >= 20 required to build (found $(node --version))"
fi

VERSION="$(node -p 'require(process.argv[1]).version' "$SRC_DIR/package.json")"
PACKAGE_NAME="$(node -p 'require(process.argv[1]).name' "$SRC_DIR/package.json")"
[[ -n "$VERSION" ]] || die "missing version in $SRC_DIR/package.json"
[[ -n "$PACKAGE_NAME" ]] || die "missing name in $SRC_DIR/package.json"

TMP="$(mktemp -d "${TMPDIR:-/tmp}/pi-acp-local.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

echo "==> local fork: $SRC_DIR"
echo "    package=$PACKAGE_NAME version=$VERSION"

if [[ "${PI_ACP_SKIP_INSTALL:-0}" == "1" ]]; then
  echo "==> npm ci skipped (PI_ACP_SKIP_INSTALL=1)"
  [[ -x "$SRC_DIR/node_modules/.bin/tsup" ]] || die "node_modules/.bin/tsup missing; run without PI_ACP_SKIP_INSTALL=1"
else
  echo "==> npm ci"
  (
    cd "$SRC_DIR"
    npm ci --no-audit --no-fund --loglevel=error
  )
fi

echo "==> npm run build"
(
  cd "$SRC_DIR"
  npm run build
)
[[ -f "$SRC_DIR/dist/index.js" ]] || die "build did not produce dist/index.js"

echo "==> npm pack"
TARBALL_NAME="$(
  cd "$SRC_DIR"
  npm pack --pack-destination "$TMP" --silent | tail -n 1
)"
[[ -n "$TARBALL_NAME" && -f "$TMP/$TARBALL_NAME" ]] || die "npm pack did not produce a tarball"

echo "==> vendor $TARBALL_NAME"
PI_ACP_PACKAGE="$PACKAGE_NAME" \
PI_ACP_SOURCE="$TMP/$TARBALL_NAME" \
PI_ACP_SOURCE_LABEL="local fork (pi-acp-main)" \
  "$ROOT/scripts/vendor-pi-acp.sh" "$VERSION"

echo
echo "==> local vendor done"
echo "    app bundle: resources/agents/pi-acp/pi-acp.bundle.mjs"
echo "    source:     $SRC_DIR"
