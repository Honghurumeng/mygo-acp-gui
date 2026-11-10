# 会话能力与 UI

本文记录除基础聊天之外的 ACP 能力：发现探测、配置/模式、历史会话、图片、MCP、命令面板、计划与用量。

## 发现与状态

`internal/agent.BuiltinLaunchResults` 返回内置表每一行的结果（ID、名称、安装提示、launch spec 或错误）。UI 对每个成功解析的 spec 并行执行 `session.Probe`：

1. 在临时工作区 spawn agent。
2. 只发 `initialize`，5 秒超时。
3. 成功 → `ready`；超时/崩溃 → `error`；stdout 非 JSON / 握手失败 → `installed-not-acp`；路径/Node 缺失 → `not-installed`。
4. probe 进程总是关闭并清理进程组。

只有 `ready` 的 spec 进入 Agent 下拉框；状态行显示所有内置 agent 及失败详情。`session/new` 要求登录时，该 agent 状态改为 `needs-auth`，只展示 `authMethods` + `loginHint`。

## 配置与模式

`Info` 解析 `configOptions`（model、effort 等）和 `modes`，并兼容只有 `models` 的旧 agent（合成为 `model` 配置项）。

- 配置栏按 `configOptions` 生成 `ui.Select`，选中后调用 `session/set_config_option` `{sessionId, configId, value}`。
- 模式下拉调用 `session/set_mode` `{sessionId, modeId}`。
- 两者都更新 `Info`，并接收 agent 的 `config_option_update` / `current_mode_update` 通知。
- 流式回合、连接中或即将切换会话时禁用，避免 agent 返回 `-32600`。
- Command Code 的 `effort` 可能随模型变化；选项一律以 agent 返回为准，不硬编码。

## 历史会话

- `session/list` `{cwd?}` → `sessions[{sessionId,cwd,title,updatedAt}]`；UI 可切换“当前工作区/全部工作区”。
- `session/load` 会重放历史 `session/update`；`session/resume` 只附着不重放。
- 未连接时，`session.ListSessions` 启动一个 initialize-only 的短进程来列表，然后关闭。
- 已连接时，在现有 agent 进程里打开历史；成功后对旧 ACP session 发 `session/close`。
- 从其他工作区打开历史时，把该 session 的 `cwd` 传给 load/resume 并更新工作区输入框，不用当前进程目录冒充。
- `session/close` 也用于“Close chat”：进程保留，清空当前会话，之后可 New/History。
- “New chat” 在同进程调用 `session/new`，成功后关闭旧 session。
- 历史重放事件在 `Start` 阶段缓冲，UI 安装好 Session 后调用 `Activate` 按顺序消费，避免丢失。
- 重放中的 `user_message_chunk` 和 `agent_message_chunk` 合并成连续的用户/助手行，结束后统一标记 Done。

## 图片

- 文件对话框选择 png/jpg/jpeg/gif/webp/bmp，最多 4 张、每张 15 MB。
- 读取后转 base64，`ContentBlock{type:"image", mimeType, data}` 与 text 一起发 `session/prompt`。
- 预览由 `ui.DecodeBitmap` 生成，挂在草稿或用户消息上。
- 只有 `promptCapabilities.image` 为 true 时启用 Attach；agent 不支持时按钮禁用。
- 不做内嵌终端、不代传本地路径；图片字节直接进 ACP。

## MCP servers

- “MCP…” 模态编辑 JSON 数组，支持 stdio（`name` + `command`/`args`/`env`）和 HTTP/SSE（`type:"http"` 或 `"sse"` + `name` + `url`/`headers`）。
- 保存前校验：必须是对象数组、必须有 `name`，stdio 必须有 `command`，http 必须有 `url`。
- 列表传给下一次 `session/new` / `session/load` / `session/resume`；已连接时也更新 Session 的待用列表。
- 不启动、不管理 MCP 进程；由 agent 自己拉起。
- 当前只保存在内存，不写 settings.json。

## 命令面板

- 解析 `available_commands_update.availableCommands`。
- Composer 的 “Commands” 按钮打开 `ui.Popover`，列出 `/name` 与描述。
- 点击把 `/name` 插入输入框；有 `input` 的命令补一个空格，由用户继续填写。
- 不执行命令、不维护本地命令表；以 agent 通告为准。

## 计划、标题与用量

- `plan` → composer 上方的可折叠计划列表，显示状态。
- `session_info_update.title` → 标题显示在会话栏。
- `usage_update` → context used/size 与费用显示在会话栏和 composer。
- prompt 结果里的 `usage` / `_meta` 保留在 `acp.PromptResult`，当前不展示明细。

## 事件顺序与生命周期

- `internal/session.Start` 支持 `DeferUpdates`：`session/update` 先进缓冲区，UI 绑定 Session 后 `Activate` 再按顺序交付。
- `Info` 在 mutex 下更新；`config_option_update`、`current_mode_update`、`session_info_update` 同时写入 Info 和 UI。
- `Session.Close` 先尽力 `session/close`（1 秒），再关 stdin、SIGTERM/SIGKILL 进程组。
- `Session.ListSessions` / `Probe` 复用同一进程生命周期，不引入第二套 spawn 逻辑。

## 测试

```bash
go test ./... -race
go test ./internal/ui -run 'TestSessionConfig|TestHistory|TestCloseChat|TestSendImage|TestMCP' -v
ACP_REAL_COMMAND_CODE=1 go test ./internal/session -run '^TestRealCommandCode$' -v -count=1
```

`scripts/fake-agent.mjs` 支持 list/load/resume/close、set_mode、set_config_option、图片计数、MCP 计数、命令和计划事件。
