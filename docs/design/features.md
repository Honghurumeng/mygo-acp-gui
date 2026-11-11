# 会话能力与 UI

本文记录除基础聊天之外的 ACP 能力：多会话、发现探测、配置/模式、历史会话、图片、MCP、命令面板、计划与用量。

## 发现与状态

`internal/agent.BuiltinLaunchResults` 返回内置表每一行的结果（ID、名称、安装提示、launch spec 或错误）。UI 对每个成功解析的 spec 并行执行 `session.Probe`：

1. 在临时工作区 spawn agent。
2. 只发 `initialize`，5 秒超时。
3. 成功 → `ready`；超时/崩溃 → `error`；stdout 非 JSON / 握手失败 → `installed-not-acp`；路径/Node 缺失 → `not-installed`。
4. probe 进程总是关闭并清理进程组。

只有 `ready` 的 spec 进入 Agent 下拉框；状态行显示所有内置 agent 及失败详情。`session/new` 要求登录时，该 agent 状态改为 `needs-auth`，只展示 `authMethods` + `loginHint`。

## 多会话

每个标签页是一个独立的 `Chat`：拥有自己的 agent 进程、ACP session、工作区、MCP 列表、消息、配置、草稿和权限请求。标签之间并行运行，切换只改变显示，不会暂停后台流式回复。

选择“每标签独立进程”而不是共享进程里的多个 ACP session：进程级隔离让 cwd、取消、权限和崩溃互不影响，代价是每个标签各有一套 agent 运行时资源。

- `App` 只保存全局状态（内置 agent、发现结果、MCP 默认值、标签列表、当前标签）和 `[]*Chat`。
- session 回调在创建时捕获所属 `*Chat`，按 session 指针路由，不读取“当前标签”，所以后台标签的 `session/update`、退出和权限都能落到正确位置。
- 标签栏显示标题（无标题时 `Chat N`）、流式转圈、未读点和权限提醒；`×` 关闭该标签并清理进程。`+ New` 复制当前标签的 agent、工作区和 MCP 列表。
- Agent / Workspace 选择框跟随当前标签；已连接或连接中时禁用。要换工作区或 agent，先 `+ New`，再修改并 Connect。
- 每个标签保留自己的消息列表滚动位置、输入草稿、图片附件、配置选择和计划面板。
- `Disconnect` 关闭当前标签的 agent 进程但保留空标签，可重新 Connect；`Close chat` 仍是 `session/close`（保留进程）；标签 `×` 才关闭进程和标签。
- 应用退出时取消所有正在连接的标签，并关闭所有已登记 session 的进程组。

权限弹窗是全局的，但标注所属标签（`Chat: …`）。多个后台标签同时请求权限时排队显示，回答按 `Chat` 路由；标签上的 `!` 在有未答权限时显示。

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
- 当前标签已连接时，优先在现有 agent 进程里打开历史；成功后对旧 ACP session 发 `session/close`。
- 当前标签已有消息或正在流式时，Open/Resume 会新建标签并连接，原来的对话继续保留；空的已连接标签则就地 load/resume。
- 从其他工作区打开历史时，把该 session 的 `cwd` 传给 load/resume 并更新工作区输入框，不用当前进程目录冒充。
- `session/close` 也用于“Close chat”：进程保留，清空当前会话，之后可 New/History。
- “New chat” 在同进程调用 `session/new`，成功后关闭旧 session；它只影响当前标签。要并行对话用标签栏的 `+ New`。
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
- 每个标签各有一份列表；设置窗口修改当前标签，并作为新标签的默认值。列表传给该标签后续的 `session/new` / `session/load` / `session/resume`；已连接时也更新 Session 的待用列表。
- 不启动、不管理 MCP 进程；由 agent 自己拉起。
- Apply 后的列表写入 settings.json，成为下次启动的默认值（见“设置持久化”）。

## 设置持久化

- `internal/settings` 读写 `PathUserData/settings.json`；`ui.Options.DataDir` 为空时完全不落盘（测试、便携运行）。
- 保存内容：最后使用的 agent、工作区、MCP 默认列表，以及 `paths` 里的显式工具覆盖。
- 保存时机：切换标签、选择 agent/目录、Connect 校验成功、MCP Apply、退出。
- 启动恢复：工作区、MCP 列表、上次 agent；显式传入的 `ui.Options.Workspace` 优先（测试用）。
- `paths` 只保存配置里的覆盖值；`ACP_GUI_*` 环境变量优先，且不会被写回文件。
- 容错：文件缺失用默认值；损坏 JSON、MCP 不是数组、相对路径、未来版本只显示工具栏警告，其余字段照常生效。
- 不保存多标签布局、草稿、附件和消息；历史会话仍由 agent 保存。

## 诊断与日志

- 顶部 “Logs” 打开 Diagnostics 模态；没有聊天时显示发现状态，有聊天时额外显示当前标签的 agent、工作区、命令行、连接错误和 stderr 尾部。
- 每个标签在内存中保留最近 64 KiB stderr（`Chat.Stderr` / `StderrBytes`），超过部分只留在日志文件。
- 同时写入 `<data>/logs/<agent>.log`：连接头、stderr 原样、`[protocol]`、`[probe]`、`[exit]`。单文件 1 MiB，滚动为 `.1` 备份，模式 0600。
- “Reveal log” 在文件管理器中定位日志；stderr 以只读等宽 TextArea 展示，可选中复制。
- 写日志失败或未设置数据目录时不影响聊天，只看不到文件副本。

## 命令面板

- 解析 `available_commands_update.availableCommands`。
- Composer 的 “Commands” 按钮打开 `ui.Popover`，列出 `/name` 与描述。
- 点击把 `/name` 插入输入框；有 `input` 的命令补一个空格，由用户继续填写。
- 不执行命令、不维护本地命令表；以 agent 通告为准。

## 计划、标题与用量

- `plan` → composer 上方的可折叠计划列表，显示状态。
- `session_info_update.title` → 标题显示在会话栏。
- `usage_update` → context used/size 与费用显示在所属标签的会话栏（每个标签独立；当前没有 composer 副本）。agent 不发时没有上下文数字；ACP 没有主动查询的方法。
- prompt 结果里的 `usage` 解析为 `acp.PromptUsage`，显示为 `turn 1.2k in · 100 out · 40 thought`（只在 agent 返回时出现），与 `context` 分开；`_meta` 保留在 `acp.PromptResult`，不展示明细。
- 当前 command-code 会发 `usage_update`；pi-acp 0.4.0 不发，也不返回 prompt `usage`（见 [pi.md](pi.md#用量)）。

## 事件顺序与生命周期

- `internal/session.Start` 支持 `DeferUpdates`：`session/update` 先进缓冲区，UI 绑定 Session 后 `Activate` 再按顺序交付。
- 每个标签各自调用一次 `session.Start`；历史重放、退出和权限都以 `*session.Session` 指针归属到对应标签。
- `Info` 在 mutex 下更新；`config_option_update`、`current_mode_update`、`session_info_update` 同时写入 Info 和 UI。
- `Session.Close` 先尽力 `session/close`（1 秒），再关 stdin、SIGTERM/SIGKILL 进程组。
- `Session.ListSessions` / `Probe` 复用同一进程生命周期，不引入第二套 spawn 逻辑。

## 测试

```bash
go test ./... -race
go test ./internal/ui -run 'TestSessionConfig|TestHistory|TestCloseChat|TestSendImage|TestMCP|TestPromptUsageFallback|TestMultipleChats|TestBackgroundChat|TestPermissionFrom|TestCloseTab' -v
go test ./internal/settings -v
go test ./internal/logs -v
go test ./internal/ui -run 'TestSettings|TestShutdownPersists|TestCorruptSettings|TestExplicitWorkspace|TestDiagnostics' -v
ACP_REAL_COMMAND_CODE=1 go test ./internal/session -run '^TestRealCommandCode$' -v -count=1
```

`scripts/fake-agent.mjs` 支持 list/load/resume/close、set_mode、set_config_option、图片计数、MCP 计数、命令和计划事件。
