# Third-party notices

This directory contains a single-file ESM bundle of @geohar/pi-acp.
Do not edit `pi-acp.bundle.mjs` by hand; rebuild it with:

```bash
scripts/vendor-pi-acp.sh 0.4.0
```

For the local fork in `pi-acp-main`, use:

```bash
scripts/vendor-pi-acp-local.sh
```

| Package | Version | License | Source |
| --- | --- | --- | --- |
| @geohar/pi-acp | 0.4.0 | MIT | local fork (pi-acp-main) |
| @agentclientprotocol/sdk | 0.26.0 | Apache-2.0 | https://github.com/agentclientprotocol/typescript-sdk |
| zod | 3.25.76 | MIT | https://github.com/colinhacks/zod |

Bundled with esbuild 0.28.2 (platform=node, format=esm, target=node22,
minify-whitespace, no legal comments; license texts are kept in this directory)
from `@geohar/pi-acp/dist/index.js`.

Full license texts: `LICENSE` (pi-acp) and `licenses/*`.
