#!/usr/bin/env bash
#
# Vendor @geohar/pi-acp as a single ESM bundle.
#
# Usage:
#   scripts/vendor-pi-acp.sh [version]
#
# Overrides (env):
#   PI_ACP_VERSION   @geohar/pi-acp version            (default 0.4.0)
#   ACP_SDK_VERSION  @agentclientprotocol/sdk version  (default 0.26.0)
#   ZOD_VERSION      zod version                       (default 3.25.76)
#   ESBUILD_VERSION  esbuild version                   (default 0.28.2)
#
# Output: gui/resources/agents/pi-acp/
#   pi-acp.bundle.mjs   single-file ESM bundle (the app runs this directly)
#   package.json        generated metadata so agentInfo.name/version stay correct
#   VERSION             pinned @geohar/pi-acp version
#   LICENSE             pi-acp MIT license
#   licenses/           licenses of the bundled dependencies
#   THIRD_PARTY_NOTICES.md
#   SHA256SUMS
#
# This is the only supported way to (re)build the bundle. Never run npx at
# runtime; the app starts `node <resources>/agents/pi-acp/pi-acp.bundle.mjs`.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="$ROOT/gui/resources/agents/pi-acp"
BUNDLE_NAME="pi-acp.bundle.mjs"

VERSION="${1:-${PI_ACP_VERSION:-0.4.0}}"
SDK_VERSION="${ACP_SDK_VERSION:-0.26.0}"
ZOD_VERSION="${ZOD_VERSION:-3.25.76}"
ESBUILD_VERSION="${ESBUILD_VERSION:-0.28.2}"

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

command -v node >/dev/null 2>&1 || die "node not found in PATH"
command -v npm >/dev/null 2>&1 || die "npm not found in PATH"

NODE_MAJOR="$(node -p 'Number(process.versions.node.split(".")[0])')"
if (( NODE_MAJOR < 20 )); then
  die "Node >= 20 required to build (found $(node --version))"
fi

TMP="$(mktemp -d "${TMPDIR:-/tmp}/pi-acp-vendor.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

echo "==> installing @geohar/pi-acp@$VERSION"
echo "    sdk=$SDK_VERSION zod=$ZOD_VERSION esbuild=$ESBUILD_VERSION"
(
  cd "$TMP"
  npm init -y >/dev/null 2>&1
  npm install --no-audit --no-fund --save-exact --loglevel=error \
    "@geohar/pi-acp@$VERSION" \
    "@agentclientprotocol/sdk@$SDK_VERSION" \
    "zod@$ZOD_VERSION" \
    "esbuild@$ESBUILD_VERSION"
)

pkg_version() {
  node -e 'console.log(require(process.argv[1]).version)' "$TMP/node_modules/$1/package.json"
}

RESOLVED_ACP="$(pkg_version '@geohar/pi-acp')"
RESOLVED_SDK="$(pkg_version '@agentclientprotocol/sdk')"
RESOLVED_ZOD="$(pkg_version 'zod')"
RESOLVED_ESBUILD="$(pkg_version 'esbuild')"

[[ "$RESOLVED_ACP" == "$VERSION" ]] || die "pi-acp resolved to $RESOLVED_ACP, expected $VERSION"
[[ "$RESOLVED_SDK" == "$SDK_VERSION" ]] || die "sdk resolved to $RESOLVED_SDK, expected $SDK_VERSION"
[[ "$RESOLVED_ZOD" == "$ZOD_VERSION" ]] || die "zod resolved to $RESOLVED_ZOD, expected $ZOD_VERSION"

echo "==> bundling with esbuild $RESOLVED_ESBUILD"
"$TMP/node_modules/.bin/esbuild" \
  "$TMP/node_modules/@geohar/pi-acp/dist/index.js" \
  --bundle \
  --platform=node \
  --format=esm \
  --target=node22 \
  --minify-whitespace \
  --legal-comments=none \
  --log-level=warning \
  --outfile="$TMP/$BUNDLE_NAME"

node --check "$TMP/$BUNDLE_NAME"

# esbuild emits source-path comments relative to the output file; in a mktemp
# directory those make the bundle non-reproducible. --minify-whitespace strips
# them. Fail loudly if a build path still leaks through.
if grep -q "pi-acp-vendor" "$TMP/$BUNDLE_NAME"; then
  die "bundle leaks the build temp path"
fi

# The adapter reports agentInfo.name/version by walking up from import.meta.url
# looking for package.json (< 6 levels). Provide one right next to the bundle so
# it reports @geohar/pi-acp instead of this repository's package.json.
cat > "$TMP/package.json" <<JSON
{
  "name": "@geohar/pi-acp",
  "version": "$RESOLVED_ACP",
  "private": true,
  "type": "module",
  "license": "MIT",
  "description": "Generated metadata for the vendored pi-acp bundle; keeps agentInfo.name/version correct."
}
JSON

echo "==> smoke test (initialize only, no pi, no credentials)"
node "$ROOT/scripts/pi-acp-smoke.mjs" "$TMP/$BUNDLE_NAME"

mkdir -p "$OUT_DIR/licenses"

cp "$TMP/$BUNDLE_NAME" "$OUT_DIR/$BUNDLE_NAME"
cp "$TMP/package.json" "$OUT_DIR/package.json"
printf '%s\n' "$RESOLVED_ACP" > "$OUT_DIR/VERSION"
cp "$TMP/node_modules/@geohar/pi-acp/LICENSE" "$OUT_DIR/LICENSE"
cp "$TMP/node_modules/@agentclientprotocol/sdk/LICENSE" "$OUT_DIR/licenses/agentclientprotocol-sdk.LICENSE"
cp "$TMP/node_modules/zod/LICENSE" "$OUT_DIR/licenses/zod.LICENSE"

cat > "$OUT_DIR/THIRD_PARTY_NOTICES.md" <<EOF
# Third-party notices

This directory contains a single-file ESM bundle of @geohar/pi-acp.
Do not edit \`$BUNDLE_NAME\` by hand; rebuild it with:

\`\`\`bash
scripts/vendor-pi-acp.sh $RESOLVED_ACP
\`\`\`

| Package | Version | License | Source |
| --- | --- | --- | --- |
| @geohar/pi-acp | $RESOLVED_ACP | MIT | https://www.npmjs.com/package/@geohar/pi-acp |
| @agentclientprotocol/sdk | $RESOLVED_SDK | Apache-2.0 | https://github.com/agentclientprotocol/typescript-sdk |
| zod | $RESOLVED_ZOD | MIT | https://github.com/colinhacks/zod |

Bundled with esbuild $RESOLVED_ESBUILD (platform=node, format=esm, target=node22,
minify-whitespace, no legal comments; license texts are kept in this directory)
from \`@geohar/pi-acp/dist/index.js\`.

Full license texts: \`LICENSE\` (pi-acp) and \`licenses/*\`.
EOF

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$@"
  else
    shasum -a 256 "$@"
  fi
}

(
  cd "$OUT_DIR"
  sha256 "$BUNDLE_NAME" package.json VERSION > SHA256SUMS
)

BUNDLE_SHA="$(node -e 'const c=require("node:crypto"),f=require("node:fs");console.log(c.createHash("sha256").update(f.readFileSync(process.argv[1])).digest("hex"))' "$OUT_DIR/$BUNDLE_NAME")"
BUNDLE_SIZE="$(wc -c < "$OUT_DIR/$BUNDLE_NAME" | tr -d ' ')"

cat <<EOF

==> done
    dir      $OUT_DIR
    version  $RESOLVED_ACP
    bundle   $BUNDLE_NAME ($((BUNDLE_SIZE / 1024)) KiB)
    sha256   $BUNDLE_SHA

The app must use this bundle directly:
    node <resources>/agents/pi-acp/$BUNDLE_NAME
Do not rebuild on startup.
EOF
