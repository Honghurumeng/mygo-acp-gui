# 会话能力与 UI

本文记录除基础聊天之外的 ACP 能力：多会话、发现探测、配置/模式、已保存会话、图片、MCP、命令面板、计划与用量、Git 与 Tasks 面板。

## 发现与状态

`internal/agent.BuiltinLaunchResults` 返回内置表每一行的结果（ID、名称、安装提示、launch spec 或错误）。UI 对每个成功解析的 spec 并行执行 `session.Probe`：

1. 在临时工作区 spawn agent。
2. 只发 `initialize`，5 秒超时。
3. 成功 → `ready`；超时/崩溃 → `error`；stdout 非 JSON / 握手失败 → `installed-not-acp`；路径/Node 缺失 → `not-installed`。
4. probe 进程总是关闭并清理进程组。

只有 `ready` 的 spec 进入 Agent 列表；侧栏底部显示所有内置 agent 及失败详情。`session/new` 要求登录时，该 agent 状态改为 `needs-auth`，只展示 `authMethods` + `loginHint`。

## 多会话

每个会话是一个独立的 `Chat`：拥有自己的 agent 进程、ACP session、工作区、MCP 列表、消息、配置、草稿和权限请求。会话之间并行运行，切换只改变显示，不会暂停后台流式回复。

选择“每会话独立进程”而不是共享进程里的多个 ACP session：进程级隔离让 cwd、取消、权限和崩溃互不影响，代价是每个会话各有一套 agent 运行时资源。agent 侧的单进程多会话能力并不一致：Command Code 可同进程多会话但必须共用 cwd，pi-acp 当前只保留最新一个活会话；codex-acp 由应用按会话独立进程启动，见 [multi-session.md](multi-session.md) 与 [codex.md](codex.md)。

- `App` 只保存全局状态（内置 agent、发现结果、工作区列表、MCP 默认值、会话列表、当前会话）和 `[]*Chat`。
- session 回调在创建时捕获所属 `*Chat`，按 session 指针路由，不读取“当前会话”，所以后台会话的 `session/update`、退出和权限都能落到正确位置。
- 左侧工作区栏按 `Chat.Workspace` 分组：先显示打开会话，再显示该目录的已保存会话。打开会话的状态用行背景表达（`internal/ui/chatstatus.go`）：流式中是自左向右移动的绿色斜线，其他会话结束/报错是绿色底，当前会话结束是蓝色选区底，等待权限是蓝/白（深色主题为蓝/背景色）交替闪烁。两者合并计算，默认只显示前 5 条，超过时显示 `Show N more` 展开全部，展开后可 `Show less` 收回；展开状态只存在内存里，下次启动恢复为默认折叠。
- 会话名前的内联图标是 agent 标记：pi 用 `pi.dev` 的方块 mark，Command Code 用其扩展里的 `#` 徽标，Codex 用终端 mark（`internal/ui/icons/*.svg`，`//go:embed` 进二进制）。`mui.Icon` 以文字颜色绘制，所以跟随主题；未知 agent（如测试的 fake）不画图标。已保存会话按存档 `agentId` 画图标，标题栏也显示当前会话的 agent 图标，hover 提示 “pi agent” / “Command Code agent” / “Codex agent”。
- 工作区头部右侧 `⋯` 提供 `Remove workspace`：软删除只隐藏该分组，已打开会话、已保存会话和目录本身都不动，记录仍在 `workspaces` 里。移除后侧栏出现 “Show removed (N)”，展开后分组显示为 `[removed]`，同一菜单可 `Restore workspace`；重新通过 `＋` 添加或打开该目录下的存档也会恢复。
- 会话右侧 `⋯` 打开重命名/删除；`Close process` 只关闭该会话的 agent 进程，标签页保留为草稿并回到存档列表，没有已连接或连接中的进程时该项显示为不可用。删除会关闭该会话的进程，并把对应存档记录标为 `show=false`。工作区上的 `＋` 在该目录下新建独立会话。
- 启动时默认创建一个草稿会话；在草稿里输入第一条消息时自动连接，成功后立即发送。
- 每个会话保留自己的消息列表滚动位置、输入草稿、图片附件、配置选择和计划面板。
- 顶部工具栏只保留 `Logs` 和 `MCP…`（连接中显示 `Cancel`）；右侧面板的开关改由窗口最右缘的窄图标栏完成，会话由工作区上的 `＋` 创建。
- 关闭窗口只是关闭窗口：应用留在后台，所有会话（包括流式中的回复）继续运行；macOS 再点 Dock 图标会按当前状态重新开窗。只有退出（⌘Q / Quit）才取消连接并关闭进程组。
- 应用退出时取消所有正在连接的会话，并关闭所有已登记 session 的进程组。

权限弹窗只显示当前会话的请求：后台会话请求权限时只排队并点亮侧栏 `!`，不会切换会话或弹窗；用户主动打开该会话后才显示，切走时弹窗隐藏并保留请求。回答按 `Chat` 路由。弹窗只能通过自身按钮作答，点遮罩或按 Esc 不会关闭，也不会回 `cancelled`。

## ACP 状态提示

pi-acp 的启动信息（pi 版本、Extensions、Skills 等）、重试、自动压缩和排队状态不是模型回答。fork 给这些 `agent_message_chunk` 打 `_meta.piAcp.startupInfo` 或 `_meta.piAcp.notice`；GUI 丢弃启动信息，把 notice 画成居中的系统行，同一次重试/压缩/排队的开始和结束更新同一行。扩展 `ctx.ui.notify` 也按系统提示显示。状态行会结束当前 assistant 行，状态之后的文本另起一行，不会把“Retrying…”和回答粘在一起。

## 系统通知

权限请求和一轮回答结束时会发系统通知（`internal/ui/notify.go`，`mygo.NewNotification`），只在用户看不到应用内提示时才发：窗口已关闭/失去焦点，或事件发生在非当前会话。当前会话 + 前台窗口里，权限弹窗、侧栏标记和 `Took` 行已经足够。

- 权限：标题 `Permission needed`，副标题是会话名，正文优先用工具的 `rawInput.description`，其次 `title`；同一会话复用通知 ID，后来的请求替换前者。
- 完成：标题 `Task complete`，失败为 `Task failed`；正文是最后一条 assistant 回答压成单行并截断，没有正文时显示 `Finished in …`。用户自己 Stop 的 `cancelled` 回合不发通知。任务 executor 的中间回合不发通知，整个任务结束（pass 或 failure）时按任务 ID 发一次通知（副标题是任务标题）。
- `Group` 按 agent + ACP session ID（草稿按 chat ID）分会话，完成和权限各自用稳定 ID，重复事件不会堆叠同一类通知。
- 通知在后台 goroutine 调 `Show`，不阻塞 UI；macOS 需要打包应用（`mygo dev` / `mygo build`），Linux 走 D-Bus，Windows 是通知气球；`NotificationsSupported` 为 false 时静默跳过。点击通知会打开/聚焦窗口，应用变为 active 时 `ClearNotifications` 清掉已看过的通知。
- 测试通过 `App.notify` 注入记录器，不启动桌面后端。
- 设置面板的开关持久化为 `notifications`，默认开启；关闭后 `canNotify` 直接返回 false，权限和完成通知都不再发送。

Dock 角标（`internal/ui/badge.go`，`mygo.App.SetBadgeCount`）显示等待用户的会话数：权限待回答（`Attention`），或回合已结束且未读（`Unread && !Streaming`），每个会话最多算一个。流式中的后台会话先不计数，回合结束后才加入；选中会话、回答权限、关闭会话或删除标签都会减少，归零即清除。

回合结束时如果窗口已关闭或失去焦点，当前会话也会标记 `Unread`（`markChatUnread`），所以关窗后完成的回合同样会让角标亮起。重新打开或重新聚焦窗口会把当前会话视为已读并清掉它的角标，后台会话的未读仍保留；窗口关闭期间角标一直保留。

- 设置面板的 `Dock badge` 开关持久化为 `dockBadge`，默认开启；关闭时立即清空当前角标，之后不再更新。
- `updateDockBadge` 缓存上次发送的数字，session 事件再频繁也不会重复调用平台；`App.SetBadgeCount` 在 macOS 画在 Dock 图标上，Linux 走 Unity launcher API，Windows 不显示（无操作）。
- 测试通过 `App.setDockBadge` 注入记录器；没有窗口（测试环境）时不调用 MyGo。

## 设置面板

- 侧栏左下角 agent 状态（名称、CLI 版本、ready 等状态）右侧的齿轮按钮打开设置面板；也可以从代码调用 `openSettings`。面板左侧是分类列表，右侧是选中分类的内容，当前有 `Appearance`、`Fonts` 和 `Notifications`，后续分类在同一 `SettingsSection` 切换。
- `Appearance` 分类提供 `System`/`Light`/`Dark` 外观和 `Default`/`Nord`/`Solarized`/`Catppuccin`/`Claude` 配色，改动立即生效并写入 `settings.json`；详见 [themes.md](themes.md)。
- `Fonts` 分类分别缩放 workspace 列表、对话流、输入框和 Git 面板，范围 80%–150%，改动立即生效并持久化；详见 [fonts.md](fonts.md)。
- 开关改动立即生效并写入 `settings.json`；测试按钮发送一条标题为 `ACP GUI` 的桌面通知，不依赖窗口焦点，用于检查系统通知权限。通知关闭时测试按钮禁用，并提示先开启。
- `Dock badge` 开关与系统通知开关并列，独立持久化；切换后立即重算或清空角标。默认开启，不依赖通知开关。
- MCP 编辑仍走顶部 `MCP…` 的独立模态，因为它修改的是当前会话的服务器列表，不放进通用设置分类。

## 配置与模式

`Info` 解析 `configOptions`（model、effort 等）和 `modes`，并兼容只有 `models` 的旧 agent（合成为 `model` 配置项）。ACP 的权威来源是 `configOptions`：agent 在 `session/new` / `session/load` 返回完整列表，在 `session/set_config_option` 的响应和 `config_option_update` 通知里返回**整个新列表**；客户端必须整份替换，不能沿用切换前的档位。

旧版 pi-acp 还把 thinking levels 塞进 `modes`（name 为 `Thinking: …`），同时提供 `thought_level` config option。这些不是权限/session mode，客户端在 `modes` 的每个 id 都命中 thinking 选项值时丢弃整个 `modes` 块及其 `currentModeId`，避免出现一份模型切换后不会刷新的重复“Session mode”菜单；Command Code 的 `default` / `plan` 等真实 mode 不受影响。

- composer 的模型 chip 打开面板，四列为 agent / provider / model / thinking；provider 由模型值的 `/` 或 `:` 前缀分组，没有前缀时归入 `Models`。可用 provider 只来自 agent 返回的 model `configOptions`：ACP 没有单独的 provider 可用性接口，模型选项就是 agent 对「当前可用模型」的声明。面板每列按内容取宽、内容左对齐，整个面板与 chip 右对齐，列表变宽时向左展开。chip 左侧画当前 agent 的品牌图标（未知 agent 不画），图标放在 chip 内部且只有 11pt，不改变 22pt 的控件行高。chip 不设固定最大宽度，完整显示模型名和 thinking 档位；动作行放不下时，右侧控件组整体换行，chip 不缩略。
- 草稿（还没有 session）也能选 provider / model / thinking：面板数据来自发现阶段的配置探测（`AgentConfigs`），选择存在 `Chat.PendingConfig`，Connect 拿到 session 后按 model 优先的顺序调 `session/set_config_option` 补上；断开连接时把当前值回填为草稿选择，重连不丢。ACP 没有“查询某个未连接模型有哪些 thinking 档位”的接口，所以发现阶段会一个进程把模型列表里缺的模型逐个 `set_config_option` 扫一遍，把每个模型的 `configOptions` 存进 `<data>/model-config.json`；草稿切到已扫过的 model 时直接用它渲染 thinking，未扫到（首次运行、超时或切换失败）才隐藏旧档位并提示先 Connect，同时清掉旧 model 的 thinking 草稿。
- 草稿的 mode chip 也来自同一次配置探测（`AgentModes`），因此不用发第一条消息就能看到 agent 的 `default` / `plan` 等模式。草稿选择存在 `Chat.PendingMode`，Connect 后、首条 prompt 前调 `session/set_mode`；断开时把当前 mode 回填，重连不丢。切换 agent 时先清掉旧 agent 的草稿 mode，再填入新 agent 的上次选择。
- 每个 agent 的模型面板选择会持久化：`settings.json` 的 `lastSelections[agentId]` 保存所有 select 型配置项的当前值（model、thinking 及其他扩展 select）和 session mode。选择 model/thinking/mode 后立即写入（草稿与已连接会话都一样）；启动草稿、`＋` 新建会话、切换 agent 时先填入该 agent 的上次选择，Connect 仍按 `session/new` 返回的 `configOptions` / `modes` 校验，已下架的 model、档位或 mode 直接跳过。provider 不单独保存，由恢复后的 model 值推导。打开已保存会话走 `session/load`，不覆盖会话自己的配置；加载后实际返回的 model/thinking/mode 会作为该 agent 的最新选择写回，供下一个新草稿使用。
- 启动时的配置缓存不是永久事实：草稿每次打开模型面板都会异步重读 agent 的 model 列表（正常 30 秒节流，缓存为空时 5 秒重试；失败保留旧列表，读取中没有 session 也保留），所以登录/改配置后不再出现的 provider 会在刷新后消失；连接成功、`config_option_update`、切换配置和断开时都用真实 session 的 `configOptions` 更新缓存，并写回 `model-config.json`。启动时把 `session/new` 的模型列表和该文件对照，只扫新模型；缓存完整时只做一次 `session/new`。`PendingConfig` 里已被刷新掉的 model 在 Connect 时跳过，不会静默换用别的模型。每模型缓存只按“模型值是否已存在”判断，不设 TTL：模型档位变化（例如升级 pi 后同一模型多出 `xhigh`）不会自动刷新，需要删掉该文件重扫。
- agent 只在会话开始前可选；连接后锁定。切换 agent 后重新 Connect 才生效；切换瞬间清掉属于旧 agent 的草稿配置选择，并恢复新 agent 在 `lastSelections` 里的 model/thinking/mode。agent 本身仍由 `lastAgent` 恢复。
- 选择 model / thinking 调用 `session/set_config_option` `{sessionId, configId, value}`；其他 select 型配置追加显示在 thinking 列。thinking 选项以 agent 当前返回的 configOptions 为准，pi fork 会按当前模型重算（如 `low/high/max`，含 `max`）；响应或 `config_option_update` 里的 `thought_level` 会整组替换，已消失的档位立即从面板和 chip 中移除。
- 模式 chip 显示当前 mode；连接后点击调用 `session/set_mode` `{sessionId, modeId}`，草稿中点击只记录 `PendingMode`，由 Connect 在 `session/new` 之后统一应用。mode 只来自真实的 `modes`（或未来的 mode config option）；pi-acp v1 的 thinking 兼容块会被过滤，不会渲染成 mode。
- 两者都更新 `Info`，并接收 agent 的 `config_option_update` / `current_mode_update` 通知。pi-acp 的 `current_mode_update` 在承载 thinking level 时，如果该值属于 thinking 选项，就不写入 `CurrentModeID`；真正的 thinking 当前值以 `thought_level.currentValue` 为准。
- 流式回合、连接中或即将切换会话时禁用，避免 agent 返回 `-32600`。
- Command Code 的 `effort` 和 Codex 的 `reasoning_effort` 可能随模型变化；选项一律以 agent 返回为准，不硬编码。

## 已保存会话与本地存档

本地存档 `<data>/sessions.json` 是侧栏历史列表的来源，由 `internal/archive` 读写。它把会话列表、session ID 和 agent 关联起来，并保存三个纯本地字段：

- `show`：`true` 正常显示；`false` 表示用户删除，只从侧栏隐藏，记录不删。
- `showName`：用户重命名后的显示名，优先于 agent 返回的 `title`。
- `userTurns`：用户每条消息的本地发送时间和该轮运行时间，形如 `[{number, sentAt, durationMs}]`；`number` 是会话中从 1 开始的用户轮次。pi 的 fork 在回放里自带每条消息的时间，Command Code 没有；没有 agent 时间时 Jump 弹窗用这里的 `sentAt`，会话流用 `durationMs` 在重放后重现 `Took …`。

记录结构：`{agentId, sessionId, cwd, title, showName, createdAt, updatedAt, userTurns, show}`；`userTurns` 条目为 `{number, sentAt, durationMs}`；`(agentId, sessionId)` 唯一，这样不同 agent 的相同 session ID 不会互相覆盖。

- 标题清洗：agent 常拿首条 user 消息当兜底标题；带图片的 prompt 会以图片临时路径开头（例如 `/var/folders/.../pi-clipboard-....png`）。`internal/ui/title.go` 去掉开头的绝对路径、保留后面的正文，只有路径时显示 `[Image]`；读存档、合并 `session/list`、接收 `session_info_update` 时都会清洗，旧存档里的路径标题在启动时改写回 `sessions.json`。

- 启动时先读存档并立即按 `cwd` 分组展示，不等 agent 发现，也不自动调 `session/list`。已打开会话不重复显示。
- session/new / load 成功、标题更新和一轮结束后，自动把 session ID、agent、cwd、agent title 和 `updatedAt` 写进存档；不覆盖 `show/showName`。
- App 不在本地保存消息正文，只保存用户消息的发送时间和每轮运行时间；右侧历史正文仍只能由 agent 重放。点击存档行按记录中的 `agentId` 启动/复用对应 agent，并执行 `session/load` 重放历史；当前会话已有消息、流式、目标 agent 与当前进程不同，或 `cwd` 与当前进程工作区不同时，在新标签页用该会话的 `cwd` 新建进程（Command Code 的 ACP 进程只服务启动时的工作区）。pi 回放时优先用 agent 给出的每条消息时间；其他 agent 或没有 replay 元数据时，按用户轮次把 `userTurns` 里的时间贴回对应消息。两者都没有的历史消息显示 `—`，不借用相邻消息的时间。
- 一轮结束后，从 prompt 发送到结果返回的耗时写入该轮 `durationMs`，并在会话流末尾追加一行右对齐的 muted `Took 1m 23s`；取消和错误也计算到结束为止。`session/load` 重放时按轮次把同样的行插到下一轮用户消息之前（最后一轮放在历史末尾）；没有本地记录的历史轮次不显示，也不猜测耗时。失败轮次也占一个用户轮次，不能被下一条用户消息吞并，否则后续时间和 `Took` 行会整体错位。
- 行尾 `⋯` 只提供 Rename、Delete；打开存档一律走 load。Rename 写 `showName`；Delete 只写 `show=false`，不关 agent 的外部历史。
- 默认不显示 `show=false` 的行；侧栏 “Show deleted (N)” 展开，`⋯` → Restore 恢复。
- discovery 完成后，每个 ready agent 会在后台自动拉一次 `session/list` 合并进存档；即使从未连接过该 agent，新增工作区也能看到它的历史。之后该 agent 首次连接成功时再同步一次，用来重试启动时失败的导入。请求显式发送 `cwd: null`（不是省略字段）表示全部工作区，并跟随 `nextCursor` 取完所有分页；否则 pi-acp 会回退到当前进程工作区，只导入一个工作区的历史。有该 agent 的已连接进程（后台标签页也算）时直接复用，否则临时 spawn 一次。同步失败（超时、进程退出）会清除该 agent 的已同步标记，下次连接重试。
- 导入只保留 `cwd` 属于已添加 workspace 的会话；其他项目的记录直接丢弃。导入和启动读存档都不会自动新增 workspace 分组，分组只来自用户显式添加、打开会话或选择目录。用户新增 workspace 时，对该目录按 `cwd` 过滤再同步一次，让已有历史立即出现。
- 合并只补 `cwd/title/updatedAt` 和新记录，不改变 `show/showName`，所以已删除/已重命名的会话不会在同步后复活或改名。合并不会用 `[Image]` 这种纯图片兜底覆盖已有的更具体标题。
- 从其他工作区打开时，把存档里的 `cwd` 传给 load、加入侧栏工作区列表；路径比较解析 symlink，避免同一目录出现两个分组。
- 存档写入是原子的（同目录临时文件 + rename，0600）。文件缺失视为空；损坏 JSON、未来版本、缺少 agentId/sessionId 或重复记录只产生侧栏警告，其余记录照常展示，不阻塞启动。
- 历史重放事件在 `Start` 阶段缓冲，UI 安装好 Session 后调用 `Activate` 按顺序消费，避免丢失。`user_message_chunk` 的 image 内容块按顺序解码成消息缩略图：pi、Command Code 和 codex 的 `session/load` 都会回放用户图片，不再只保留文本。
- 重放中的 `user_message_chunk`、`agent_message_chunk` 和 `agent_thought_chunk` 合并成连续的用户/助手行，结束后统一标记 Done。pi-acp 给每条持久化消息带 `_meta.piAcp.replay.key`；key 变化时强制新行，因此失败 assistant（空内容、无 chunk）两侧的 user 消息不会粘连。空的 user 消息也会保留一行，保证轮次计数。
- pi 的失败轮次回放成居中的 `Turn failed: …` / `Turn aborted.` 系统行，不再从恢复后的历史里消失。
- `agent_thought_chunk`（实时 `thinking_delta` 和历史回放都走这个类型）累积到当前助手行的 `Thought`，渲染为回答上方的「Thinking」折叠块，默认收起；只有推理没有正文的空行不会画出来。

## 图片

- 文件对话框选择 png/jpg/jpeg/gif/webp/bmp，最多 4 张、每张 15 MB。
- 读取后转 base64，`ContentBlock{type:"image", mimeType, data}` 与 text 一起发 `session/prompt`。
- 预览由 `ui.DecodeBitmap` 生成；composer 附件行和用户消息只显示缩略图，不显示文件名或 MIME 类型。点击缩略图打开图片预览 modal（`Close`、点击外部或 Esc 关闭），按窗口可用空间和图片原始尺寸缩放，不放大超过原图。
- composer 左侧 `＋` 打开图片选择；已连接且 `promptCapabilities.image` 为 false 时禁用，草稿阶段允许先选图。
- 输入框支持直接粘贴剪贴板图片：`OnPaste` 只在剪贴板文本为空时读取 `mygo.Clipboard.ReadImage()`，把 PNG 字节转 base64 后作为 image 块；不写临时文件、不把路径塞进文本。文本剪贴板仍走编辑器原生的文本粘贴，不会同时生成附件。
- 图片粘贴与 `＋` 共用数量、大小和 `promptCapabilities.image` 校验；剪贴板图片在后台解码，UI 线程只追加附件。
- 不做内嵌终端、不代传本地路径；图片字节直接进 ACP。

## @ 文件引用

- 在输入框的空白词首输入 `@` 打开文件面板；查询按 basename 优先、路径其次、最后 fuzzy subsequence 排序，最多显示 50 条。`↑`/`↓` 选择，`Enter`/`Tab` 插入，`Esc` 关闭。
- 条目布局和右侧 Git 面板一致：文件名在前，所在的 workspace 相对目录以更小号 muted 文字紧随其后，大小在最右；同名文件因此可凭目录区分。
- 文件索引在后台扫描当前会话 workspace：跳过 `.git`/`node_modules`/`vendor`/`build` 等目录，最多 20000 个文件，跳过带空白或 `@` 的名字（`@token` 以空白分隔）；每个会话缓存一次，切 workspace 后重建。扫描期间面板显示 `Indexing workspace files…`，失败只显示在面板里，不阻塞聊天。
- 选中后输入框插入 `@相对路径 `，光标移到插入内容之后，可以直接继续输入；@ 文件不是附件，composer 上方的附件行只显示粘贴/选择的图片，不显示 @ 文件（即使它是图片）。删掉文本里的 `@token` 就等于取消引用。
- 发送时只把 `@相对路径 ` 文本放进 `session/prompt` 的 text 块，不发送 `resource` / `resource_link`，也不内嵌文件正文：agent 以自己的 cwd 为 workspace，会按路径自行读取；不需要也不声明 `fs`/`terminal` 客户端能力。
- 用户消息下方仍显示文件 chip 作为可读提示（只保留文本里仍有对应 `@token` 的文件）；点选记录只在当前草稿/会话内存里，不写入 settings.json 或 sessions.json，历史重放以 agent 返回的 user/assistant 内容块为准（当前渲染文本和用户图片）。

## MCP servers

- “MCP…” 模态编辑 JSON 数组，支持 stdio（`name` + `command`/`args`/`env`）和 HTTP/SSE（`type:"http"` 或 `"sse"` + `name` + `url`/`headers`）。
- 保存前校验：必须是对象数组、必须有 `name`，stdio 必须有 `command`，http 必须有 `url`。
- 每个会话各有一份列表；设置窗口修改当前会话，并作为新会话的默认值。列表传给该会话后续的 `session/new` / `session/load`；已连接时也更新 Session 的待用列表。
- 不启动、不管理 MCP 进程；由 agent 自己拉起。
- Apply 后的列表写入 settings.json，成为下次启动的默认值（见“设置持久化”）。

## Git 面板

- 右侧 300 DIP 可折叠面板，跟随当前会话/草稿的 workspace；窗口最右缘的窄图标栏里 Git 图标按钮开关，当前 tab 的图标按钮高亮为 Accent 色。
- 异步探测 `git` 是否存在和 workspace 所属仓库根目录；没有 git 显示安装提示，没有仓库显示 “Initialize Git repository” 按钮（`git init`）。
- 有仓库时显示 commit message、Staged / Changes 列表、分支状态和提交图（`internal/ui/gitgraph.go` 自绘，结构化 log + lane，行高 22 DIP）；文件可逐项 stage/unstage，也可 Stage all / Unstage all。点文件名可打开 diff 预览，点提交行可展开该提交改动的文件列表，列表里的文件可打开该提交的 diff。提交行不再显示短 hash；hover 350 ms 弹出详情卡（subject、作者、时间、refs、修改文件数/代码增删行数（单行）、完整 hash），卡片位于图表左侧，卡内可复制完整 hash。
- Commit 只提交暂存内容；Fetch/Pull/Push 不等待凭据终端，Undo 用 soft reset 保留改动；所有命令有超时和 generation 校验，切换会话不会串仓库。
- 详细设计见 [git.md](git.md)。

## Tasks 面板

- 右侧与 Git 面板共用 300 DIP 列，窗口最右缘图标栏的 Tasks 图标按钮打开、切换或关闭；面板内只显示当前面板名称，不再有内建 tab 条。Tasks 跟随当前会话 / 草稿的 workspace。
- 每个 workspace 有有序任务；状态只有 `pending`、`ongoing`、`complete`，complete 带 `pass` / `failure`。`Run pending` 串行跑完所有 pending，`Stop` 把当前任务退回 pending 并停止队列；失败任务留在 complete(failure)，但队列继续执行剩余 pending。
- 每个任务新建独立 executor Chat / agent 进程 / ACP 会话；回合结束后取最后一段 assistant 文本，另起隔离 checker 进程和新会话判断。checker 收到任务文本、本轮最后一段以及之前每轮的总结 + feedback，没有工具；返回严格 JSON。
- incomplete 时把 checker reason 发回同一个 executor 会话继续；complete 时写 `<workspace>/tasks/<id>-<slug>.md`，执行一次 `git add -A` + commit（不 push），再开始下一个任务。
- 失败时先恢复任务开始前的 work tree 快照（丢弃本次成果），再写总结并标记 complete(failure)，不 commit，然后继续执行剩余 pending；只有用户 Stop 才停队列。非 Git 仓库或快照失败时在 note 里说明无法回滚。
- 任务索引保存在 `<data>/tasks.json`，任务记录、result、轮次、note、summaryPath 跨重启恢复；上次退出遗留的 ongoing 自动改回 pending。只有 pending 可编辑 / 排序；删除任务保留已写出的 Markdown。
- complete 任务可打开浮动总结卡片：默认 Markdown 渲染，标题栏 `Diff` 切换成 all-additions 的 Git diff 视图；failure 行显示 `✗ failure`，卡片标题显示 `Task failure`。详细设计见 [tasks.md](tasks.md)。

## 设置持久化

- `internal/settings` 读写 `PathUserData/settings.json`；`ui.Options.DataDir` 为空时完全不落盘（测试、便携运行）。
- 保存内容：最后使用的 agent、工作区、侧栏工作区列表（`workspaces`）、软删除记录（`removedWorkspaces`）、折叠状态（`collapsedWorkspaces`）、MCP 默认列表、每个 agent 的模型/thinking/mode（`lastSelections`）、系统通知开关（`notifications`，默认 true）、Dock 角标开关（`dockBadge`，默认 true），以及 `paths` 里的显式工具覆盖。
- 保存时机：切换会话、选择 agent/目录/model/thinking/mode、添加/移除/恢复工作区、折叠/展开工作区、切换系统通知或 Dock 角标、Connect 校验成功、MCP Apply、退出。
- 启动恢复：工作区、侧栏工作区列表（含软删除分组）、折叠状态、MCP 列表、上次 agent，以及该 agent 的模型/thinking/mode 草稿选择；显式传入的 `ui.Options.Workspace` 优先（测试用）。上次工作区已被软删除时，草稿改用第一个可见工作区（没有则用 home）。启动时创建的草稿不会展开已折叠的工作区。
- `paths` 只保存配置里的覆盖值；`ACP_GUI_*` 环境变量优先，且不会被写回文件。
- 容错：文件缺失用默认值；损坏 JSON、MCP 不是数组、相对路径、未来版本只显示侧栏/标题栏警告，其余字段照常生效。
- 不保存会话布局、草稿文本、附件和消息；会话索引、用户消息发送时间和每轮运行时间在单独的 `sessions.json` 里（见“已保存会话与本地存档”）。

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

## 消息跳转

- composer 底栏左下角（附件 `＋` 右侧）的 `Jump` 打开“Jump to a message”弹窗；当前会话还没有用户发言时按钮禁用。
- 列表直接读 `Chat.Messages` 里的 `RoleUser` 行，包括 `session/load` 重放的发言和正在回答的 prompt；助手、工具、系统提示和错误不进列表。
- 按时间正序显示（旧的在上、新的在下），序号按用户自己的发言轮次从 1 递增；每条显示单行预览（超过 180 个字符按 rune 截断）和发送时间，只有图片的发言显示图片数量。发送时间在今天就只显示 `15:04`，同年显示 `Jan 2 15:04`，跨年显示 `Jan 2, 2006 15:04`；没有 agent 时间或本地记录的历史消息显示 `—`（tooltip 说明来源），不猜测时间。点条目或打开弹窗不会重发 prompt、不改变会话。
- 列表区域最大高度 380 DIP，并按窗口高度收缩；超出时在弹窗内滚动。弹窗打开时先滚到最新一条（底部），再向上翻看旧发言。
- 点击条目关闭弹窗并调用 `chat.list.ScrollTo(row, Start)`，把该发言滚到消息区顶部；只影响当前会话的滚动位置。

## 滚动到底部

- 消息列表用 `ListState.FollowEnd` + `Smooth`：在底部时跟住新消息和正在变长的回答；滚离底部后停止跟随，不会被新输出拽回去。
- 不在底部时，消息区底部中央浮出 `↓ Latest` 按钮（可访问名为 `Jump to latest`，边框 + 阴影），读 `chat.list.AtEnd()`；MyGo 在滚动改变该状态时自动重绘一帧，所以按钮随滚动出现/消失。
- 点击调用 `chat.list.ScrollToEnd()`，列表回到底部并重新跟随。按钮绝对定位在消息列表外层的 `Box` 上（`LeftPercent(50)` + `TranslatePercent(-50, 0)`），不参与列布局，显示或隐藏都不会改变滚动位置。

## 实时运行时间

- 回答进行中，会话流右下角浮出不可点击的 `Working 1m 23s` 胶囊（`IgnoreInput`），样式与 `↓ Latest` 一致：Surface 背景、1px 边框、阴影，绝对定位 `Right(14).Bottom(14)`，不参与列布局。
- 计时读取 `chat.turn.startedAt` 和当前帧的 `Context.Now()`；每个活动帧用 `Context.After(100 * time.Millisecond)` 请求下一帧，所以 10 秒内按 0.1 秒刷新，之后按秒刷新。
- prompt 结果、取消、错误或 agent 退出后 `chat.Streaming`/`turn.startedAt` 清除，胶囊立即消失；持久化的 `Took …` 行仍按“已保存会话与本地存档”写入会话流。

## 计划、标题与用量

- `plan` → composer 上方的可折叠计划列表，显示状态。
- `session_info_update.title` → 标题显示在顶部会话栏和侧栏会话项。收到后立即写 `sessions.json`；pi-acp fork 在首次 prompt 用用户正文合成一条兜底标题，command-code 在首轮后生成标题，用户手动 rename 写入 `showName` 并始终优先。
- `usage_update` → composer 中的环形进度 + 百分比（有用量但四舍五入为 0 时显示 `<1%`）；hover 显示 used/size、百分比和费用。agent 不发时没有上下文数字；ACP 没有主动查询的方法。
- prompt 结果里的 `usage` 解析为 `acp.PromptUsage`，显示为 `turn 1.2k in · 100 out · 40 thought`（只在 agent 返回时出现），与 `context` 分开；`_meta` 保留在 `acp.PromptResult`，不展示明细。
- 当前 command-code 和 vendored pi-acp fork（0.4.0 + usage 补丁）都会发 `usage_update` 并返回 prompt `usage`；上游 npm `@geohar/pi-acp@0.4.0` 没有这两处映射（见 [pi.md](pi.md#用量)）。

## 事件顺序与生命周期

- `internal/session.Start` 支持 `DeferUpdates`：`session/update` 先进缓冲区，UI 绑定 Session 后 `Activate` 再按顺序交付。
- 每个会话各自调用一次 `session.Start`；历史重放、退出和权限都以 `*session.Session` 指针归属到对应会话。
- `Info` 在 mutex 下更新；`config_option_update`、`current_mode_update`、`session_info_update` 同时写入 Info 和 UI。
- `Session.Close` 先尽力 `session/close`（1 秒），再关 stdin、SIGTERM/SIGKILL 进程组。
- `Session.ListSessions` / `Probe` 复用同一进程生命周期，不引入第二套 spawn 逻辑。

## 测试

```bash
go test ./... -race
go test ./internal/ui -run 'TestSessionConfig|TestSaved|TestDeleteSaved|TestDeleteOpen|TestCloseChat|TestSendImage|TestPasteImage|TestPasteText|TestImagePreview|Test.*Mention|TestMCP|TestPromptUsageFallback|TestMultipleChats|TestBackgroundChat|TestPermissionFrom|TestCloseTab|TestFirstMessage|TestWorkspaceConversation|TestRename|TestGit|TestGitPanel|TestComposerHeight|TestRemoveWorkspace|TestRemovedWorkspace|TestAgentIcon|TestAgentBadges|TestDockBadge|TestBackgroundTurn|TestActiveChatIsUnread' -v
go test ./internal/archive -v
go test ./internal/git -v
go test ./internal/settings -v
go test ./internal/logs -v
go test ./internal/ui -run 'TestSettings|TestShutdownPersists|TestCorruptSettings|TestExplicitWorkspace|TestDiagnostics' -v
ACP_REAL_COMMAND_CODE=1 go test ./internal/session -run '^TestRealCommandCode$' -v -count=1
```

`scripts/fake-agent.mjs` 支持 list/load/close、set_mode、set_config_option、图片计数、MCP 计数、命令和计划事件。
