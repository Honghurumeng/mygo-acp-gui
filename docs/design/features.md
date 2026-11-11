# 会话能力与 UI

本文记录除基础聊天之外的 ACP 能力：多会话、发现探测、配置/模式、历史会话、图片、MCP、命令面板、计划与用量。

## 发现与状态

`internal/agent.BuiltinLaunchResults` 返回内置表每一行的结果（ID、名称、安装提示、launch spec 或错误）。UI 对每个成功解析的 spec 并行执行 `session.Probe`：

1. 在临时工作区 spawn agent。
2. 只发 `initialize`，5 秒超时。
3. 成功 → `ready`；超时/崩溃 → `error`；stdout 非 JSON / 握手失败 → `installed-not-acp`；路径/Node 缺失 → `not-installed`。
4. probe 进程总是关闭并清理进程组。

只有 `ready` 的 spec 进入 Agent 列表；侧栏底部显示所有内置 agent 及失败详情。`session/new` 要求登录时，该 agent 状态改为 `needs-auth`，只展示 `authMethods` + `loginHint`。

## 多会话

每个会话是一个独立的 `Chat`：拥有自己的 agent 进程、ACP session、工作区、MCP 列表、消息、配置、草稿和权限请求。会话之间并行运行，切换只改变显示，不会暂停后台流式回复。

选择“每会话独立进程”而不是共享进程里的多个 ACP session：进程级隔离让 cwd、取消、权限和崩溃互不影响，代价是每个会话各有一套 agent 运行时资源。

- `App` 只保存全局状态（内置 agent、发现结果、工作区列表、MCP 默认值、会话列表、当前会话）和 `[]*Chat`。
- session 回调在创建时捕获所属 `*Chat`，按 session 指针路由，不读取“当前会话”，所以后台会话的 `session/update`、退出和权限都能落到正确位置。
- 左侧工作区栏按 `Chat.Workspace` 分组显示会话标题（无标题时 `Chat N`）、流式转圈、未读点和权限提醒；工作区名可折叠，超过 5 条会话时只显示前 5 条，并提供展开/收起。
- 会话右侧 `⋯` 打开重命名/删除；删除会关闭该会话的进程。`New conversation` 新建独立会话，工作区上的 `＋` 在该目录下新建。
- 启动时默认创建一个草稿会话；在草稿里输入第一条消息时自动 Connect，成功后立即发送，不需要先点 Connect。
- 每个会话保留自己的消息列表滚动位置、输入草稿、图片附件、配置选择和计划面板。
- `Disconnect` 关闭当前会话的 agent 进程但保留草稿；`Close chat` 仍是 `session/close`（保留进程）；顶部 `New chat` 在同进程创建新 session。
- 应用退出时取消所有正在连接的会话，并关闭所有已登记 session 的进程组。

权限弹窗是全局的，但标注所属会话（`Chat: …`）。多个后台会话同时请求权限时排队显示，回答按 `Chat` 路由；侧栏的 `!` 在有未答权限时显示。

## 配置与模式

`Info` 解析 `configOptions`（model、effort 等）和 `modes`，并兼容只有 `models` 的旧 agent（合成为 `model` 配置项）。

- composer 的模型 chip 打开面板，四列为 agent / provider / model / thinking；provider 由模型值的 `/` 或 `:` 前缀分组，没有前缀时归入 `Models`。
- agent 只在会话开始前可选；连接后锁定。切换 agent 后重新 Connect 才生效。
- 选择 model / thinking 调用 `session/set_config_option` `{sessionId, configId, value}`；其他 select 型配置追加显示在 thinking 列。
- 模式 chip 显示当前 mode，点击后调用 `session/set_mode` `{sessionId, modeId}`。
- 两者都更新 `Info`，并接收 agent 的 `config_option_update` / `current_mode_update` 通知。
- 流式回合、连接中或即将切换会话时禁用，避免 agent 返回 `-32600`。
- Command Code 的 `effort` 可能随模型变化；选项一律以 agent 返回为准，不硬编码。

## 历史会话

- `session/list` `{cwd?}` → `sessions[{sessionId,cwd,title,updatedAt}]`；UI 可切换“当前工作区/全部工作区”。
- `session/load` 会重放历史 `session/update`；`session/resume` 只附着不重放。
- 未连接时，`session.ListSessions` 启动一个 initialize-only 的短进程来列表，然后关闭。
- 当前会话已连接时，优先在现有 agent 进程里打开历史；成功后对旧 ACP session 发 `session/close`。
- 当前会话已有消息或正在流式时，Open/Resume 会新建会话并连接，原来的对话继续保留；空的已连接会话则就地 load/resume。
- 从其他工作区打开历史时，把该 session 的 `cwd` 传给 load/resume、加入侧栏工作区列表，不用当前进程目录冒充。
- `session/close` 也用于“Close chat”：进程保留，清空当前会话，之后可 New/History。
- “New chat” 在同进程调用 `session/new`，成功后关闭旧 session；它只影响当前会话。要并行对话用侧栏的 `New conversation`。
- 历史重放事件在 `Start` 阶段缓冲，UI 安装好 Session 后调用 `Activate` 按顺序消费，避免丢失。
- 重放中的 `user_message_chunk` 和 `agent_message_chunk` 合并成连续的用户/助手行，结束后统一标记 Done。

## 图片

- 文件对话框选择 png/jpg/jpeg/gif/webp/bmp，最多 4 张、每张 15 MB。
- 读取后转 base64，`ContentBlock{type:"image", mimeType, data}` 与 text 一起发 `session/prompt`。
- 预览由 `ui.DecodeBitmap` 生成，挂在草稿或用户消息上。
- composer 左侧 `＋` 打开图片选择；已连接且 `promptCapabilities.image` 为 false 时禁用，草稿阶段允许先选图。
- 不做内嵌终端、不代传本地路径；图片字节直接进 ACP。

## MCP servers

- “MCP…” 模态编辑 JSON 数组，支持 stdio（`name` + `command`/`args`/`env`）和 HTTP/SSE（`type:"http"` 或 `"sse"` + `name` + `url`/`headers`）。
- 保存前校验：必须是对象数组、必须有 `name`，stdio 必须有 `command`，http 必须有 `url`。
- 每个会话各有一份列表；设置窗口修改当前会话，并作为新会话的默认值。列表传给该会话后续的 `session/new` / `session/load` / `session/resume`；已连接时也更新 Session 的待用列表。
- 不启动、不管理 MCP 进程；由 agent 自己拉起。
- Apply 后的列表写入 settings.json，成为下次启动的默认值（见“设置持久化”）。

## 设置持久化

- `internal/settings` 读写 `PathUserData/settings.json`；`ui.Options.DataDir` 为空时完全不落盘（测试、便携运行）。
- 保存内容：最后使用的 agent、工作区、侧栏工作区列表、MCP 默认列表，以及 `paths` 里的显式工具覆盖。
- 保存时机：切换会话、选择 agent/目录、添加工作区、Connect 校验成功、MCP Apply、退出。
- 启动恢复：工作区、侧栏工作区列表、MCP 列表、上次 agent；显式传入的 `ui.Options.Workspace` 优先（测试用）。
- `paths` 只保存配置里的覆盖值；`ACP_GUI_*` 环境变量优先，且不会被写回文件。
- 容错：文件缺失用默认值；损坏 JSON、MCP 不是数组、相对路径、未来版本只显示侧栏/标题栏警告，其余字段照常生效。
- 不保存会话布局、草稿、附件和消息；历史会话仍由 agent 保存。

## 诊断与日志

- 顶部 “Logs” 打开 Diagnostics 模态；没有聊天时显示发现状态，有聊天时额外显示当前会话的 agent、工作区、命令行、连接错误和 stderr 尾部。
- 每个会话在内存中保留最近 64 KiB stderr（`Chat.Stderr` / `StderrBytes`），超过部分只留在日志文件。
- 同时写入 `<data>/logs/<agent>.log`：连接头、stderr 原样、`[protocol]`、`[probe]`、`[exit]`。单文件 1 MiB，滚动为 `.1` 备份，模式 0600。
- “Reveal log” 在文件管理器中定位日志；stderr 以只读等宽 TextArea 展示，可选中复制。
- 写日志失败或未设置数据目录时不影响聊天，只看不到文件副本。

## 命令面板

- 解析 `available_commands_update.availableCommands`。
- 输入框内容以 `/` 开头且还没有空格时，composer 上方出现命令面板，按前缀过滤，最大高度约 300 并滚动。
- 点击把 `/name` 插入输入框；有 `input` 的命令补一个空格，由用户继续填写。
- 不执行命令、不维护本地命令表；以 agent 通告为准。

## 计划、标题与用量

- `plan` → composer 上方的可折叠计划列表，显示状态。
- `session_info_update.title` → 标题显示在顶部会话栏和侧栏会话项。
- `usage_update` → composer 中的环形进度 + 百分比；hover 显示 used/size、百分比和费用。agent 不发时没有上下文数字；ACP 没有主动查询的方法。
- prompt 结果里的 `usage` 解析为 `acp.PromptUsage`，显示为 `turn 1.2k in · 100 out · 40 thought`（只在 agent 返回时出现），与 `context` 分开；`_meta` 保留在 `acp.PromptResult`，不展示明细。
- 当前 command-code 会发 `usage_update`；pi-acp 0.4.0 不发，也不返回 prompt `usage`（见 [pi.md](pi.md#用量)）。

## 事件顺序与生命周期

- `internal/session.Start` 支持 `DeferUpdates`：`session/update` 先进缓冲区，UI 绑定 Session 后 `Activate` 再按顺序交付。
- 每个会话各自调用一次 `session.Start`；历史重放、退出和权限都以 `*session.Session` 指针归属到对应会话。
- `Info` 在 mutex 下更新；`config_option_update`、`current_mode_update`、`session_info_update` 同时写入 Info 和 UI。
- `Session.Close` 先尽力 `session/close`（1 秒），再关 stdin、SIGTERM/SIGKILL 进程组。
- `Session.ListSessions` / `Probe` 复用同一进程生命周期，不引入第二套 spawn 逻辑。

## 测试

```bash
go test ./... -race
go test ./internal/ui -run 'TestSessionConfig|TestHistory|TestCloseChat|TestSendImage|TestMCP|TestPromptUsageFallback|TestMultipleChats|TestBackgroundChat|TestPermissionFrom|TestCloseTab|TestFirstMessage|TestWorkspaceConversation|TestRename|TestGit|TestComposerHeight' -v
go test ./internal/settings -v
go test ./internal/logs -v
go test ./internal/ui -run 'TestSettings|TestShutdownPersists|TestCorruptSettings|TestExplicitWorkspace|TestDiagnostics' -v
ACP_REAL_COMMAND_CODE=1 go test ./internal/session -run '^TestRealCommandCode$' -v -count=1
```

`scripts/fake-agent.mjs` 支持 list/load/resume/close、set_mode、set_config_option、图片计数、MCP 计数、命令和计划事件。
