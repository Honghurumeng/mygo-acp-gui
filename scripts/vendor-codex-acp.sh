#!/usr/bin/env bash
#
# Vendor the official codex-acp adapter as a single-file bundle.
#
# Usage:
#   scripts/vendor-codex-acp.sh [version]
#   CODEX_ACP_SOURCE=/path/to/codex-acp.tgz scripts/vendor-codex-acp.sh [version]
#
# Overrides (env):
#   CODEX_ACP_PACKAGE   codex-acp npm package name (default @agentclientprotocol/codex-acp)
#   CODEX_ACP_SOURCE    npm install spec (default $CODEX_ACP_PACKAGE@$VERSION)
#   CODEX_ACP_VERSION   codex-acp version (default 2.6.0)
#
# Output: resources/agents/codex-acp/
#   codex-acp.bundle.mjs  official dist/index.js, copied byte-for-byte
#   VERSION               pinned codex-acp version
#   LICENSE               codex-acp Apache-2.0 license
#   licenses/             licenses of the JavaScript dependencies in the bundle
#   THIRD_PARTY_NOTICES.md
#   SHA256SUMS
#
# Unlike pi-acp, codex-acp publishes a single-file ESM bundle itself. We keep
# that official bundle unchanged and only add metadata/licensing beside it.
# The bundle expects @openai/codex unless CODEX_PATH is set; this app always
# sets CODEX_PATH to the user's installed Codex CLI (see docs/design/codex.md).
# Never run npx at startup.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="$ROOT/resources/agents/codex-acp"
BUNDLE_NAME="codex-acp.bundle.mjs"

VERSION="${1:-${CODEX_ACP_VERSION:-2.6.0}}"
PACKAGE_NAME="${CODEX_ACP_PACKAGE:-@agentclientprotocol/codex-acp}"
SOURCE="${CODEX_ACP_SOURCE:-$PACKAGE_NAME@$VERSION}"

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

SOURCE_IS_LOCAL=0
if [[ -e "$SOURCE" || "$SOURCE" == ./* || "$SOURCE" == ../* || "$SOURCE" == /* ]]; then
  [[ -e "$SOURCE" ]] || die "local codex-acp source not found: $SOURCE"
  SOURCE_IS_LOCAL=1
  SOURCE="$(cd "$(dirname "$SOURCE")" && pwd)/$(basename "$SOURCE")"
fi

TMP="$(mktemp -d "${TMPDIR:-/tmp}/codex-acp-vendor.XXXXXX")"
STAGE=""
cleanup() {
  rm -rf "$TMP"
  if [[ -n "$STAGE" ]]; then rm -rf "$STAGE"; fi
}
trap cleanup EXIT

echo "==> installing $PACKAGE_NAME"
echo "    source=$SOURCE"
(
  cd "$TMP"
  npm init -y >/dev/null 2>&1
  # --omit=optional avoids downloading platform-specific @openai/codex-*
  # native binaries; they are not bundled and not needed by this app.
  npm install --omit=optional --no-audit --no-fund --save-exact --loglevel=error "$SOURCE"
)

PKG_DIR="$TMP/node_modules/$PACKAGE_NAME"
[[ -f "$PKG_DIR/package.json" ]] || die "installed package not found at $PKG_DIR"

pkg_field() {
  node -e 'console.log(require(process.argv[1])[process.argv[2]])' "$PKG_DIR/package.json" "$1"
}

RESOLVED_NAME="$(pkg_field name)"
RESOLVED_VERSION="$(pkg_field version)"
RESOLVED_LICENSE="$(pkg_field license)"

[[ "$RESOLVED_NAME" == "$PACKAGE_NAME" ]] || die "resolved package name $RESOLVED_NAME, expected $PACKAGE_NAME"
[[ "$RESOLVED_VERSION" == "$VERSION" ]] || die "codex-acp resolved to $RESOLVED_VERSION, expected $VERSION"
[[ -f "$PKG_DIR/dist/index.js" ]] || die "codex-acp dist/index.js missing"
[[ -f "$PKG_DIR/LICENSE" ]] || die "codex-acp LICENSE missing"

if [[ -n "${CODEX_ACP_SOURCE_LABEL:-}" ]]; then
  SOURCE_LABEL="$CODEX_ACP_SOURCE_LABEL"
elif (( SOURCE_IS_LOCAL )); then
  SOURCE_LABEL="local source ($RESOLVED_NAME@$RESOLVED_VERSION)"
else
  SOURCE_LABEL="$RESOLVED_NAME@$RESOLVED_VERSION"
fi

echo "==> checking official bundle"
cp "$PKG_DIR/dist/index.js" "$TMP/$BUNDLE_NAME"
node --check "$TMP/$BUNDLE_NAME"
VERSION_OUTPUT="$(node "$TMP/$BUNDLE_NAME" --version)"
[[ "$VERSION_OUTPUT" == *"$RESOLVED_VERSION"* ]] || die "bundle --version did not report $RESOLVED_VERSION: $VERSION_OUTPUT"
if ! grep -q '@openai/codex/bin/codex.js' "$TMP/$BUNDLE_NAME"; then
  die "bundle no longer references @openai/codex; review the CODEX_PATH launch design"
fi
if ! grep -q 'CODEX_PATH' "$TMP/$BUNDLE_NAME"; then
  die "bundle does not support CODEX_PATH"
fi

STAGE="$ROOT/resources/agents/.codex-acp.stage.$$"
rm -rf "$STAGE"
mkdir -p "$STAGE/licenses"

cp "$TMP/$BUNDLE_NAME" "$STAGE/$BUNDLE_NAME"
printf '%s\n' "$RESOLVED_VERSION" > "$STAGE/VERSION"
cp "$PKG_DIR/LICENSE" "$STAGE/LICENSE"

# Copy the licenses of every JavaScript package embedded in the official
# bundle, and generate a deterministic NOTICE. @openai/codex is deliberately
# external, so it is listed separately instead of copied.
node - "$TMP" "$STAGE" "$RESOLVED_NAME" "$RESOLVED_VERSION" "$RESOLVED_LICENSE" "$SOURCE_LABEL" <<'NODE'
const fs = require("node:fs");
const path = require("node:path");
const [tmp, stage, adapterName, adapterVersion, adapterLicense, sourceLabel] = process.argv.slice(2);
const modules = path.join(tmp, "node_modules");
const licenseDir = path.join(stage, "licenses");

function packageDirs(dir, out) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (!entry.isDirectory() || entry.name.startsWith(".")) continue;
    const child = path.join(dir, entry.name);
    if (fs.existsSync(path.join(child, "package.json"))) {
      out.push(child);
      const nested = path.join(child, "node_modules");
      if (fs.existsSync(nested)) packageDirs(nested, out);
    } else if (entry.name.startsWith("@")) {
      packageDirs(child, out);
    }
  }
}

const dirs = [];
packageDirs(modules, dirs);
const seen = new Set();
const rows = [];
const licenseFiles = [];
for (const dir of dirs) {
  let pkg;
  try {
    pkg = JSON.parse(fs.readFileSync(path.join(dir, "package.json"), "utf8"));
  } catch {
    continue;
  }
  if (!pkg.name || !pkg.version) continue;
  if (pkg.name === adapterName) continue;
  if (pkg.name === "@openai/codex" || pkg.name.startsWith("@openai/codex-")) continue;
  const key = `${pkg.name}@${pkg.version}`;
  if (seen.has(key)) continue;
  seen.add(key);

  const files = fs.readdirSync(dir)
    .filter((name) => /^(license|licence|copying)(\.|$)/i.test(name))
    .filter((name) => fs.statSync(path.join(dir, name)).isFile());
  const license = typeof pkg.license === "string" ? pkg.license : (pkg.license?.type || "see package");
  let licenseFile = "";
  if (files.length > 0) {
    const target = `${pkg.name.replace(/^@/, "").replace(/\//g, "-")}.LICENSE`;
    fs.copyFileSync(path.join(dir, files[0]), path.join(licenseDir, target));
    licenseFile = `licenses/${target}`;
    licenseFiles.push(target);
  }
  rows.push({
    name: pkg.name,
    version: pkg.version,
    license,
    licenseFile,
  });
}
rows.sort((a, b) => a.name.localeCompare(b.name));

let codexVersion = "0.166.0";
try {
  codexVersion = JSON.parse(fs.readFileSync(path.join(modules, "@openai/codex", "package.json"), "utf8")).version;
} catch {}

const lines = [];
lines.push("# Third-party notices");
lines.push("");
lines.push("This directory contains `codex-acp.bundle.mjs`, the official single-file");
lines.push(`distribution bundle of ${adapterName}@${adapterVersion} (${adapterLicense}).`);
lines.push("Do not edit the bundle by hand; rebuild it with:");
lines.push("");
lines.push("```bash");
lines.push(`scripts/vendor-codex-acp.sh ${adapterVersion}`);
lines.push("```");
lines.push("");
lines.push("The app runs the bundle with Node.js 22+ and always sets `CODEX_PATH` to");
lines.push("the user's installed Codex CLI. The Codex CLI package (`@openai/codex`,");
lines.push("Apache-2.0, https://github.com/openai/codex) is **not bundled**; the");
lines.push("adapter starts `codex app-server` from the resolved absolute path.");
lines.push("");
lines.push("| Package | Version | License | Source |");
lines.push("| --- | --- | --- | --- |");
lines.push(`| ${adapterName} | ${adapterVersion} | ${adapterLicense} | ${sourceLabel} |`);
for (const row of rows) {
  lines.push(`| ${row.name} | ${row.version} | ${row.license} | https://www.npmjs.com/package/${row.name}/v/${row.version} |`);
}
lines.push(`| @openai/codex (external, not bundled) | ${codexVersion} | Apache-2.0 | https://github.com/openai/codex |`);
lines.push("");
lines.push("The JavaScript dependencies above are embedded in the official bundle.");
lines.push("Their license texts are kept in `licenses/`; the adapter's own license is");
lines.push("`LICENSE`.");
lines.push("");
fs.writeFileSync(path.join(stage, "THIRD_PARTY_NOTICES.md"), lines.join("\n"));
NODE

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$@"
  else
    shasum -a 256 "$@"
  fi
}

(
  cd "$STAGE"
  sha256 "$BUNDLE_NAME" VERSION LICENSE THIRD_PARTY_NOTICES.md > SHA256SUMS
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
    version  $RESOLVED_VERSION
    bundle   $BUNDLE_NAME ($((BUNDLE_SIZE / 1024)) KiB)
    sha256   $BUNDLE_SHA

The app must use this bundle directly:
    CODEX_PATH=/absolute/path/to/codex node <resources>/agents/codex-acp/$BUNDLE_NAME
Do not rebuild on startup.
EOF
