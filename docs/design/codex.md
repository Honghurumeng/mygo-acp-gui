# Codex 集成

## 结论：官方 codex-acp bundle + 系统 Codex CLI

Codex 使用 [agentclientprotocol/codex-acp](https://github.com/agentclientprotocol/codex-acp)（npm：`@agentclientprotocol/codex-acp`，Apache-2.0）。不自己实现适配器，也不走 npx：把官方发布的单文件 `dist/index.js` 原样 vendor 成 bundle，启动时直接用绝对路径的 Node 执行。

```
resources/agents/codex-acp/
  codex-acp.bundle.mjs   官方 dist/index.js，逐字节保留（~2.1 MiB）
  VERSION                2.6.0
  LICENSE                codex-acp 的 Apache-2.0
  licenses/              bundle 内 JS 依赖的 LICENSE
  THIRD_PARTY_NOTICES.md
  SHA256SUMS
```

固定版本与校验和：

| 包 | 版本 | 协议 |
| --- | --- | --- |
| `@agentclientprotocol/codex-acp` | `2.6.0` | Apache-2.0 |
| `@agentclientprotocol/sdk` | `2.4.0` | Apache-2.0 |
| `zod` | `4.6.7` | MIT |
| `diff` | `9.0.0` | BSD-3-Clause |
| Codex CLI（外部，不打包） | `0.169.0` 已实测 | Apache-2.0 |

`codex-acp.bundle.mjs` 的 sha256：
`733b7521c2a437a34ed016d45a6f46bebff2bac123a172467574ee827db98b99`

与 pi-acp 不同，codex-acp 的上游 npm 包已经发布单文件 ESM bundle。`scripts/vendor-codex-acp.sh` 只做安装、校验、复制、许可证归档和冒烟，不再用 esbuild 重打；这样 bundle 与官方发布物一致，升级时差异只看 VERSION。官方 bundle 自带 `package_default`，不需要旁边的 `package.json` 才能报告 `agentInfo.name/version`。

`internal/agent/codex_prompt.txt` 是生成模型目录时用的 `base_instructions`，内容取自 Codex 自带 catalog 的 coding prompt（Codex 仓库为 Apache-2.0）；它只用于本应用生成的自定义 provider 目录，不修改官方 bundle。升级 Codex CLI 后如果目录格式变了，先对比新版自带 prompt 再更新这个文件。

## 运行时要求

- **Node.js 22+**：启动前校验 `node --version`。
- **Codex CLI 已安装并在 PATH 上**：应用用登录 shell 解析绝对路径，然后把 `CODEX_PATH` 传给 bundle。适配器内部执行 `codex app-server`。
- Codex CLI 需要支持 `app-server`；本仓库用 `codex-cli 0.169.0` 实测通过。官方 2.6.0 的 npm 依赖声明是 `@openai/codex ^0.166.0`。
- 用户自己的 Codex 登录/配置（默认 `~/.codex/`）：应用不读取、不复制、不代登录。

### 为什么不 vendor `@openai/codex`

官方 npm 包默认依赖 `@openai/codex`，它会按平台安装一个几百 MiB 的原生二进制。把整棵依赖树塞进 `.app` 会显著增大安装包，而且每个平台都要单独重新 vendor。适配器支持官方 `CODEX_PATH`：

```bash
CODEX_PATH=/absolute/path/to/codex node resources/agents/codex-acp/codex-acp.bundle.mjs
```

因此本应用只 vendor 适配器，Codex CLI 和 pi 一样由用户自备。没有 `CODEX_PATH` 时 bundle 会尝试 `require("@openai/codex/bin/codex.js")` 并失败；`CodexLaunchSpec` 在启动前就拒绝缺失的 Codex，不会走到这一步。

## 可选的供应商 / 模型覆盖

设置面板的 `Codex` 分类只做一层可选覆盖，不改用户自己的 Codex 配置：

- 开关关闭（默认）：不注入任何 `CODEX_CONFIG`，Codex 完全按 `~/.codex/config.toml` 的 `model_provider` / `model` 运行。
- 开关打开：把 provider 定义和 model 通过 `CODEX_CONFIG` 交给 codex-acp，由它转成 `codex app-server` 的 `-c` 参数。`~/.codex/config.toml` 不读、不写、不备份；登录、MCP、skills、sandbox、审批仍由 Codex 自己加载。
- 字段：provider 名称、Responses `base_url`、model slug、context window、reasoning levels、API key 环境变量名、可选的直接 API key、是否复用 Codex 登录（`requires_openai_auth`），以及可选的 `model_catalog_url`。provider id 在 `settings.json` 里规范化为 `[a-z0-9_-]+`，缺省 `acp-gui`。
- `base_url` 会规范化为以 `/v1` 结尾：输入 `https://api.deepseek.com` 保存为 `https://api.deepseek.com/v1`；已带 `/v1` 的保持不变。
- API key 有两种输入：
  - **环境变量名**（如 `DEEPSEEK_API_KEY`）：只保存变量名，key 由登录 shell 提供；设置面板会提示变量是否缺失。
  - **直接 key**：保存在 `settings.json`（0600），启动 Codex 时注入子进程环境，环境变量名用上面的名字；名字为空时用 `OPENAI_API_KEY`。直接 key 优先于环境变量，且不会写进 `CODEX_CONFIG`、命令行或日志。
  - 复用 Codex 登录时不设置 `env_key`，两种 key 输入都不使用。
- 应用时机：点 Apply 后保存到 `settings.json` 的 `codex`，并重建 Codex 的 launch spec；新会话生效。已连接的会话仍使用启动时的 app-server，需要重连（当前没有重连按钮，可新建会话）。
- 模型目录：不填 `model_catalog_url` 时，应用在 `127.0.0.1` 随机端口生成一份 Codex `{"models":[...]}` 目录，并通过 provider 定义注入。目录包含当前 model、context window、reasoning levels 和 Codex 的 `base_instructions`，因此 Codex 不再对未知 slug 打印 `Model metadata ... not found`，对话内“推理强度”菜单也会出现。
- `Reasoning levels` 是逗号分隔的 effort 列表（缺省 `low, medium, high`）；留空表示不声明 reasoning，Codex 不显示推理强度，也不会在请求里带 `reasoning`。`Context window` 缺省 `200000`，用于上下文统计和自动压缩，应不超过供应商真实上限。
- 目录服务只在本应用进程内运行，随 `Shutdown` 关闭；Codex 会把拉到的目录缓存到 `~/.codex/models_cache.json`（只写缓存，不改 `config.toml`），每次 app-server 启动会重新拉取。
- 当前仍只支持一个 provider、一个 model；多 model 需要扩展设置和 `agent.CodexCatalogJSON`，自定义 `http_headers`、query params、多 provider 配置还没有 UI。

`CODEX_CONFIG` 的结构示例：

```json
{
  "model_provider": "acp-gui",
  "model": "deepseek-chat",
  "model_providers": {
    "acp-gui": {
      "name": "DeepSeek",
      "base_url": "https://api.deepseek.com/v1",
      "wire_api": "responses",
      "env_key": "DEEPSEEK_API_KEY"
    }
  }
}
```

codex-acp 启动时读 `CODEX_CONFIG`；provider 定义里的 `model_catalog_url` 会一起传给 app-server（本机 0.169.0 实测）。Codex 的 `WireApi` 仍然只有 `responses`，本功能不做 Chat Completions 转换。

## 路径解析

与 pi / command-code 共用 `internal/agent.ResolveEnvironment`：

1. 登录 shell（`-lc`，必要时 `-ilc`）里 `command -v node`、`command -v codex`。
2. 找不到时查 `PATH`、npm 全局 bin、`~/.codex/bin`、`~/.local/bin`、Homebrew 等常见目录。
3. 环境变量覆盖：`ACP_GUI_NODE`、`ACP_GUI_CODEX`（必须绝对路径）；`ACP_GUI_SHELL` 可换登录 shell。
4. `settings.json` 的 `paths.node` / `paths.codex` 是第二优先级；无效覆盖保留 shell 结果并警告。

启动时把 node 和 codex 所在目录补进子进程 `PATH`，并写入 `CODEX_PATH=<绝对路径>`。状态栏显示 `codex <CLI 版本>`；不显示 bundle 的 `VERSION`，因为 `initialize.agentInfo.version` 是适配器版本。

## ACP 行为

已用官方 bundle + 本机 Codex CLI 验证：

- `initialize`：`agentInfo.name = "@agentclientprotocol/codex-acp"`、`title = "Codex"`、`version = 适配器版本`；`authMethods` 包含 `api-key` / `chat-gpt`；`agentCapabilities.promptCapabilities` 支持 `image`、`embeddedContext`，`loadSession=true`。
- `session/new` 返回 `sessionId`、`configOptions`（`mode`、`collaboration_mode`、`model`、`reasoning_effort`、`fast-mode`）、真实 `modes` 和旧式 `models`。
- `reasoning_effort` 以 `category: thought_level` 暴露，GUI 的 Thinking 列直接使用；model、mode 等选项全部以 agent 返回为准，不硬编码。
- 自定义 provider + 生成的模型目录（`model` 设为 `deepseek/deepseek-v4.1-flash`，目录由 `agent.CodexCatalogJSON` 生成）：`session/new` 的模型菜单只包含该 slug，`reasoning_effort` 出现且 `currentModelId` 为 `deepseek/deepseek-v4.1-flash[medium]`；不再出现 `Model metadata ... not found`。
- session mode 使用 `session/set_mode`（`read-only` / `workspace-write` / `agent` / `agent-full-access`）；`mode` config option 与服务端 `modes` 是同一语义，GUI 的 mode chip 走 `modes`。
- `session/prompt` 流式返回 text/thought/tool call；`session/cancel`、权限请求走现有 ACP 客户端。
- 适配器会推送 `_auth/status_update` 扩展通知；GUI 对未知通知安静忽略，不当作协议错误。
- `session/list` / `session/load` / `session/close` 可用；历史重放按通用 ACP 路径处理。
- 适配器是长驻进程，会再 spawn 一个 `codex app-server` 子进程；`internal/session` 用进程组退出清理，一起杀掉。

官方 bundle 的 AIR 扩展、steering、subagent、goal 等能力没有在 GUI 里实现；应用只使用标准 ACP 方法，未实现的扩展能力不影响基础聊天。

## 认证

应用不处理登录：

- `initialize` 返回 `authMethods`：API Key、ChatGPT。连接要求认证时只展示名称、描述和可复制命令。
- 登录提示固定为 `codex login`，由用户在外部终端执行；应用不代跑、不轮询、不打开终端。
- 也可由用户在外部设置 `CODEX_API_KEY` / `OPENAI_API_KEY`；应用不读取、不保存 key。
- `codex-acp` 会打开浏览器完成 ChatGPT 登录，但那需要客户端主动调用 `authenticate`；本应用不调用，保持“只展示”。

## 日志

Codex CLI / 适配器的 stderr 由会话收集，写入 `<data>/logs/codex.log`（1 MiB 滚动）。连接时的命令、`CODEX_PATH` 和 bundle 路径也记录在日志里（当前没有查看面板）。

## 构建 / 升级

```bash
scripts/vendor-codex-acp.sh              # 默认 2.6.0
scripts/vendor-codex-acp.sh 2.6.1        # 指定官方版本
CODEX_ACP_SOURCE=/path/to/pkg.tgz scripts/vendor-codex-acp.sh
node scripts/codex-acp-smoke.mjs          # initialize（需要 codex，不调用模型）
```

脚本流程：临时目录 `npm install --omit=optional` → 校验 package/version → `node --check` bundle → `node bundle --version` → 验证仍支持 `CODEX_PATH` → 原子替换 `resources/agents/codex-acp/` → 生成 `licenses/`、`THIRD_PARTY_NOTICES.md`、`SHA256SUMS`。任何一步失败都保留旧 bundle。

升级步骤：

1. `scripts/vendor-codex-acp.sh <version>`。
2. `node scripts/codex-acp-smoke.mjs`。
3. 真实 Codex 冒烟：`ACP_REAL_CODEX=1 go test ./internal/session -run TestRealCodex -v -count=1`（会调用模型、消耗额度）。
4. 更新本文的版本表、sha256 和 [discovery.md](discovery.md)。
5. 提交 bundle 和元数据。

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
- 本仓库 bundle 由 `scripts/vendor-codex-acp.sh` 从官方 npm 包复制；不改写逻辑，不在启动时构建。
