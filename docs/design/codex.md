# Codex 集成

## 结论：本地补丁版 codex-acp bundle + 系统 Codex CLI

Codex 使用 [agentclientprotocol/codex-acp](https://github.com/agentclientprotocol/codex-acp)（npm：`@agentclientprotocol/codex-acp`，Apache-2.0）。不自己实现适配器，也不走 npx：以官方 2.6.0 源码为基础，应用本仓库的最小补丁（自定义模型、可配置标题模型、自定义供应商），用 `scripts/vendor-codex-acp-local.sh` 打成单文件 bundle，启动时直接用绝对路径的 Node 执行。

```
resources/agents/codex-acp/
  codex-acp.bundle.mjs   本地补丁源码构建的 dist/index.js（~2.1 MiB）
  VERSION                2.6.0（上游基线，补丁不改变包版本）
  LICENSE                codex-acp 的 Apache-2.0
  licenses/              bundle 内 JS 依赖的 LICENSE
  THIRD_PARTY_NOTICES.md
  SHA256SUMS
```

固定版本与校验和：

| 包 | 版本 | 协议 |
| --- | --- | --- |
| `@agentclientprotocol/codex-acp` | `2.6.0` | Apache-2.0 |
| `@agentclientprotocol/sdk` | `2.6.0` | Apache-2.0 |
| `zod` | `4.6.7` | MIT |
| `diff` | `9.0.0` | BSD-3-Clause |
| Codex CLI（外部，不打包） | `0.169.0` 已实测 | Apache-2.0 |

`codex-acp.bundle.mjs` 的 sha256：
`3abb599baf83ff9332a47898037c8f059c68f6a06da564cbf891067f6359c614`

上游 npm 包已经发布单文件 ESM bundle，但本仓库需要几处上游没有的行为，所以 `codex-acp-main` 保存了基于 2.6.0 的源码，`scripts/vendor-codex-acp-local.sh` 先 `npm ci` / `npm run build`，再用 `npm pack` 交给 `scripts/vendor-codex-acp.sh` 做校验、许可证归档和原子替换。升级上游时先更新源码树，再重跑本地脚本；不与 npm 上的 bundle 做逐字节比较。`codex-acp-main` 由 `.gitignore` 忽略，只随本地工作区保留；仓库只提交构建出的 bundle 和元数据。

## 运行时要求

- **Node.js 22+**：启动前校验 `node --version`。
- **Codex CLI 已安装并在 PATH 上**：应用用登录 shell 解析绝对路径，然后把 `CODEX_PATH` 传给 bundle。适配器内部执行 `codex app-server`。
- Codex CLI 需要支持 `app-server`；本仓库用 `codex-cli 0.169.0` 实测通过。官方 2.6.0 的 npm 依赖声明是 `@openai/codex ^0.166.0`。
- 用户自己的 Codex 登录/配置（默认 `~/.codex/`）：应用不读取、不复制、不代登录。设置里的自定义供应商只通过子进程环境传入，不写 `~/.codex/config.toml`。

### 本地补丁

补丁只动适配器，不碰 Codex CLI：

1. **自定义模型**：`CODEX_ACP_MODELS`（JSON 数组或逗号/换行列表）把 provider 自己的 model id 追加到 `model/list` 之后；Codex 不认识的模型仍会出现在 ACP `model` 选项里，也能用 `session/set_config_option` 切换。`src/CustomModels.ts` 生成最小 `Model` 元数据（text、非默认）；`CODEX_ACP_REASONING_EFFORTS` 声明这些模型的 Thinking 档位，缺失时退回单一的 medium；会话从 Codex 配置带入的档位不在声明列表时，回退到该模型的默认档位，避免出现当前值不在选项里的 ACP 配置。provider 开启且目标是 DeepSeek（provider 名称、Base URL 或模型 id 含 deepseek）时，Go 侧还会生成 `<data>/codex-models.json` 并把路径放进 `CODEX_CONFIG.model_catalog_json`；catalog 使用 DeepSeek Flash/Pro 的工具和推理档位元数据，`contextWindow`/`autoCompactPercent` 覆盖默认 1M/90% 的上下文和自动压缩阈值，slug 用设置里的模型 id（不是脚本里的 `deepseek-flash`），因此 Codex 不再打印 fallback metadata warning（见 [codex-provider.md](codex-provider.md)）。
2. **可配置标题模型**：`CODEX_ACP_TITLE_MODEL` 优先；未设置时使用会话当前模型，最后才回退到 `gpt-5.6-luna`。这样自定义 provider 不会因为标题生成固定请求一个不存在的模型而失败。
3. **自定义供应商**：复用上游已有的 `CODEX_CONFIG` / `MODEL_PROVIDER` 支持，由 `internal/codexprovider` 生成 `model_providers.acp-gui`、`base_url`、`wire_api=responses`、`requires_openai_auth=false`，API key 走 `env_key` 指向的环境变量，不出现在命令行。供应商开关只控制路由；`model_context_window`、`model_auto_compact_token_limit`、`CODEX_ACP_MODELS`、`CODEX_ACP_REASONING_EFFORTS` / `CODEX_ACP_TITLE_MODEL` 在开关关闭时也可单独生效，便于扩展现有 Codex provider。

设置界面与字段见 [codex-provider.md](codex-provider.md)。

### 为什么不 vendor `@openai/codex`

官方 npm 包默认依赖 `@openai/codex`，它会按平台安装一个几百 MiB 的原生二进制。把整棵依赖树塞进 `.app` 会显著增大安装包，而且每个平台都要单独重新 vendor。适配器支持官方 `CODEX_PATH`：

```bash
CODEX_PATH=/absolute/path/to/codex node resources/agents/codex-acp/codex-acp.bundle.mjs
```

因此本应用只 vendor 适配器，Codex CLI 和 pi 一样由用户自备。没有 `CODEX_PATH` 时 bundle 会尝试 `require("@openai/codex/bin/codex.js")` 并失败；`CodexLaunchSpec` 在启动前就拒绝缺失的 Codex，不会走到这一步。

## 路径解析

与 pi / command-code 共用 `internal/agent.ResolveEnvironment`：

1. 登录 shell（`-lc`，必要时 `-ilc`）里 `command -v node`、`command -v codex`。
2. 找不到时查 `PATH`、npm 全局 bin、`~/.codex/bin`、`~/.local/bin`、Homebrew 等常见目录。
3. 环境变量覆盖：`ACP_GUI_NODE`、`ACP_GUI_CODEX`（必须绝对路径）；`ACP_GUI_SHELL` 可换登录 shell。
4. `settings.json` 的 `paths.node` / `paths.codex` 是第二优先级；无效覆盖保留 shell 结果并警告。

启动时把 node 和 codex 所在目录补进子进程 `PATH`，并写入 `CODEX_PATH=<绝对路径>`。状态栏显示 `codex <CLI 版本>`；不显示 bundle 的 `VERSION`，因为 `initialize.agentInfo.version` 是适配器版本。

## ACP 行为

已用本地补丁 bundle + 本机 Codex CLI 验证：

- `initialize`：`agentInfo.name = "@agentclientprotocol/codex-acp"`、`title = "Codex"`、`version = 适配器版本`；`authMethods` 包含 `api-key` / `chat-gpt`；`agentCapabilities.promptCapabilities` 支持 `image`、`embeddedContext`，`loadSession=true`。
- `session/new` 返回 `sessionId`、`configOptions`（`mode`、`collaboration_mode`、`model`、`reasoning_effort`、`fast-mode`）、真实 `modes` 和旧式 `models`。启用自定义供应商后，`CODEX_ACP_MODELS` 里的模型会追加到 `model` 选项，当前模型来自 `CODEX_CONFIG.model`；切到自定义模型时 `reasoning_effort` 的选项来自 `CODEX_ACP_REASONING_EFFORTS`。
- `reasoning_effort` 以 `category: thought_level` 暴露，GUI 的 Thinking 列直接使用；model、mode 等选项全部以 agent 返回为准，不硬编码。
- session mode 使用 `session/set_mode`（`read-only` / `workspace-write` / `agent` / `agent-full-access`）；`mode` config option 与服务端 `modes` 是同一语义，GUI 的 mode chip 走 `modes`。
- `session/prompt` 流式返回 text/thought/tool call；`session/cancel`、权限请求走现有 ACP 客户端。
- 适配器会推送 `_auth/status_update` 扩展通知；GUI 对未知通知安静忽略，不当作协议错误。
- `session/list` / `session/load` / `session/close` 可用；历史重放按通用 ACP 路径处理。
- 适配器是长驻进程，会再 spawn 一个 `codex app-server` 子进程；`internal/session` 用进程组退出清理，一起杀掉。

bundle 的 AIR 扩展、steering、subagent、goal 等能力没有在 GUI 里实现；应用只使用标准 ACP 方法，未实现的扩展能力不影响基础聊天。

## 认证

应用不处理登录：

- `initialize` 返回 `authMethods`：API Key、ChatGPT。连接要求认证时只展示名称、描述和可复制命令。
- 登录提示固定为 `codex login`，由用户在外部终端执行；应用不代跑、不轮询、不打开终端。
- 也可由用户在外部设置 `CODEX_API_KEY` / `OPENAI_API_KEY`；应用只在用户主动保存自定义供应商设置时保存其填写的 key（仅 provider 开关开启时传给 Codex），且只通过子进程环境传递。
- `codex-acp` 会打开浏览器完成 ChatGPT 登录，但那需要客户端主动调用 `authenticate`；本应用不调用，保持“只展示”。

## 日志

Codex CLI / 适配器的 stderr 由会话收集，写入 `<data>/logs/codex.log`（1 MiB 滚动）。连接时的命令、`CODEX_PATH` 和 bundle 路径也记录在日志里（当前没有查看面板）。

## 构建 / 升级

```bash
scripts/vendor-codex-acp-local.sh          # 本仓库补丁源码，日常重打
CODEX_ACP_SKIP_INSTALL=1 scripts/vendor-codex-acp-local.sh  # 复用 node_modules
scripts/vendor-codex-acp.sh                # 官方 2.6.0，不含补丁
scripts/vendor-codex-acp.sh 2.6.1          # 指定官方版本
CODEX_ACP_SOURCE=/path/to/pkg.tgz scripts/vendor-codex-acp.sh
node scripts/codex-acp-smoke.mjs           # initialize（需要 codex，不调用模型）
```

本地脚本流程：`npm ci` → `npm run build` → `npm pack` → 官方 vendor 脚本的临时目录 `npm install --omit=optional` → 校验 package/version → `node --check` bundle → `node bundle --version` → 验证仍支持 `CODEX_PATH` → 原子替换 `resources/agents/codex-acp/` → 生成 `licenses/`、`THIRD_PARTY_NOTICES.md`、`SHA256SUMS`。任何一步失败都保留旧 bundle。

升级步骤：

1. 更新 `codex-acp-main` 到目标上游版本，保留本地补丁并跑 `npm test` / `npm run typecheck`。
2. `scripts/vendor-codex-acp-local.sh`。
3. `node scripts/codex-acp-smoke.mjs`。
4. 真实 Codex 冒烟：`ACP_REAL_CODEX=1 go test ./internal/session -run TestRealCodex -v -count=1`（会调用模型、消耗额度）。
5. 更新本文的版本表、sha256 和 [discovery.md](discovery.md)。
6. 提交 bundle 和元数据。

不要在启动时 `npm install` / `npx` / 重新 bundle。

## 常见错误

- `codex: the Codex CLI was not found...`：`codex` 不在登录 shell PATH；安装 Codex CLI 或设置 `ACP_GUI_CODEX` / `paths.codex`。
- `Cannot find module '@openai/codex/bin/codex.js'`：不是通过 `CodexLaunchSpec` 启动，子进程缺少 `CODEX_PATH`；重跑正式启动路径，不要直接裸跑 bundle。
- `Node.js 22+ required`：升级 Node，或设置 `ACP_GUI_NODE`。
- `authMethods` / `-32000`：Codex 未登录或模型不可用；在外部执行 `codex login` 后重连。
- 启动后立即退出：看 `logs/codex.log`，常见原因是 Codex CLI 版本过旧或 `codex app-server` 不可用。

## 来源

- 官方适配器：https://github.com/agentclientprotocol/codex-acp
- npm：`@agentclientprotocol/codex-acp@2.6.0`（Apache-2.0）
- Codex CLI：https://github.com/openai/codex（Apache-2.0）
- 本仓库 bundle 由 `scripts/vendor-codex-acp-local.sh` 从本地补丁源码 `codex-acp-main` 构建；补丁清单见上文，构建过程不在启动时执行。
