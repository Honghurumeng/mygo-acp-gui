# Agent 发现

## 官方 registry

ACP 有官方 agent registry：

- 运行时 JSON：`https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json`
- Schema：`https://github.com/agentclientprotocol/registry/blob/main/agent.schema.json`
- 仓库：`https://github.com/agentclientprotocol/registry`

当前快照（2026-11 观测）：约 45 个 agent，schema `v1.0.0`。条目包含：

```json
{
  "id": "codex-acp",
  "name": "Codex",
  "version": "2.6.0",
  "description": "ACP adapter for OpenAI's coding assistant",
  "license": "Apache-2.0",
  "icon": "https://cdn.agentclientprotocol.com/registry/v1/latest/codex-acp.svg",
  "distribution": {
    "npx": { "package": "@agentclientprotocol/codex-acp@2.6.0" },
    "binary": {
      "darwin-aarch64": { "archive": "...", "sha256": "...", "cmd": "./binary", "args": [] }
    }
  }
}
```

要点：

- `binary.cmd` 是**解压后的相对路径**，不是 PATH 命令；不能直接拿它做“已安装”检测。
- `npx` / `uvx` 是包名，适合安装或按需运行，不等于本机已安装。
- 注册表负责“有哪些 agent、怎么装”，**不负责“本机装没装”**；后者由本应用解决。
- registry 里的 `pi-acp` 指向上游；本应用对 pi 使用 vendored 的 `@geohar/pi-acp`，需要覆盖该条目。
- 建议把 registry 快照打进 `resources/registry.json`；启动先读快照，联网刷新失败时用 last-known-good。

## 启动描述（launch spec）

每个可探测 agent 需要一条本地启动描述，registry 元数据只是补充：

```json
{
  "id": "command-code",
  "name": "Command Code",
  "command": "command-code",
  "args": ["acp"],
  "env": {},
  "detect": ["command-code"],
  "versionArgs": ["--version"],
  "source": "builtin",
  "registryId": null,
  "installHint": "npm i -g command-code"
}
```

`source` 取值：`builtin`（手工维护）、`registry`（从 registry 生成）、`user`（用户添加）、`vendored`（随应用分发，如 pi）。

首批建议支持（命令以实际探测为准，不要只信名字）：

| id | 候选命令 / 参数 | 说明 |
| --- | --- | --- |
| `pi` | vendored `node .../pi-acp/dist/index.js` | 特例，见 [pi.md](pi.md) |
| `command-code` | `command-code acp` | 本机已验证；不在官方 registry 中 |
| `opencode` | `opencode acp` | registry binary 参数 |
| `goose` | `goose acp` | registry binary 参数 |
| `cursor` | `cursor-agent acp` | registry binary 参数 |
| `gemini` | `gemini --acp` | registry npx 参数 |
| `qwen-code` | `qwen --acp --experimental-skills` | registry npx 参数 |
| `auggie` | `auggie --acp` | registry npx 参数 |
| `cline` | `cline --acp` | registry npx 参数 |
| `codebuddy-code` | `codebuddy --acp` | registry npx 参数 |
| `claude-acp` | `claude-agent-acp` / npx `@agentclientprotocol/claude-agent-acp` | 不是 `claude` 本身 |
| `codex-acp` | `codex-acp` / npx `@agentclientprotocol/codex-acp` | 不是 `codex` 本身 |
| `github-copilot` | npx `@github/copilot-language-server --acp` | 以实际 bin 为准 |

注意：

- **`pi` 本身不是 ACP agent**，必须有 `pi-acp` 适配器。
- **`codex` 本身不是 ACP agent**，需要 `@agentclientprotocol/codex-acp`。
- 用户自定义 agent 必须支持“命令 + 参数 + 环境变量 + 工作目录”的编辑和测试。

## 候选生成顺序

对 catalog 中每个条目：

1. 用户自定义覆盖（最高优先级）
2. 应用托管目录（`mygo.App.Path(mygo.PathData)` 下的 agents）
3. `resources/agents/...`（vendored，如 pi）
4. PATH 中的已知命令（`exec.LookPath`）
5. 全局包管理器安装的包：
   - `npm ls -g --json --depth=0` + `npm root -g`，读包 `package.json` 的 `bin` 映射
   - `pnpm ls -g --json`、`bun pm ls -g`、`uv tool list`（按需）
6. （可选，后续）导入 Zed `~/.config/zed/settings.json` 的 `agent_servers`

去重后得到候选列表。每个候选保存**绝对路径**，不要只存命令名。

## 探测（probe）

“是不是 ACP agent”的唯一可靠判断是握手。探测流程：

1. spawn 候选进程（cwd 用用户工作区或临时目录），继承登录 shell 环境。
2. 写一行：

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":false,"writeTextFile":false},"terminal":false}}}
```

3. 等待 stdout 上的一行 JSON，超时默认 **5 秒**。
4. 判定：
   - 有 `result.protocolVersion` / `result.agentInfo` / `result.agentCapabilities` → **ACP OK**
   - 返回 JSON-RPC `error` → 记录错误，标记失败
   - stdout 出现非 JSON → 标记“不是 ACP / 输出污染”
   - 超时 / 退出 → 标记失败，保留 stderr 末尾
5. 关闭 stdin，先 SIGTERM，宽限后杀**进程组**。
6. 缓存：绝对路径、文件 mtime、版本、`agentInfo`、`capabilities`、`authMethods`、探测时间、结果。
   - 文件变了或用户点“重新检测”就重新探测。
   - 失败结果也要有 TTL，避免每次启动都等超时。

探测时声明 `fs: false`、`terminal: false`，与本应用实际能力一致；不要为了“探测成功”而声明用不到的能力。

`session/new` 是第二层验证（是否已登录、能否真正开会话），只在用户选中 agent / 点“连接”时做，不在启动扫描时对每个 agent 都做。`session/new` 返回 `-32000` 或 `auth_required` 时进入“需要登录”状态，展示 agent 的 `authMethods`。

## 状态模型

| 状态 | 含义 | 动作 |
| --- | --- | --- |
| `ready` | 探测成功，已知路径和版本 | 可连接 |
| `needs-auth` | `initialize` 成功，但 `session/new` 要求认证 | 展示登录/配置指引 |
| `installed-not-acp` | 命令存在但握手失败 | 显示 stderr，提示不是 ACP |
| `not-installed` | registry 有，但本机没找到 | 显示安装命令 |
| `error` | 超时/崩溃/协议错误 | 重试、查看日志 |
| `custom` | 用户手动添加 | 可编辑、可删除、可测试 |

## 手动命令行测试

macOS/Linux（需有 `timeout`；否则用 probe 脚本）：

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":false,"writeTextFile":false},"terminal":false}}}' \
  | timeout 5 command-code acp
```

Windows PowerShell：

```powershell
'{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":false,"writeTextFile":false},"terminal":false}}}' `
  | & command-code acp
```

预期看到一行 JSON：

```json
{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentInfo":{"name":"Command Code","version":"1.88.5"},"agentCapabilities":{"loadSession":true,"promptCapabilities":{"audio":false,"image":true,"embeddedContext":true}},"authMethods":[{"id":"command-code-cli","name":"Command Code CLI login","description":"Sign in once with `cmd login` in a terminal; this session reuses that account."}]}}
```

`command-code` 会把 `[acp] initialize: ...` 写到 stderr。stdout 必须只有协议 JSON；探测时 stderr 单独收集，不能当作响应。

建议补一个跨平台 `scripts/probe-agent.mjs`（或 Go 版 `cmd/probe`）：发送 `initialize`、5 秒超时、打印 result/stderr、杀掉进程组。

## 缓存与刷新

- 缓存文件：`probe-cache.json`，按绝对路径索引。
- 失效条件：mtime 变化、版本变化、用户手动刷新、TTL 过期。
- registry 刷新：内置快照 + 成功联网后写回缓存；失败静默回退。
- 探测失败也缓存（带 TTL），避免每次启动都等 5 秒。

## 安全

- 只探测目录中已知或被用户显式添加的命令，不遍历全盘。
- 每次探测都必须能杀掉进程组。
- 不自动安装 registry agent；MVP 只展示安装命令。
- 不把 API key 写入应用配置或日志。
