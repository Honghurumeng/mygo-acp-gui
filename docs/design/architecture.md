# 架构

## 目标

原生桌面 ACP GUI：Go + [MyGo](https://github.com/egoist/mygo) 写界面，检测内置 agent（目前 pi、command-code）是否可用，选择 agent 和工作区后聊天。

早期 Node 原型（`server/` + `public/`）已移除，只保留在 git 历史中；当前实现是仓库根目录的 Go + MyGo 工程。

## 整体架构

```
MyGo 原生窗口 (Go + ui)
  ├─ 会话标签栏（每个标签一个 agent 进程 + ACP session）
  ├─ Agent 选择 / 工作区选择 / 当前标签的会话视图
  ├─ ui/markdown.Stream / View      ← agent_message_chunk
  ├─ ui.List(FollowEnd)             ← 消息列表
  ├─ ui.Collapsible                 ← thought / tool_call
  ├─ ui.Select / Segmented          ← model / thought_level
  └─ ACP client (Go)
        ↕ newline-delimited JSON-RPC 2.0 (stdin/stdout)
     agent 子进程（pi-acp、command-code）
        ↕
     agent 自己的模型/工具运行时
```

## Go 包划分

```
仓库根/
  main.go                  MyGo 应用入口、窗口与退出清理
  main_test.go             UI 冒烟测试
  mygo.json                App 元数据与打包配置
  internal/acp/            JSON-RPC、请求/通知、权限、行分帧（已实现）
  internal/agent/          内置 agent 表、launch spec、登录 shell 路径解析、工作区校验、状态探测
  internal/session/        进程组 + ACP 会话生命周期 + update/permission 分发（已实现）
  internal/settings/       settings.json 读写、容错与原子落盘（已实现）
  internal/logs/           每个 agent 的滚动日志文件（已实现）
  internal/ui/             多标签会话、聊天、流式 Markdown、权限队列、窗口更新（已实现）
  resources/
    agents/pi-acp/
      pi-acp.bundle.mjs    单文件 bundle，启动时直接 node 执行（见 pi.md）
      package.json/VERSION/LICENSE/licenses/THIRD_PARTY_NOTICES.md/SHA256SUMS
```

分层原则：`internal/acp` 不 import UI；探测逻辑可独立测试；UI 只读状态、发命令。

## 工程与构建

- `go.mod`：module `github.com/reed/acp-gui`，Go 1.27.3（`GOTOOLCHAIN=auto` 会自动拉取）。
- MyGo CLI 通过 `tool github.com/egoist/mygo/cmd/mygo` 固定在 `go.mod`，统一用 `go tool mygo`。
- 无前端构建：原生 UI，入口 `main.go`，配置在 `mygo.json`。
- App：name `ACP GUI`、slug `acp-gui`、identifier `dev.reed.acp-gui`、version `0.1.0`。
- `go tool mygo build` 产物在 `build/<os>-<arch>/`（已 gitignore）；`resources/` 整目录进 `.app`。
- 当前已实现 `internal/acp`、`internal/agent`、`internal/session`、`internal/ui` 的 MVP 链路（见下方“当前链路”）。

## 当前链路

```
启动 app
  → internal/settings 读取 PathUserData/settings.json（上次 agent/工作区/MCP、路径覆盖）
  → internal/agent.ResolveEnvironment（环境变量 > settings paths > 登录 shell；node/pi/command-code 绝对路径）
  → internal/agent.BuiltinLaunchResults
      pi: vendored bundle + Node 22+
      command-code: <绝对路径>/command-code acp
  → internal/session.Probe（initialize，5 秒，逐 agent 并行）
  → 标签栏 + New / Connect：每个标签各自 internal/session.Start
      session/new | session/load | session/resume（DeferUpdates 缓冲历史）
  → session/prompt（文本 + 图片）
  → session/update → 所属 *Chat → Window.Update → markdown / tool / plan / usage
  → session/set_config_option / set_mode
  → session/list → load / resume / close；New chat 同进程 session/new
  → Stop → session/cancel
  → 标签 × / Disconnect → Session.Close → kill(-pgid)
  → 关闭窗口 / 退出 → 关闭所有标签的进程组
```

- 解析并探测 pi、command-code；单个 agent 缺失不会隐藏另一个，状态行展示 `ready` / `not-installed` / `installed-not-acp` / `needs-auth` / `error`。内置 agent 表固定为 pi、command-code，不做 registry 发现。
- 路径解析优先使用 `ACP_GUI_NODE` / `ACP_GUI_PI` / `ACP_GUI_COMMAND_CODE` 的绝对路径覆盖，其次是 `settings.json` 的 `paths`，否则用登录 shell 的 `PATH`。只定位已有安装，不安装、不捆绑 Node。
- UI 状态只在主线程修改。session 回调通过 `Window.Update`（测试中通过队列）提交，不直接改状态。
- 每个标签是一个 `ui.Chat`，保存自己的 Session、Info、消息、输入草稿、配置、附件和连接代数；App 持有 `Chats` 与 `Active`。回调捕获 chat 指针，因此后台标签仍能流式更新。
- 测试用 `scripts/fake-agent.mjs`：prompt 流式输出、`/slow` + cancel、`/permission`、config/mode、list/load/resume/close、图片/MCP 计数、命令/plan/usage、子进程清理。

## ACP 客户端

- 分帧：严格按 `\n` 切分；允许行尾 `\r`；**不要**按 `U+2028` / `U+2029` 切分。
- 两向消息：
  - 客户端 → agent：request（有 `id`）、notification（无 `id`）
  - agent → 客户端：notification（`session/update`）、request（`session/request_permission`）
  - 未知 agent→client 请求：回 `-32601 method not supported`
- pi 用 vendored 单文件 bundle 启动，不经过 npx，也不在启动时构建（见 [pi.md](pi.md)）
- 必备方法：
  - `initialize`（声明 `clientCapabilities`）
  - `session/new`（cwd 必须是绝对路径）
  - `session/prompt`（在 goroutine 中调用，一轮结束才 resolve）
  - `session/cancel`（notification）
  - `session/set_config_option`（model / thought_level）；旧适配器回退 `session/set_model` / `session/set_mode`
  - 权限响应：`{"outcome":{"outcome":"selected","optionId":"..."}}`；取消：`{"outcome":{"outcome":"cancelled"}}`
- `session/update` 需要处理：
  - `agent_message_chunk`、`agent_thought_chunk`
  - `tool_call`、`tool_call_update`（pending → in_progress → completed/failed）
  - `plan`
  - `current_mode_update`、`config_option_update`、`available_commands_update`
  - 未知 `sessionUpdate`：安静忽略，不要崩
- 错误：
  - JSON-RPC error 保留 `code` / `data`；`-32000` 或 `authMethods` → 展示登录指引，不代登录
  - stdout 非 JSON → 协议错误日志
  - stderr 单独收集，展示在诊断面板
- 能力声明与现有实现一致：

```go
ClientCapabilities{
  Fs:       FileSystemCapabilities{ReadTextFile: false, WriteTextFile: false},
  Terminal: false,
}
```

pi 自己的工具在工作区内读写执行；客户端只负责展示和审批。

## 进程管理

- Unix：`Setpgid: true`，退出时 `kill(-pid, SIGTERM)`，宽限 1.5 秒后 `SIGKILL`。
- Windows：新进程组 + `taskkill /PID <pid> /T /F`。
- 先关 stdin 请求优雅退出；agent 意外退出时也对残留进程组发 SIGTERM。
- 应用退出 / 窗口关闭必须清理所有 agent 进程；`Session.Close` 幂等，`App.Shutdown` 取消所有标签的连接并关闭 sessions 中登记的全部进程组。
- stderr 单独收集，保留末尾 64 KiB；初始化/会话失败时附在错误里（截断展示）。

## MyGo UI

- 消息流：`markdown.Stream` 显示进行中的回复，结束后切换 `markdown.View`（同一位置）。
- 聊天列表：`ui.List`，`FollowEnd` + `FollowAnimation = &ui.Smooth`；用户上滑后停止跟随。
- 思考/工具：`ui.Collapsible`；工具卡片显示标题、状态、可展开的入参/输出。
- 权限：`session/request_permission` 回调阻塞，`ui.Modal` 展示工具名和 agent 给出的 `options`；按钮直接回 `optionId`，关闭/取消回 `cancelled`。
- 配置：`ui.Select` / `ui.Segmented`；模型、effort、模式从 `configOptions` / `modes` 生成，不硬编码。
- 输入：`ui.TextArea`，Enter 发送、Shift+Enter 换行；流式期间同一位置切换为 Stop；支持图片附件。
- 多会话：标签栏显示标题/流式/未读/权限提醒；`+ New` 复制当前 agent、工作区和 MCP；标签 `×` 关闭对应进程。Agent/Workspace 选择跟随当前标签，连接后锁定。
- 权限：全局模态但带 `Chat: …` 标签，后台请求排队；回答按所属 `Chat` 路由，标签 `!` 提醒未答请求。
- 历史：`session/list` 模态；当前标签已有消息或流式时，Open/Resume 在新标签连接，否则就地 load/resume；Close chat 走 `session/close`。
- MCP：JSON 模态，校验后传给 new/load/resume；命令面板解析 `available_commands_update`。
- 诊断：顶部 “Logs” 打开 `Diagnostics`，显示发现状态、当前标签的 stderr 尾部、日志路径与 Reveal；stderr 同时写入滚动日志文件。
- 计划/用量：composer 上方 plan，会话栏显示 usage/title。
- 跨 goroutine 改状态：`win.Update(func(){ ... })`；没有窗口的测试用 `App.Flush` 排队应用。
- 资源：`mygo.App.Path(mygo.PathResources)`；数据与设置：`mygo.App.Path(mygo.PathUserData)`。
- 登录：只展示 agent 的 `authMethods` / 命令，由用户在外部终端自行解决；不内嵌终端。
- 参考 MyGo 示例：`examples/markdown-chat`。

## 数据与配置

存储在 `mygo.App.Path(mygo.PathUserData)`：

```
settings.json        lastAgent、lastWorkspace、mcpServers、paths 覆盖
logs/                每个 agent 的 stderr / 协议日志（1 MiB 滚动，保留一个 .1 备份）
```

`settings.json` 由 `internal/settings` 读写：

- 结构：`{version, lastAgent, lastWorkspace, mcpServers, paths:{shell,node,pi,commandCode}}`。
- 保存时机：切换标签、选择 agent/目录、Connect 校验成功（也覆盖手输工作区）、MCP Apply、退出。写入是原子的（同目录临时文件 + rename，0600），父目录不存在时创建。
- 加载容错：文件缺失用默认值；JSON 损坏、MCP 不是数组、相对路径、未来版本只产生警告（工具栏显示），其余字段照常使用，绝不阻塞启动。
- 路径优先级：`ACP_GUI_*` 环境变量 > `settings.json` 的 `paths` > 登录 shell 解析。显式覆盖无效时保留 shell 结果并警告，不会把可用的路径清空。
- 只保存最后一次状态（当前标签的 agent/工作区/MCP），不保存多标签布局、图片草稿或消息；历史会话仍由 agent 自己保存。
- 不存 registry 快照，也不缓存探测结果：启动时重新检测。

`main.go` 把 `mygo.App.Path(mygo.PathUserData)` 传给 `ui.Options.DataDir`；`DataDir` 为空时完全不落盘（测试、便携运行）。

日志由 `internal/logs` 写入 `<data>/logs/<agent>.log`：

- 记录连接头（时间、agent、动作、工作区、命令行）、agent stderr 原样输出、`[protocol]` 错误、`[probe]` 发现失败和 `[exit]` 退出原因。
- 单文件上限 1 MiB；写满时旧文件改名为 `<agent>.log.1`（覆盖上一份备份），新文件继续写。文件模式 0600。
- UI 每个标签另保留最近 64 KiB stderr 供 Diagnostics 展示；完整历史看日志文件。
- 日志可能包含 agent 输出里的敏感信息，只在用户打开 Diagnostics 时显示，不在主界面展开。
- 写日志失败不影响会话，只丢掉文件副本。

## 安全与边界

- 只探测内置表里的 pi / command-code，不遍历可执行文件，也不接受表外 agent。
- 探测超时 5 秒；每次探测都要能杀掉进程组，不能残留。
- 不静默安装任何 agent；未安装时只展示安装提示/命令。
- agent 继承用户环境；不在日志里主动打印 API key。stderr 可能含敏感信息：主界面不展开，Diagnostics 里可查看，日志文件 0600。
- 不声明 `fs/*`、`terminal/*` 能力；未知 agent→client 请求一律拒绝。
- 不管登录与模型可用性：不检测凭据/配额，不代跑登录命令，不在应用内打开终端。
- 不管理 Node 运行时（不捆绑、不安装、不升级），只定位用户已有的 Node。
- vendored pi-acp 固定版本并附 LICENSE；更新走显式脚本，不自动跟随 npm。

## 相关文档

- [Agent 发现](discovery.md)
- [pi 集成](pi.md)
- [Command Code 集成](command-code.md)
- [会话能力与 UI](features.md)
- [测试与计划](plan.md)
