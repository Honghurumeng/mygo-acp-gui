# 架构

## 目标

原生桌面 ACP GUI：Go + [MyGo](https://github.com/egoist/mygo) 写界面，检测内置 agent（目前 pi、command-code、codex）是否可用，选择 agent 和工作区后聊天；右侧还有 Git 与串行 Tasks 面板。

早期 Node 原型（`server/` + `public/`）已移除，只保留在 git 历史中；当前实现是仓库根目录的 Go + MyGo 工程。

## 整体架构

```
MyGo 原生窗口 (Go + ui)
  ├─ 左侧工作区/会话栏（每个会话一个 agent 进程 + ACP session）
  ├─ 顶部会话标题栏 + MCP/诊断操作
  ├─ 消息区 + 可拖动高度的 composer
  ├─ composer 底栏：附件、Jump、git 分支、mode、上下文环、agent/model/thinking、发送
  ├─ @ 文件面板：workspace 索引 → 只把 @相对路径 文本写进 prompt（agent 自读）
  ├─ 右侧面板（可折叠，Git / Tasks 两个 tab）
  │    ├─ Git：提交、暂存/更改、分支图与 fetch/pull/push
  │    └─ Tasks：任务队列、executor / checker 双 agent、成功总结 + 每任务一次 commit、失败回滚
  ├─ ui/markdown.Stream / View      ← agent_message_chunk
  ├─ ui.List(FollowEnd)             ← 消息列表
  ├─ ui.Collapsible                 ← thought / tool_call
  ├─ ui.Popover / PopoverBase       ← 模型/provider/thinking、mode、会话操作
  └─ ACP client (Go)
        ↕ newline-delimited JSON-RPC 2.0 (stdin/stdout)
     agent 子进程（pi-acp、command-code、codex-acp）
        ↕
     agent 自己的模型/工具运行时
```

## Go 包划分

```
仓库根/
  main.go                  MyGo 应用入口、窗口重建与退出清理
  main_test.go             UI 冒烟测试
  mygo.json                App 元数据与打包配置
  internal/acp/            JSON-RPC、请求/通知、权限、行分帧（已实现）
  internal/agent/          内置 agent 表、launch spec、登录 shell 路径解析、工作区校验、状态探测
  internal/session/        进程组 + ACP 会话生命周期 + update/permission 分发（已实现）
  internal/settings/       settings.json 读写、容错与原子落盘（已实现）
  internal/archive/        sessions.json 本地会话存档（agent/session 关系、show/showName）
  internal/tasks/          tasks.json 任务索引 + workspace/tasks/*.md 总结读写
  internal/logs/           每个 agent 的滚动日志文件（已实现）
  internal/git/            Git 探测、status 解析、提交与远端操作、任务失败回滚快照（不经过 shell）
  internal/ui/             多会话侧栏、聊天、流式 Markdown、权限队列、系统通知、Dock 角标、Git 面板、主题系统、窗口更新（已实现）
  internal/ui/icons/       pi / Command Code / Codex 图标（SVG，侧栏 + 标题栏）
  resources/
    agents/pi-acp/
      pi-acp.bundle.mjs    单文件 bundle，启动时直接 node 执行（见 pi.md）
      package.json/VERSION/LICENSE/licenses/THIRD_PARTY_NOTICES.md/SHA256SUMS
    agents/codex-acp/
      codex-acp.bundle.mjs 官方 codex-acp 单文件 bundle，CODEX_PATH 指向系统 codex（见 codex.md）
      VERSION/LICENSE/licenses/THIRD_PARTY_NOTICES.md/SHA256SUMS
```

分层原则：`internal/acp` 不 import UI；探测逻辑可独立测试；UI 只读状态、发命令。

## 工程与构建

- `go.mod`：module `github.com/reed/acp-gui`，Go 1.27.3（`GOTOOLCHAIN=auto` 会自动拉取）。
- MyGo CLI 通过 `tool github.com/egoist/mygo/cmd/mygo` 固定在 `go.mod`，统一用 `go tool mygo`。
- 无前端构建：原生 UI，入口 `main.go`，配置在 `mygo.json`。
- App：name `ACP GUI`、slug `acp-gui`、identifier `dev.reed.acp-gui`、version `0.4.0`。
- `go tool mygo build` 产物在 `build/<os>-<arch>/`（已 gitignore）；`resources/` 整目录进 `.app`。
- 当前已实现 `internal/acp`、`internal/agent`、`internal/session`、`internal/archive`、`internal/ui` 的 MVP 链路（见下方“当前链路”）。

## 当前链路

```
启动 app
  → internal/settings 读取 PathUserData/settings.json（上次 agent/工作区/MCP、外观/主题、分区字号、路径覆盖）
  → internal/archive 读取 PathUserData/sessions.json，立即按 cwd 展示已保存会话（不等 agent）
  → internal/agent.ResolveEnvironment（环境变量 > settings paths > 登录 shell；node/pi/command-code/codex 绝对路径）
  → internal/agent.BuiltinLaunchResults
      pi: vendored bundle + Node 22+
      command-code: <绝对路径>/command-code acp
      codex: vendored bundle + CODEX_PATH=<绝对路径>/codex + Node 22+
  → internal/session.Probe（initialize，5 秒，逐 agent 并行）
  → 侧栏工作区/会话 + 工作区 ＋：每个会话各自 internal/session.Start
      session/new | session/load（DeferUpdates 缓冲历史）
  → session/prompt（文本 + 图片；@ 文件以路径文本出现）
  → session/update → 所属 *Chat → Window.Update → markdown / tool / plan / usage
  → session/set_config_option / set_mode
  → 点击存档行 → 按记录里的 agent + sessionId 执行 session/load 重放历史；cwd 不同则新建进程
  → 右侧 Git 面板 → 异步探测 workspace 仓库 → status/log 展示与 stage/commit/fetch/pull/push
  → 右侧 Tasks 面板 → 串行任务：新 executor 会话 → 回合结束取最后一段 → 隔离 checker 进程携带前几轮上下文判定
      → incomplete 回复继续；complete 写 tasks/<id>-<slug>.md → git add -A + commit（不 push）→ 下一个
      → 失败 / 8 轮未过 → 恢复任务快照丢弃成果 → 写总结并标记 complete/failure，不 commit → 继续下一个 pending
  → 首次连接成功 → session/list 合并进存档（保留 show/showName）
  → Stop → session/cancel
  → 删除会话 → 存档 show=false（软删除）+ Session.Close → kill(-pgid)
  → 关闭窗口 → 只销毁窗口：App 留在后台，会话与 agent 进程继续运行
  → 退出（⌘Q / Quit）→ 关闭所有会话的进程组
```

- 解析并探测 pi、command-code、codex；单个 agent 缺失不会隐藏另一个，侧栏状态展示 `ready` / `not-installed` / `installed-not-acp` / `needs-auth` / `error`。内置 agent 表固定为 pi、command-code、codex，不做 registry 发现。
- 路径解析优先使用 `ACP_GUI_NODE` / `ACP_GUI_PI` / `ACP_GUI_COMMAND_CODE` / `ACP_GUI_CODEX` 的绝对路径覆盖，其次是 `settings.json` 的 `paths`，否则用登录 shell 的 `PATH`。只定位已有安装，不安装、不捆绑 Node。
- UI 状态只在主线程修改。session 回调通过 `Window.Update`（测试中通过队列）提交，不直接改状态。
- 每个会话是一个 `ui.Chat`，保存自己的 Session、Info、消息、输入草稿、配置、附件和连接代数；App 持有 `Chats`、`Active`、工作区列表和侧栏折叠状态。回调捕获 chat 指针，因此后台会话仍能流式更新。
- `App.Archived` 是本地会话索引，按 `(agentId, sessionId)` 去重；`SavedSessions` 只是它排序后的 UI 视图。启动时侧栏不调用 `session/list`，因此 agent 慢、未登录或未安装不影响历史列表。
- 测试用 `scripts/fake-agent.mjs`：prompt 流式输出、`/slow` + cancel、`/permission`、config/mode、list/load/close、图片/MCP 计数、命令/plan/usage、任务 executor/checker 标记、子进程清理。

## ACP 客户端

- 分帧：严格按 `\n` 切分；允许行尾 `\r`；**不要**按 `U+2028` / `U+2029` 切分。
- 两向消息：
  - 客户端 → agent：request（有 `id`）、notification（无 `id`）
  - agent → 客户端：notification（`session/update`）、request（`session/request_permission`）
  - 未知 agent→client 请求：回 `-32601 method not supported`
- pi / codex 用 vendored 单文件 bundle 启动，不经过 npx，也不在启动时构建（见 [pi.md](pi.md)、[codex.md](codex.md)）
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
- 应用退出时必须清理所有 agent 进程；`Session.Close` 幂等，`App.Shutdown` 取消所有会话的连接并关闭 sessions 中登记的全部进程组。
- 关闭窗口不是退出：窗口销毁后 `App` 继续在后台运行（macOS 保留 Dock 图标），`App.Detach` 只解除窗口，`Post` 仍通过已关闭窗口的 `Window.Update` 在主线程落地（不重绘），所以后台会话继续流式、权限继续排队，状态不丢。
- 重新打开窗口：macOS 点 Dock 图标触发 `OnActivate(hasVisibleWindows=false)`，`main.go` 用同一个 `App` 新建窗口并 `App.Attach`，先补上关闭期间排队的更新，再画第一帧。
- 非 macOS 平台没有 Dock 图标可恢复窗口，关闭最后一个窗口时仍走 `App.Quit()`，避免留下无界面的进程。
- stderr 单独收集，保留末尾 64 KiB；初始化/会话失败时附在错误里（截断展示）。

## MyGo UI

- 消息流：`markdown.Stream` 显示进行中的回复，结束后切换 `markdown.View`（同一位置）。
- 聊天列表：`ui.List`，`FollowEnd` + `FollowAnimation = &ui.Smooth`；用户上滑后停止跟随。
- 思考/工具：`ui.Collapsible`；工具卡片用 `ui.CollapsibleBase` 做成整卡可点击：默认折叠，标题（指令）单行省略，展开后显示完整指令与入参/输出，再次点击重新折叠。
- 权限：`session/request_permission` 回调阻塞；弹窗用 MyGo `ui.DialogBase` + 本地 `arrive`/`shade` 做遮罩与入场，展示工具名、入参和 agent 给出的 `options`；`rawInput.description` 存在时作为主标题，工具 `title` 降为次行；只有弹窗按钮会作答（选项回 `optionId`，`Cancel` 回 `cancelled`），点遮罩或按 Esc 被吞掉，避免误拒绝。
- 通用外观（按钮、弹窗、空状态、代码块、图标）都在 `internal/ui` 的 `widgets.go` 与 `components.go`，不依赖第三方 UI 库；见 [components.md](components.md)。
- 配置：模型面板按 agent → provider → model → thinking 选择；模型/思考来自 `configOptions`（切换模型时由响应/`config_option_update` 整组替换），mode 来自真实 `modes`，不硬编码。pi-acp v1 把 thinking levels 同时放进 `modes` 的兼容块会被过滤。agent 只在会话开始前可选；未连接的草稿用发现阶段的配置探测缓存显示模型、thinking 和 mode，选择在 Connect 后补发。每个 agent 的 model/thinking/mode 存入 `settings.json` 的 `lastSelections`，新建草稿和切换 agent 时恢复，Connect 时用 agent 返回的列表校验。ACP 只报告当前模型的档位，所以配置探测会在同一个临时进程里把未缓存模型逐个切换并存入 `<data>/model-config.json`（`internal/modelconfig`）；下次启动用模型列表对照该文件，只扫新模型。草稿切到已缓存的 model 直接显示其档位，未缓存时才隐藏并等 Connect 后由 agent 给出。
- 输入：`ui.TextAreaBase` 嵌在可拖动高度的 composer 卡片中；Enter 发送、Shift+Enter 换行；流式期间 Send 切换为 Stop；支持图片附件。
- 多会话：左侧栏按 workspace 分组，先显示打开会话，再显示该目录的已保存会话；打开会话的状态用行背景表达：流式为移动的绿色斜线，其他会话结束/报错为绿底，当前会话结束为蓝底，权限请求为蓝/白闪烁。两类会话合并后默认只列前 5 条，`Show N more` / `Show less` 展开收起，展开状态不落盘（启动时默认折叠）。会话名前的内联图标标明 agent（pi 的方块 mark、Command Code 的 # 徽标、Codex 的终端 mark，`internal/ui/agenticons.go`，随主题变色）；已保存会话用存档里的 `agentId` 画同一个图标，标题栏也用同一图标标当前会话，模型 chip 左侧也用它标当前 agent。工作区头部 `⋯` 可移除该分组（软删除，只隐藏不删记录/会话），移除项通过 “Show removed (N)” 查看并从同一菜单恢复。工作区的 `＋` 新建独立进程；连接在首条消息或打开存档时自动发生。打开会话的 `⋯` 菜单还可 `Close process`：只关闭该会话的 agent 进程，标签页保留为草稿，会话回到存档列表；没有已连接或连接中的进程时该项置灰。
- 权限：模态只显示当前会话的请求并标注 `Chat: …`；后台请求只排队，由侧栏 `!` 提醒，用户切到该会话时才显示，切走时隐藏但保留；回答按所属 `Chat` 路由；遮罩/Esc 不关闭。
- 历史：启动直接从 `sessions.json` 展示；点击存档行按记录的 agent + sessionId 执行 `session/load` 重放历史。已打开会话不重复显示，当前会话已有消息或流式时在新标签页打开。`⋯` 可重命名（showName）或软删除（show=false），删除项通过 “Show deleted” 恢复。
- MCP：JSON 模态，校验后传给 new/load；输入 `/` 时 composer 上方出现命令面板，解析 `available_commands_update`。
- Git 面板：右侧 300 DIP、可折叠，跟随当前会话 workspace；`internal/git` 异步执行 status/log 与 stage/commit/fetch/pull/push，无 git 或非仓库时只显示安装/`git init` 提示（见 [git.md](git.md)）。
- Tasks 面板与 Git 共用右侧列：同一时间一个串行 runner；每个任务新建 executor Chat/session，回合结束后另起隔离 checker 进程，只按任务文本、本轮最后一段和此前轮次上下文判断。checker 通过后写 `tasks/<id>-<slug>.md`、执行一次 `git add -A` + commit（不 push），再开始下一个 pending；失败恢复任务开始前的 work tree 快照、写总结并标记 complete/failure（不 commit），然后继续下一个 pending；只有 Stop 会停队列并退回 pending；支持 pending 编辑/排序/删除和跨重启持久化（见 [tasks.md](tasks.md)）。
- 诊断：顶部 “Logs” 打开 `Diagnostics`，显示发现状态、当前会话的 stderr 尾部、日志路径与 Reveal；stderr 同时写入滚动日志文件。
- 计划/用量：composer 上方 plan；上下文环形进度、turn token 用量和会话标题在 composer/标题栏显示。
- 跨 goroutine 改状态：`win.Update(func(){ ... })`；没有窗口的测试用 `App.Flush` 排队应用。
- 资源：`mygo.App.Path(mygo.PathResources)`；数据与设置：`mygo.App.Path(mygo.PathUserData)`。
- 登录：只展示 agent 的 `authMethods` / 命令，由用户在外部终端自行解决；不内嵌终端。
- 参考 MyGo 示例：`examples/markdown-chat`。

## 数据与配置

存储在 `mygo.App.Path(mygo.PathUserData)`：

```
settings.json        lastAgent、lastWorkspace、workspaces、removedWorkspaces、mcpServers、lastSelections（每个 agent 的模型/thinking/mode）、appearance、theme、notifications、paths 覆盖
sessions.json        本地会话存档：agentId、sessionId、cwd、title、showName、show、会话时间、用户消息发送时间与每轮运行时间
tasks.json           任务索引：workspace、title/text、pending/ongoing/complete、result、顺序、session、轮次、note、summaryPath
<workspace>/tasks/   每个完成任务一份 Markdown 总结（tasks/<id>-<slug>.md）；成功的随任务 commit，失败的只保留文件
logs/                每个 agent 的 stderr / 协议日志（1 MiB 滚动，保留一个 .1 备份）
```

`settings.json` 由 `internal/settings` 读写：

- 结构：`{version, lastAgent, lastWorkspace, workspaces, removedWorkspaces, mcpServers, lastSelections, appearance, theme, notifications, dockBadge, paths:{shell,node,pi,commandCode,codex}}`。`workspaces` 保留全部已知目录（含软删除项），`removedWorkspaces` 是软删除标记，二者都按侧栏顺序保存；`lastSelections` 按 agent ID 保存 `{configOptions:{id:value}, mode}`（模型、thinking、其他 select 和 session mode，见 [features.md](features.md)）；`appearance`/`theme` 缺省为 `system`/`default`（见 [themes.md](themes.md)）；`notifications` 和 `dockBadge` 缺省为 true，显式 false 会持久化。
- 保存时机：切换会话、选择 agent/目录/model/thinking/mode、添加/移除/恢复工作区、切换设置面板里的外观/主题/系统通知/Dock 角标、Connect 校验成功、MCP Apply、退出。写入是原子的（同目录临时文件 + rename，0600），父目录不存在时创建。
- 加载容错：文件缺失用默认值；JSON 损坏、MCP 不是数组、相对路径、未来版本只产生警告（标题栏/侧栏显示），其余字段照常使用，绝不阻塞启动。
- 路径优先级：`ACP_GUI_*` 环境变量 > `settings.json` 的 `paths` > 登录 shell 解析。显式覆盖无效时保留 shell 结果并警告，不会把可用的路径清空。
- 保存最后一次状态（当前会话的 agent/工作区/MCP）、每个 agent 的模型/thinking/mode、侧栏工作区列表和每个工作区的折叠状态；不保存会话布局、图片草稿或消息。
- 会话索引单独放在 `sessions.json`，由 `internal/archive` 读写：`{version, sessions:[{agentId,sessionId,cwd,title,showName,createdAt,updatedAt,userTurns:[{number,sentAt,durationMs}],show}]}`。`(agentId, sessionId)` 唯一；`show=false` 是软删除，`showName` 是本地重命名；`userTurns` 按用户轮次保存本地发送时间和该轮运行时间（prompt 发送到结果返回），供 Jump 和会话流在 `session/load` 重放后显示。pi 的 fork 回放会带每条消息的发送时间；Command Code 和旧 bundle 没有时间元数据，退回本地 `userTurns`（ACP 本身不提供消息时间或耗时）。
- 存档写入时机：session/new、load 成功，标题/轮次更新，重命名、删除/恢复。启动只读存档，且不会从存档补建 workspace；discovery 完成后，每个 ready agent 在后台自动调一次 `session/list` 合并（`cwd: null` 表示全部工作区并走完分页；有已连接进程时复用，否则临时 spawn；失败下次连接重试），之后该 agent 连接成功时再补一次。合并只保留 cwd 属于已添加 workspace 的会话，且绝不覆盖 `show/showName`。`session_info_update.title` 会立即写入存档；启动读存档时会把旧版本用首条 user 消息生成的图片临时路径标题清洗成正文或 `[Image]`（`internal/ui/title.go`），避免侧栏先显示路径再等同步。新增 workspace 时按该 cwd 过滤同步一次。文件为原子写入、0600；损坏 JSON 只在侧栏提示，不阻塞启动。
- 任务索引单独放在 `tasks.json`，由 `internal/tasks` 读写：任务按 workspace 归属，状态固定为 `pending` / `ongoing` / `complete`，complete 带 `pass` / `failure` result；`summaryPath` 是 `tasks/<id>-<slug>.md` 的相对路径。创建、编辑、排序、删除和每次状态变化都原子落盘；启动时把遗留的 ongoing 改回 pending，旧的 complete 行补成 pass。成功总结会随任务 commit 提交；失败总结留在 workspace 但不提交。任务记录删除不删总结文件。
- 不存 registry 快照，也不缓存探测结果：启动时重新检测。

`main.go` 把 `mygo.App.Path(mygo.PathUserData)` 传给 `ui.Options.DataDir`；`DataDir` 为空时完全不落盘（测试、便携运行）。

日志由 `internal/logs` 写入 `<data>/logs/<agent>.log`：

- 记录连接头（时间、agent、动作、工作区、命令行）、agent stderr 原样输出、`[protocol]` 错误、`[probe]` 发现失败和 `[exit]` 退出原因。
- 单文件上限 1 MiB；写满时旧文件改名为 `<agent>.log.1`（覆盖上一份备份），新文件继续写。文件模式 0600。
- UI 每个会话另保留最近 64 KiB stderr 供 Diagnostics 展示；完整历史看日志文件。
- 日志可能包含 agent 输出里的敏感信息，只在用户打开 Diagnostics 时显示，不在主界面展开。
- 写日志失败不影响会话，只丢掉文件副本。

## 安全与边界

- 只探测内置表里的 pi / command-code / codex，不遍历可执行文件，也不接受表外 agent。
- 探测超时 5 秒；每次探测都要能杀掉进程组，不能残留。
- 不静默安装任何 agent；未安装时只展示安装提示/命令。
- agent 继承用户环境；不在日志里主动打印 API key。stderr 可能含敏感信息：主界面不展开，Diagnostics 里可查看，日志文件 0600。
- 不声明 `fs/*`、`terminal/*` 能力；未知 agent→client 请求一律拒绝。
- 本地存档含 cwd 和 session id，只写用户数据目录（0600），不上传、不跨设备同步。
- 不管登录与模型可用性：不检测凭据/配额，不代跑登录命令，不在应用内打开终端。
- 不管理 Node 运行时（不捆绑、不安装、不升级），只定位用户已有的 Node。
- 任务 runner 成功时只执行 `git add -A` + commit，不 push、不 force；失败时用任务开始前的 work tree 快照回滚本次改动（不 reset 到 HEAD），不 commit。任务提交会把当时工作区的全部改动一起提交。
- Git 面板只调用用户已有的 git，不经过 shell、不代登录、不弹出凭据提示；不执行 force push / hard reset / clean。
- vendored pi-acp / codex-acp 固定版本并附 LICENSE；更新走显式脚本，不自动跟随 npm。

## 相关文档

- [Agent 发现](discovery.md)
- [pi 集成](pi.md)
- [Command Code 集成](command-code.md)
- [Codex 集成](codex.md)
- [会话能力与 UI](features.md)
- [UI 组件库](components.md)
- [右侧 Git 面板](git.md)
- [右侧任务面板与串行 runner](tasks.md)
- [测试与计划](plan.md)
