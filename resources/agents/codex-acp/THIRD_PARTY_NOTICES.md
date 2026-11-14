# Third-party notices

This directory contains `codex-acp.bundle.mjs`, the official single-file
distribution bundle of @agentclientprotocol/codex-acp@2.6.0 (Apache-2.0).
Do not edit the bundle by hand; rebuild it with:

```bash
scripts/vendor-codex-acp.sh 2.6.0
```

The app runs the bundle with Node.js 22+ and always sets `CODEX_PATH` to
the user's installed Codex CLI. The Codex CLI package (`@openai/codex`,
Apache-2.0, https://github.com/openai/codex) is **not bundled**; the
adapter starts `codex app-server` from the resolved absolute path.

| Package | Version | License | Source |
| --- | --- | --- | --- |
| @agentclientprotocol/codex-acp | 2.6.0 | Apache-2.0 | @agentclientprotocol/codex-acp@2.6.0 |
| @agentclientprotocol/sdk | 2.4.0 | Apache-2.0 | https://www.npmjs.com/package/@agentclientprotocol/sdk/v/2.4.0 |
| bundle-name | 4.1.1 | MIT | https://www.npmjs.com/package/bundle-name/v/4.1.1 |
| cross-spawn | 7.0.6 | MIT | https://www.npmjs.com/package/cross-spawn/v/7.0.6 |
| default-browser | 5.5.1 | MIT | https://www.npmjs.com/package/default-browser/v/5.5.1 |
| default-browser-id | 5.0.1 | MIT | https://www.npmjs.com/package/default-browser-id/v/5.0.1 |
| define-lazy-prop | 3.0.0 | MIT | https://www.npmjs.com/package/define-lazy-prop/v/3.0.0 |
| diff | 9.0.0 | BSD-3-Clause | https://www.npmjs.com/package/diff/v/9.0.0 |
| is-docker | 3.0.0 | MIT | https://www.npmjs.com/package/is-docker/v/3.0.0 |
| is-in-ssh | 1.0.0 | MIT | https://www.npmjs.com/package/is-in-ssh/v/1.0.0 |
| is-inside-container | 1.0.0 | MIT | https://www.npmjs.com/package/is-inside-container/v/1.0.0 |
| is-wsl | 3.1.1 | MIT | https://www.npmjs.com/package/is-wsl/v/3.1.1 |
| isexe | 2.0.0 | ISC | https://www.npmjs.com/package/isexe/v/2.0.0 |
| open | 11.0.4 | MIT | https://www.npmjs.com/package/open/v/11.0.4 |
| path-key | 3.1.1 | MIT | https://www.npmjs.com/package/path-key/v/3.1.1 |
| powershell-utils | 0.2.1 | MIT | https://www.npmjs.com/package/powershell-utils/v/0.2.1 |
| run-applescript | 7.1.0 | MIT | https://www.npmjs.com/package/run-applescript/v/7.1.0 |
| shebang-command | 2.0.0 | MIT | https://www.npmjs.com/package/shebang-command/v/2.0.0 |
| shebang-regex | 3.0.0 | MIT | https://www.npmjs.com/package/shebang-regex/v/3.0.0 |
| vscode-jsonrpc | 9.0.3 | MIT | https://www.npmjs.com/package/vscode-jsonrpc/v/9.0.3 |
| which | 2.0.2 | ISC | https://www.npmjs.com/package/which/v/2.0.2 |
| wsl-utils | 1.0.1 | MIT | https://www.npmjs.com/package/wsl-utils/v/1.0.1 |
| zod | 4.6.7 | MIT | https://www.npmjs.com/package/zod/v/4.6.7 |
| @openai/codex (external, not bundled) | 0.166.0 | Apache-2.0 | https://github.com/openai/codex |

The JavaScript dependencies above are embedded in the official bundle.
Their license texts are kept in `licenses/`; the adapter's own license is
`LICENSE`.
