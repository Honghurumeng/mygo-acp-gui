# pi 集成

## 结论：单文件 bundle，直接引用

pi-acp 已按「单文件 bundle」方式 vendor 到仓库，构建一次后固定使用：

```
gui/resources/agents/pi-acp/
  pi-acp.bundle.mjs      唯一运行时入口（~453 KiB）
  package.json           生成的元数据，仅供 agentInfo 读取（不要删）
  VERSION                0.4.0
  LICENSE                pi-acp 的 MIT 协议
  licenses/              @agentclientprotocol/sdk、zod 的 LICENSE
  THIRD_PARTY_NOTICES.md
  SHA256SUMS
```

打包 `.app` 时把整个 `agents/pi-acp/` 目录拷进 Resources；只拷 bundle 会丢 `agentInfo` 元数据和许可证。

固定版本与校验和：

| 包 | 版本 | 协议 |
| --- | --- | --- |
| `@geohar/pi-acp` | `0.4.0` | MIT |
| `@agentclientprotocol/sdk` | `0.26.0` | Apache-2.0 |
| `zod` | `3.25.76` | MIT |
| esbuild（仅构建期） | `0.28.2` | — |

`pi-acp.bundle.mjs` 的 sha256：
`28341f24eaf74f73f06d6f86a0cf63ed9dc1dd24c09763463d3b7531e67143c4`

应用启动时直接执行这个文件：

```go
res, _ := mygo.App.Path(mygo.PathResources)
bundle := filepath.Join(res, "agents", "pi-acp", "pi-acp.bundle.mjs")
cmd := exec.Command(nodePath, bundle)
```

**不要在启动时 `npm install` / `npx` / 重新 bundle。** 只有升级 pi-acp 时才跑 vendor 脚本。

## 为什么用单文件

- 只拷 `dist/index.js` 不够：它仍 `import "@agentclientprotocol/sdk"`，而 SDK 依赖 `zod`；完整依赖树约 7.1 MB。
- 不走 npx：npx 需要联网、版本漂移、启动慢，且从 Finder 启动时 PATH 不可靠。
- 单文件 bundle：离线可用、启动确定、体积小（453 KiB）；代价只是升级时要跑一次脚本，已自动化。

## 构建 / 升级

唯一入口：`scripts/vendor-pi-acp.sh`。

```bash
scripts/vendor-pi-acp.sh              # 默认 0.4.0
scripts/vendor-pi-acp.sh 0.4.1        # 指定版本
node scripts/pi-acp-smoke.mjs         # 单独复跑 initialize 冒烟
```

脚本流程：临时目录 `npm install --save-exact` → esbuild `--bundle --platform=node --format=esm --target=node22` → `node --check` → initialize 冒烟 → 原子替换 `gui/resources/agents/pi-acp/` → 生成 NOTICE 和 `SHA256SUMS`。

直接依赖版本锁在脚本里，可用环境变量覆盖：

```bash
PI_ACP_VERSION=0.4.0 ACP_SDK_VERSION=0.26.0 ZOD_VERSION=3.25.76 \
  ESBUILD_VERSION=0.28.2 scripts/vendor-pi-acp.sh
```

构建使用 `--minify-whitespace --legal-comments=none` 去掉 esbuild 生成的临时目录注释，保证同样输入产出同样字节（已验证连续两次构建 SHA 相同），bundle 可以提交并校验。

升级步骤：

1. 跑 vendor 脚本（依赖范围变化时同时覆盖 SDK / zod 版本）。
2. `node scripts/pi-acp-smoke.mjs`。
3. 本机真实 pi 冒烟（`initialize` + 一轮提示）。
4. 更新本文的版本表、sha256。
5. 提交 bundle 和元数据。

## 为什么旁边要放 package.json

pi-acp 在 `initialize` 里用 `readNearestPackageJson(import.meta.url)` 向上最多 6 层找 `package.json`，据此返回 `agentInfo`。如果 bundle 旁没有这个文件，会一路读到仓库根目录的 `package.json`，UI 就会显示 `acp-pi-gui@0.1.0`。

因此脚本生成一个最小的 `gui/resources/agents/pi-acp/package.json`（`name: @geohar/pi-acp`，`version` 与 VERSION 一致）。它不参与 npm 安装，**不要删**。

## 运行时要求

- **Node.js 22+**（构建目标 node22；启动前校验 `node --version`）。
- **pi 已安装并在 PATH 上**：pi-acp 会 spawn `pi --mode rpc`。
- pi 版本以 pinned pi-acp 的 README 为准（0.4.0 要求 0.81.0+）。
- pi 已配置模型凭据（`~/.pi/agent` 或 `PI_CODING_AGENT_DIR`）。

## PATH

GUI 从 Finder 启动时 PATH 往往不含 `/usr/local/bin`、`~/.npm-global/bin`。必须：

1. 用登录 shell 解析一次环境（如 `/bin/zsh -lc 'command -v node; command -v pi'`），或
2. 在设置里让用户显式选择 node / pi 路径。

把解析到的绝对路径和必要环境变量传给 agent 子进程。`PI_CODING_AGENT_DIR` 要透传。

## 认证

`session/new` 失败且带 `authMethods` 时，展示登录卡片：

- `terminal` 类型：pi 给出的参数是 `--terminal-login`，等价命令：

```bash
node <resources>/agents/pi-acp/pi-acp.bundle.mjs --terminal-login
```

  该进程会 spawn `pi` 并继承终端（已验证会使用 PATH 上的 `pi`）。
- 可选：用 MyGo 的 `plugins/terminal` 内嵌终端执行上面的命令，让用户直接完成 `/login`。
- 完成后重新走 `initialize` / `session/new`。

不要把 API key 写进应用配置；沿用 pi 自己的凭据存储。

## 更新

- 版本固定，不自动跟随 npm，也不在运行时检查更新。
- 更新只走 `scripts/vendor-pi-acp.sh`；不要手改 `pi-acp.bundle.mjs`。
- 更新后先跑 `node scripts/pi-acp-smoke.mjs`，再跑真实 pi 冒烟，最后跑 UI 测试。

## 常见错误

- `agentInfo.name` 显示成 `acp-pi-gui`：bundle 旁的 `package.json` 丢了。
- `ERR_MODULE_NOT_FOUND: @agentclientprotocol/sdk`：不是 bundle 启动，或 bundle 被手改 / 没打全；重跑 vendor 脚本。
- `spawn pi ENOENT`：GUI 的 PATH 没解析到 pi；用登录 shell 或设置绝对路径。
- `-32000` / `authMethods`：pi 没有可用凭据，走上面的认证引导。
- `Lock file is already being held` / `Invalid settings file`：检查 `~/.pi/agent/*.lock` 残留。

## 来源

- `@geohar/pi-acp@0.4.0`（MIT），npm 包；上游 fork：`https://github.com/georgeharker/pi-acp`，原始项目：`https://github.com/svkozak/pi-acp`。
- 包的 `src/pi-extension.ts` 是给 `pi install npm:pi-acp` 用的 extension，外部 ACP adapter 模式不需要它，未打进 bundle。
- 本仓库的 bundle 入口即该包的 `dist/index.js`。
