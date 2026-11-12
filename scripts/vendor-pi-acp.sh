#!/usr/bin/env bash
#
# Vendor pi-acp as a single ESM bundle.
#
# Usage:
#   scripts/vendor-pi-acp.sh [version]
#   PI_ACP_SOURCE=/path/to/pi-acp.tgz scripts/vendor-pi-acp.sh [version]
#
# Overrides (env):
#   PI_ACP_PACKAGE   pi-acp npm package name           (default @geohar/pi-acp)
#   PI_ACP_SOURCE    npm install spec for pi-acp       (default $PI_ACP_PACKAGE@$VERSION)
#                    accepts a registry spec or a local .tgz (fork builds)
#   PI_ACP_SOURCE_LABEL  stable source text for NOTICE  (default derived from source)
#   PI_ACP_VERSION   pi-acp version                    (default 0.4.0)
#   ACP_SDK_VERSION  @agentclientprotocol/sdk version  (default 0.26.0)
#   ZOD_VERSION      zod version                       (default 3.25.76)
#   ESBUILD_VERSION  esbuild version                   (default 0.28.2)
#
# Output: resources/agents/pi-acp/
#   pi-acp.bundle.mjs   single-file ESM bundle (the app runs this directly)
#   package.json        generated metadata so agentInfo.name/version stay correct
#   VERSION             pinned pi-acp version
#   LICENSE             pi-acp MIT license
#   licenses/           licenses of the bundled dependencies
#   THIRD_PARTY_NOTICES.md
#   SHA256SUMS
#
# This is the only supported way to (re)build the bundle. Never run npx at
# runtime; the app starts `node <resources>/agents/pi-acp/pi-acp.bundle.mjs`.
#
# For the local fork in ./pi-acp-main, use scripts/vendor-pi-acp-local.sh,
# which builds the tarball and calls this script with PI_ACP_SOURCE set.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="$ROOT/resources/agents/pi-acp"
BUNDLE_NAME="pi-acp.bundle.mjs"

VERSION="${1:-${PI_ACP_VERSION:-0.4.0}}"
PACKAGE_NAME="${PI_ACP_PACKAGE:-@geohar/pi-acp}"
SOURCE="${PI_ACP_SOURCE:-$PACKAGE_NAME@$VERSION}"
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

# Local tarballs are common for fork builds; resolve them to an absolute path
# before changing into the temporary install directory.
SOURCE_IS_LOCAL=0
if [[ -e "$SOURCE" || "$SOURCE" == ./* || "$SOURCE" == ../* || "$SOURCE" == /* ]]; then
  [[ -e "$SOURCE" ]] || die "local pi-acp source not found: $SOURCE"
  SOURCE_IS_LOCAL=1
  SOURCE="$(cd "$(dirname "$SOURCE")" && pwd)/$(basename "$SOURCE")"
fi

TMP="$(mktemp -d "${TMPDIR:-/tmp}/pi-acp-vendor.XXXXXX")"
STAGE=""
cleanup() {
  rm -rf "$TMP"
  if [[ -n "$STAGE" ]]; then rm -rf "$STAGE"; fi
}
trap cleanup EXIT

echo "==> installing $PACKAGE_NAME"
echo "    source=$SOURCE"
echo "    sdk=$SDK_VERSION zod=$ZOD_VERSION esbuild=$ESBUILD_VERSION"
(
  cd "$TMP"
  npm init -y >/dev/null 2>&1
  npm install --no-audit --no-fund --save-exact --loglevel=error \
    "$SOURCE" \
    "@agentclientprotocol/sdk@$SDK_VERSION" \
    "zod@$ZOD_VERSION" \
    "esbuild@$ESBUILD_VERSION"
)

PKG_DIR="$TMP/node_modules/$PACKAGE_NAME"
[[ -f "$PKG_DIR/package.json" ]] || die "installed package not found at $PKG_DIR"

pkg_field() {
  node -e 'console.log(require(process.argv[1])[process.argv[2]])' "$PKG_DIR/package.json" "$1"
}

RESOLVED_NAME="$(pkg_field name)"
RESOLVED_ACP="$(pkg_field version)"
RESOLVED_SDK="$(node -e 'console.log(require(process.argv[1]).version)' "$TMP/node_modules/@agentclientprotocol/sdk/package.json")"
RESOLVED_ZOD="$(node -e 'console.log(require(process.argv[1]).version)' "$TMP/node_modules/zod/package.json")"
RESOLVED_ESBUILD="$(node -e 'console.log(require(process.argv[1]).version)' "$TMP/node_modules/esbuild/package.json")"

[[ "$RESOLVED_NAME" == "$PACKAGE_NAME" ]] || die "resolved package name $RESOLVED_NAME, expected $PACKAGE_NAME"
[[ "$RESOLVED_ACP" == "$VERSION" ]] || die "pi-acp resolved to $RESOLVED_ACP, expected $VERSION"
[[ "$RESOLVED_SDK" == "$SDK_VERSION" ]] || die "sdk resolved to $RESOLVED_SDK, expected $SDK_VERSION"
[[ "$RESOLVED_ZOD" == "$ZOD_VERSION" ]] || die "zod resolved to $RESOLVED_ZOD, expected $ZOD_VERSION"

# Keep the generated NOTICE stable and free of mktemp paths.
if [[ -n "${PI_ACP_SOURCE_LABEL:-}" ]]; then
  SOURCE_LABEL="$PI_ACP_SOURCE_LABEL"
elif (( SOURCE_IS_LOCAL )); then
  SOURCE_LABEL="local fork ($RESOLVED_NAME@$RESOLVED_ACP)"
else
  SOURCE_LABEL="$RESOLVED_NAME@$RESOLVED_ACP"
fi

[[ -f "$PKG_DIR/dist/index.js" ]] || die "pi-acp dist/index.js missing; run npm run build in the fork first"

echo "==> bundling with esbuild $RESOLVED_ESBUILD"
"$TMP/node_modules/.bin/esbuild" \
  "$PKG_DIR/dist/index.js" \
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
# it reports the vendored package instead of this repository's package.json.
cat > "$TMP/package.json" <<JSON
{
  "name": "$RESOLVED_NAME",
  "version": "$RESOLVED_ACP",
  "private": true,
  "type": "module",
  "license": "MIT",
  "description": "Generated metadata for the vendored pi-acp bundle; keeps agentInfo.name/version correct."
}
JSON

echo "==> smoke test (initialize only, no pi, no credentials)"
node "$ROOT/scripts/pi-acp-smoke.mjs" "$TMP/$BUNDLE_NAME"

# Build the complete directory next to the destination, then swap it in only
# after every check passed. A failed rebuild leaves the previous bundle intact.
STAGE="$ROOT/resources/agents/.pi-acp.stage.$$"
rm -rf "$STAGE"
mkdir -p "$STAGE/licenses"

cp "$TMP/$BUNDLE_NAME" "$STAGE/$BUNDLE_NAME"
cp "$TMP/package.json" "$STAGE/package.json"
printf '%s\n' "$RESOLVED_ACP" > "$STAGE/VERSION"
if [[ -f "$PKG_DIR/LICENSE" ]]; then
  cp "$PKG_DIR/LICENSE" "$STAGE/LICENSE"
elif [[ -f "$OUT_DIR/LICENSE" ]]; then
  # Local fork tarballs may omit LICENSE; keep the previous vendored copy.
  cp "$OUT_DIR/LICENSE" "$STAGE/LICENSE"
else
  die "pi-acp LICENSE missing from $PKG_DIR and no previous vendor copy exists"
fi
cp "$TMP/node_modules/@agentclientprotocol/sdk/LICENSE" "$STAGE/licenses/agentclientprotocol-sdk.LICENSE"
cp "$TMP/node_modules/zod/LICENSE" "$STAGE/licenses/zod.LICENSE"

if [[ -f "$PKG_DIR/THIRD_PARTY_NOTICES.md" ]]; then
  cp "$PKG_DIR/THIRD_PARTY_NOTICES.md" "$STAGE/THIRD_PARTY_NOTICES.md"
else
  cat > "$STAGE/THIRD_PARTY_NOTICES.md" <<EOF
# Third-party notices

This directory contains a single-file ESM bundle of $RESOLVED_NAME.
Do not edit \`$BUNDLE_NAME\` by hand; rebuild it with:

\`\`\`bash
scripts/vendor-pi-acp.sh $RESOLVED_ACP
\`\`\`

For the local fork in \`pi-acp-main\`, use:

\`\`\`bash
scripts/vendor-pi-acp-local.sh
\`\`\`

| Package | Version | License | Source |
| --- | --- | --- | --- |
| $RESOLVED_NAME | $RESOLVED_ACP | MIT | $SOURCE_LABEL |
| @agentclientprotocol/sdk | $RESOLVED_SDK | Apache-2.0 | https://github.com/agentclientprotocol/typescript-sdk |
| zod | $RESOLVED_ZOD | MIT | https://github.com/colinhacks/zod |

Bundled with esbuild $RESOLVED_ESBUILD (platform=node, format=esm, target=node22,
minify-whitespace, no legal comments; license texts are kept in this directory)
from \`$RESOLVED_NAME/dist/index.js\`.

Full license texts: \`LICENSE\` (pi-acp) and \`licenses/*\`.
EOF
fi

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$@"
  else
    shasum -a 256 "$@"
  fi
}

(
  cd "$STAGE"
  sha256 "$BUNDLE_NAME" package.json VERSION > SHA256SUMS
)

rm -rf "$OUT_DIR"
mv "$STAGE" "$OUT_DIR"
STAGE=""

BUNDLE_SHA="$(node -e 'const c=require("node:crypto"),f=require("node:fs");console.log(c.createHash("sha256").update(f.readFileSync(process.argv[1])).digest("hex"))' "$OUT_DIR/$BUNDLE_NAME")"
BUNDLE_SIZE="$(wc -c < "$OUT_DIR/$BUNDLE_NAME" | tr -d ' ')"

cat <<EOF

==> done
    dir      $OUT_DIR
    package  $RESOLVED_NAME
    version  $RESOLVED_ACP
    bundle   $BUNDLE_NAME ($((BUNDLE_SIZE / 1024)) KiB)
    sha256   $BUNDLE_SHA

The app must use this bundle directly:
    node <resources>/agents/pi-acp/$BUNDLE_NAME
Do not rebuild on startup.
EOF
