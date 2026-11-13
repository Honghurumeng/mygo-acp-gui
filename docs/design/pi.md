# pi 集成

## 结论：单文件 bundle，直接引用

pi-acp 已按「单文件 bundle」方式 vendor 到仓库，构建一次后固定使用：

```
resources/agents/pi-acp/
  pi-acp.bundle.mjs      唯一运行时入口（~456 KiB）
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
`61f648af1c1a4365904ce076ac78c4bb582db566d106ac59a2a93990c25bb8ee`

当前 bundle 来自本地 fork（`pi-acp-main`，上游 0.4.0 + usage/推理强度/thinking/图片回放/会话标题/工具标题补丁），不是 npm 上的原版 0.4.0；差异和重打步骤见 [pi-fork.md](pi-fork.md)。

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
- 单文件 bundle：离线可用、启动确定、体积小（456 KiB）；代价只是升级时要跑一次脚本，已自动化。

## 构建 / 升级

有两个入口：

- `scripts/vendor-pi-acp.sh`：从 npm registry 或指定 tarball 打包，适合升级上游版本。
- `scripts/vendor-pi-acp-local.sh`：构建本地 fork `pi-acp-main` 并调用上面的脚本，日常修改 pi-acp 后用这个。

```bash
scripts/vendor-pi-acp-local.sh              # 本地 fork：npm ci -> build -> pack -> vendor
PI_ACP_SKIP_INSTALL=1 scripts/vendor-pi-acp-local.sh  # 复用 node_modules，快速重打
scripts/vendor-pi-acp.sh                    # npm 上的 @geohar/pi-acp 0.4.0
scripts/vendor-pi-acp.sh 0.4.1              # 指定上游版本
node scripts/pi-acp-smoke.mjs               # 单独复跑 initialize 冒烟
```

脚本流程：临时目录 `npm install --save-exact`（本地 fork 先 `npm ci` + `npm run build` + `npm pack`）→ esbuild `--bundle --platform=node --format=esm --target=node22` → `node --check` → initialize 冒烟 → 原子替换 `resources/agents/pi-acp/` → 生成 NOTICE 和 `SHA256SUMS`。任何一步失败都会保留旧 bundle。

直接依赖版本锁在脚本里，可用环境变量覆盖：

```bash
PI_ACP_VERSION=0.4.0 ACP_SDK_VERSION=0.26.0 ZOD_VERSION=3.25.76 \
  ESBUILD_VERSION=0.28.2 scripts/vendor-pi-acp.sh
```

本地 tarball 也可以手动接入：

```bash
(cd pi-acp-main && npm ci && npm run build && npm pack --pack-destination /tmp)
PI_ACP_SOURCE=/tmp/geohar-pi-acp-0.4.0.tgz \
  PI_ACP_SOURCE_LABEL='local fork (pi-acp-main)' \
  scripts/vendor-pi-acp.sh 0.4.0
```

构建使用 `--minify-whitespace --legal-comments=none` 去掉 esbuild 生成的临时目录注释，保证同样输入产出同样字节（已验证连续两次构建 SHA 相同），bundle 可以提交并校验。本地 fork 的 tarball 路径不会写进 NOTICE，来源固定显示为 `local fork (pi-acp-main)`。

升级步骤：

1. 改 `pi-acp-main` 后跑 `scripts/vendor-pi-acp-local.sh`；升级上游版本则拉新源码、合并补丁后再跑。
2. `node scripts/pi-acp-smoke.mjs`。
3. 本机真实 pi 冒烟（`ACP_REAL_PI=1 go test ./internal/session -run TestRealPi -v -count=1`，会断言 usage）。
4. 更新本文的版本表、sha256 和 [pi-fork.md](pi-fork.md)。
5. 提交 bundle 和元数据。

## 为什么旁边要放 package.json

pi-acp 在 `initialize` 里用 `readNearestPackageJson(import.meta.url)` 向上最多 6 层找 `package.json`，据此返回 `agentInfo`。如果 bundle 旁没有这个文件，会一路读到上层目录的 `package.json`，UI 就会显示错误的 `agentInfo.name/version`。

因此脚本生成一个最小的 `resources/agents/pi-acp/package.json`（`name: @geohar/pi-acp`，`version` 与 VERSION 一致）。它不参与 npm 安装，**不要删**。

## 运行时要求

- **Node.js 22+**（构建目标 node22；启动前校验 `node --version`）。
- **pi 已安装并在 PATH 上**：pi-acp 会 spawn `pi --mode rpc`。
- pi 版本以 pinned pi-acp 的 README 为准（0.4.0 要求 0.81.0+）。
- pi 已配置模型凭据（`~/.pi/agent` 或 `PI_CODING_AGENT_DIR`）。
- Node 与 pi 都由用户自备：应用不捆绑、不下载、不安装、不升级 Node，也不检测或代管模型凭据/配额。

## PATH

GUI 从 Finder 启动时 PATH 往往不含 `/usr/local/bin`、`~/.npm-global/bin`。`internal/agent` 已实现：

1. 用 `$SHELL -lc`（默认 zsh/bash/sh，5 秒超时）导出登录 shell 的完整 NUL 分隔环境，再用 `command -v node` / `command -v pi` 解析绝对路径。若 PATH 只写在 `.zshrc` 这类交互配置里，再用 `-ilc` 重试一次；仍找不到则检查 npm 全局 bin 和常见安装目录。
2. 支持显式覆盖：`ACP_GUI_NODE`、`ACP_GUI_PI`（必须是绝对路径），`ACP_GUI_SHELL` 可换登录 shell。
3. 启动前校验 Node 22+、bundle 存在、pi 可执行；把 node/pi 所在目录补进子进程 PATH。
4. 把解析到的完整环境和绝对路径传给 agent 子进程；`PI_CODING_AGENT_DIR` 等变量自然透传。
5. 状态栏显示 Node/pi 版本；解析失败不阻塞窗口，错误显示在工具栏并可重试（重启/重新连接）。

这里只负责“找到已有的 Node/pi”，不做版本管理或安装引导。未实现：设置界面里的选择器；目前用 `settings.json` 的 `paths.node` / `paths.pi` 或环境变量覆盖。

## 模型与推理强度

pi-acp 的 model `configOption` 来自 pi RPC `get_available_models`；`thought_level` 不再写死，而是来自 `get_available_thinking_levels`，因此选项随当前模型变化（pi 1.5+ 可能是 `low/high/max`，也可能带 `off/minimal/medium/xhigh`）。当前值来自 `get_state.thinkingLevel`。

切换模型后，pi-acp 会重读模型、thinking levels 和当前 thinking level，返回新的 `configOptions`；同时用 `current_mode_update` 同步 thinking。旧 pi 没有 `get_available_thinking_levels` 时回退到 `state.model.thinkingLevelMap`，再回退到旧版固定列表。

GUI 以 `thought_level` config option 为唯一 thinking 来源：`session/set_config_option` 的响应和 `config_option_update` 都整组替换 options/currentValue。因为 pi 只报告当前模型的档位，配置探测会在同一个临时会话里对模型列表逐个 `set_config_option`，把每个模型返回的 `configOptions` 存进 `<data>/model-config.json`（见 [discovery.md](discovery.md)）；下次启动只扫新模型，草稿因此能直接显示各模型的档位。pi-acp v1 为了兼容旧客户端，还会把同一组 thinking levels 放进 `modes` 并发送 `current_mode_update`；这不是权限/session mode。GUI 在读取 Info 时，如果 `modes` 的每个 id 都是 `thought_level` 的选项值，就丢弃该 `modes` 块和对应的 `currentModeId`，因此不会出现一个模型切换后不刷新、且与 thinking 列表重复的 “Session mode: Thinking: …” 菜单。`session/set_mode` 仍保留给 Command Code 这类有真实模式语义的 agent。

## 思考内容

推理文本走 `agent_thought_chunk`：实时流由 `message_update.assistantMessageEvent.type === 'thinking_delta'` 映射，历史恢复（`session/load`）由 fork 的 `normalizePiAssistantBlocks()` 从持久化的 `{ type: 'thinking', thinking }` 块回放（上游只回放 text 块，会丢掉推理）。GUI 把它画成回答上方的「Thinking」折叠块，默认收起；两条路径用的是同一个 update 类型，所以恢复会话后的显示和刚跑完时一致。

## 用量

vendored pi-acp fork 在每轮 `session/prompt` 结束后做两件事：

- 调 pi RPC `get_session_stats`，把 `contextUsage.tokens/contextWindow` 发成 ACP `usage_update`（有 `cost` 时按 USD 附带累计费用），UI 显示 composer 右侧的上下文环和 used/size。
- 从 pi `message_update.usage` 读取本轮 input/output/cache/reasoning/totalTokens，映射为 `PromptResponse.usage`，UI 显示 `turn 1.2k in · 100 out · 40 thought`。

`/compact` 结束后也会立即刷新一次 `usage_update`；`contextUsage.tokens` 为 `null`（刚压缩完）或查询失败时跳过，不影响对话。上游 npm `@geohar/pi-acp@0.4.0` 没有这两处映射，因此必须使用本仓库 fork 打出的 bundle；客户端不需要改。

## 认证

**应用不处理登录**：不实现登录流程、不内嵌终端、不代跑登录命令、不检测凭据是否可用。模型可用性（凭据、订阅、配额）完全由用户自己解决。

ACP 没有独立的 provider 可用性接口。pi-acp 的 model `configOptions` 来自 pi RPC `get_available_models`，只包含已配置凭据的 provider；它是 pi 自己的声明，不是网络健康检查。GUI 只按这份列表展示 provider，打开模型面板时会重读，登录或改配置后列表会跟着变；已配置但已失效的 key/token 仍可能被 pi 列出，只有真正请求时才会报错。

`session/new` 失败且带 `authMethods` 时只做展示：

- 列出每个 method 的 `name` / `description`；`terminal` 类型把 `args` 拼成可复制的命令行，例如：

```bash
node <resources>/agents/pi-acp/pi-acp.bundle.mjs --terminal-login
```

- 提示“请在终端执行后重新连接”；应用不代跑、不监听、不轮询登录结果。用户重新连接时才重新走 `initialize` / `session/new`。
- 不使用 MyGo `plugins/terminal`，也不声明 ACP `terminal` 能力。
- 不要把 API key 写进应用配置；沿用 pi 自己的凭据存储。

## 更新

- 版本固定，不自动跟随 npm，也不在运行时检查更新。
- 更新只走 `scripts/vendor-pi-acp-local.sh`（本地 fork）或 `scripts/vendor-pi-acp.sh`（上游）；不要手改 `pi-acp.bundle.mjs`。
- 更新后先跑 `node scripts/pi-acp-smoke.mjs`，再跑真实 pi 冒烟，最后跑 UI 测试。

## 常见错误

- `agentInfo.name` 显示成 `acp-pi-gui`：bundle 旁的 `package.json` 丢了。
- `ERR_MODULE_NOT_FOUND: @agentclientprotocol/sdk`：不是 bundle 启动，或 bundle 被手改 / 没打全；重跑 vendor 脚本。
- `spawn pi ENOENT`：GUI 的 PATH 没解析到 pi；用登录 shell 或设置绝对路径。
- `-32000` / `authMethods`：pi 没有可用凭据；应用只展示 `authMethods` 和命令，由用户自行在终端登录后重连。
- `Lock file is already being held` / `Invalid settings file`：检查 `~/.pi/agent/*.lock` 残留。

## 来源

- 上游 `@geohar/pi-acp@0.4.0`（MIT），npm 包；上游 fork：`https://github.com/georgeharker/pi-acp`，原始项目：`https://github.com/svkozak/pi-acp`。
- 本仓库 bundle 由本地 fork `pi-acp-main` 打出：上游 0.4.0 + usage 映射补丁（见 [pi-fork.md](pi-fork.md)）。`pi-acp-main` 被 `.gitignore` 忽略，不随主仓库提交。
- 包的 `src/pi-extension.ts` 是给 `pi install npm:pi-acp` 用的 extension，外部 ACP adapter 模式不需要它，未打进 bundle。
- 本仓库的 bundle 入口即该包的 `dist/index.js`。
