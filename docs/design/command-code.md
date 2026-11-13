# Command Code 集成

## 定位与启动

Command Code 是用户自备的 CLI，应用只定位已有安装，不安装、不升级、不代登录。

| 项 | 值 |
| --- | --- |
| 内置 id | `command-code` |
| 启动命令 | `<绝对路径>/command-code acp` |
| 版本探测 | `command-code --version`（5 秒超时；失败只影响版本显示） |
| 显式覆盖 | `ACP_GUI_COMMAND_CODE=/absolute/path` |
| 运行时 | Node.js 22+（command-code 的 bin 通过 `env node` 启动） |
| 登录提示 | `command-code login`（只在 UI 展示，不代跑） |

路径解析顺序与 pi 共用：登录 shell（`-lc`，必要时 `-ilc`）→ `PATH` → npm 全局 bin / 常见安装目录。启动时把 `command-code` 和 node 所在目录补进子进程 `PATH`，其余环境原样透传。Node 缺失或低于 22 时不把 command-code 列为可启动。

`resources/` 不参与 Command Code 启动；它没有 vendored 资源。

## 已验证的 ACP 流程

本机 `command-code 1.88.5`，客户端声明 `fs: false`、`terminal: false`。

### initialize

返回 `protocolVersion: 1`，并带有：

- `agentInfo.name = "Command Code"`、`agentInfo.version = CLI 版本`
- `authMethods = [{ id: "command-code-cli", name: "Command Code CLI login", ... }]`
- `agentCapabilities`: `loadSession`、图片/embedded context、MCP HTTP、`sessionCapabilities` 的 list/close

`initialize` 本身不要求登录。

### session/new

成功时返回 `sessionId`、`modes`、`configOptions`、`models`：

- `modes`: `default`、`auto-accept`、`plan`、`dont-ask`、`bypass`
- `configOptions`: `model`（很多模型）和 `effort`（off/low/high/max）
- `model` 列表由 Command Code 自己决定，包含普通模型和 `opencode-go/*`（byok）等；ACP 没有 provider 可用性字段，也没有标记某个模型需要额外 BYOK key。GUI 只能展示 agent 返回的列表，无法区分 BYOK 或未配置的 provider；这类过滤应在 agent 侧完成。
- 默认工作区是绝对路径；命令写入 stderr，不污染 stdout

未登录时返回 JSON-RPC `-32000`（`Not authenticated. Run \`cmd login\` in a terminal first.`），应用进入 `needs-auth`，展示 `authMethods` 和 `command-code login`，不代登录。

### session/prompt 与 session/update

一轮 prompt 中实际观察到的 update：

- `available_commands_update`：很大，可能每轮开始都发；应用当前安静忽略
- `agent_thought_chunk`：思考流
- `agent_message_chunk`：正文流
- `tool_call` / `tool_call_update`：`pending → in_progress → completed/failed`；工具内容在 `content` 数组里
- `usage_update`：上下文 used/size 与费用；`internal/session` 解析为 `UpdateUsage`，UI 显示在所属标签的会话栏
- prompt 结果除 `stopReason` 外还带 `usage` / `_meta`；`usage` 解析为 `acp.PromptUsage` 并显示为 `turn … in · … out`，`_meta` 保留不展示

`session/cancel` 是 notification；取消后 prompt 以 `stopReason: "cancelled"` 结束，同一会话可以继续发下一轮。用户拒绝权限时 agent 会发 `tool_call_update` 并把回合正常结束。

### session/request_permission

写文件等修改操作会触发。实测 options：

```json
[
  {"optionId":"allow_once","name":"Allow once","kind":"allow_once"},
  {"optionId":"allow_always","name":"Always allow","kind":"allow_always"},
  {"optionId":"reject_once","name":"Reject","kind":"reject_once"},
  {"optionId":"reject_always","name":"Always reject","kind":"reject_always"}
]
```

应用阻塞等待用户选择，返回 `{"outcome":{"outcome":"selected","optionId":"..."}}`；关闭弹窗回 `{"outcome":{"outcome":"cancelled"}}`。响应必须带原请求的 `id`，不能发成 notification。

`toolCall.rawInput.description`（如 `shell_command` 的 5–10 词说明）作为弹窗主标题；`title`（`Shell: <command>`）降为次行，完整入参仍在 JSON 块中。

### stderr

Command Code 会向 stderr 写 `[acp] ...` 日志，也会打印它自己看到的请求/响应 JSON。应用单独收集 stderr，不把它当协议输出；只有 stdout 的 NDJSON 是 ACP。stderr 同时进入当前标签的 Diagnostics 缓冲和 `logs/command-code.log`（1 MiB 滚动）。

## 当前应用覆盖

已跑通：发现（initialize 探测）、`initialize`、`session/new`、流式正文/思考/工具、权限审批、`session/cancel`、取消后继续、`session/set_config_option`（model/effort）、`session/set_mode`、`session/list`、`session/load`、`session/close`、图片 prompt、MCP servers 透传、`available_commands_update` 命令面板、plan/title/usage、退出时进程组清理。

仍未实现（全局计划，不是 Command Code 特有）：

- MCP servers 的可视化行编辑（当前是校验过的 JSON）

## 测试

自动化真实流程（会调用模型、可能产生费用）：

```bash
ACP_REAL_COMMAND_CODE=1 go test ./internal/session -run '^TestRealCommandCode$' -v -count=1
ACP_REAL_COMMAND_CODE=1 go test ./internal/session -run '^TestRealCommandCodeAuthRequired$' -v -count=1
```

第一条覆盖：发现 → `initialize` → `session/new` → 普通回复 → 写文件权限 → config/mode → 图片 prompt → 取消 → 取消后继续 → list/load/close/new → `Close`。第二条用临时 `HOME` 验证未登录时 `session/new` 的 `-32000`、`authMethods` 和登录提示，不碰真实凭据。默认跳过。

不调用模型的握手冒烟：

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":false,"writeTextFile":false},"terminal":false}}}' \
  | command-code acp
```

回到内置表与状态模型见 [discovery.md](discovery.md)。
