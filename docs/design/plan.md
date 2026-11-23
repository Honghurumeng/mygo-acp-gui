# 测试与计划

## 测试

### 单元 / 集成

- `internal/acp`：行分帧（多 chunk、CRLF、非 JSON、U+2028 内容）、请求/响应匹配、权限回执
- `internal/agent`：内置表、launch spec 解析、路径解析、登录 shell/交互 shell/npm 回退、版本解析
- `internal/session`：probe 正常/非 JSON stdout/立即退出/stderr tail/上下文超时、`session/new` 认证失败、进程组清理
- `internal/settings`：缺失/损坏/未来版本、相对路径、原子写入
- `internal/logs`：追加、1 MiB 滚动与备份、超大 chunk、并发写入
- `internal/files`：排序、跳过 `.git`、新建/重命名/移动/删除、重名、非法名字、越界路径、符号链接
- `internal/ui`：启动发现状态机、侧栏状态、draft 补选 agent、权限允许/拒绝/取消、异常退出、Git 远程操作、任务进程隔离/串行、附件数量与大小限制

探测失败 fixture 直接写在 `internal/session/probe_errors_test.go`，用 POSIX shell 模拟；不再依赖不存在的 `testdata/`。
复用 `scripts/fake-agent.mjs` 作为假 agent，跑 UI/客户端集成测试。真正的 CLI 探针用 `ACP_REAL_*` 环境变量显式开启。

`scripts/fake-agent.mjs` 目前支持：

- 默认按块流式输出 `agent_message_chunk`；
- `/slow` 放慢输出，用于测 Stop / `session/cancel`；
- `/permission` 先发 `tool_call`，再发 `session/request_permission`；
- `/thought` 先发 `agent_thought_chunk`；
- `TASK_EXECUTION` 返回任务执行总结，`TASK_COMPLETION_CHECK` 返回 `{complete, reason, summary}` 供任务 runner 测试；`/refuse` 让 executor 以 refusal 结束，`/writefile <path>` 让 executor 在 workspace 写一个文件，用于验证失败回滚；
- `FAKE_TASK_CHECK_STATE` + 任务文本里的 `/eventually` 让连续 checker 第一次 incomplete、第二次 complete；`/alwaysincomplete` 让 checker 一直拒绝，用于测 8 轮失败；
- `FAKE_AGENT_CHILD_PID_FILE` 启动一个子进程，验证关闭时整个进程组都被清理；
- `/exit` 在流式输出中途退出，`/permission-exit` 在权限请求挂起时退出，用于测 `applyExit`、stderr 和权限取消；
- `FAKE_AGENT_SESSION_LOG` 记录进程、会话、`session/new` cwd 以及 prompt start/finish，用于断言 executor 跨轮复用会话、checker 每轮独立进程、任务串行；
- `FAKE_AGENT_NO_SESSION` 让 `session/new` 返回 `-32000`，配合 initialize 的 `authMethods` 测 `needs-auth`。

对应测试：

```bash
go test ./... -race
go test ./internal/session -run 'TestFakeAgent|TestCloseKillsProcessGroup|TestProbe|TestStartReportsAuth|TestVendored' -v
go test ./internal/ui -run 'TestDiscovery|TestChatWithFakeAgent|TestMultipleChats|TestBackgroundChat|TestPermission|TestCloseTab|TestSavedSessionOpensNewTab|TestGitPanel|TestFilePanel|TestTask|TestAttachment' -v
```

pi-acp bundle 冒烟（只 `initialize`，不 spawn pi、不碰凭据）：

```bash
node scripts/pi-acp-smoke.mjs
```

codex-acp bundle 冒烟（只 `initialize`，需要系统 `codex`，不创建会话、不调模型）：

```bash
node scripts/codex-acp-smoke.mjs
```

真实 agent 冒烟（会创建会话并调用模型，opt-in）：

```bash
ACP_REAL_PI=1 go test ./internal/session -run TestRealPi -v
ACP_REAL_COMMAND_CODE=1 go test ./internal/session -run '^TestRealCommandCode$' -v -count=1
ACP_REAL_CODEX=1 go test ./internal/session -run TestRealCodex -v -count=1
```

### UI

按 MyGo 约定用 `ui.NewTester`：

- 只测真实会变的行为：点击 agent 切换状态、断开、发送、停止、权限选择、配置切换
- 不测标签文案、默认路径排序
- 新增 UI 测试跑 `-count=5`，避免 flaky

### 真实 agent（可选、本机）

当前已实现真实 pi / command-code / codex 的 opt-in 冒烟：

```bash
ACP_REAL_PI=1 go test ./internal/session -run TestRealPi -v
ACP_REAL_COMMAND_CODE=1 go test ./internal/session -run '^TestRealCommandCode$' -v -count=1
ACP_REAL_COMMAND_CODE=1 go test ./internal/session -run '^TestRealCommandCodeAuthRequired$' -v -count=1
ACP_REAL_CODEX=1 go test ./internal/session -run TestRealCodex -v -count=1
```

pi 测试走 `initialize` + `session/new` + 一轮 `session/prompt`，并断言 `usage_update` 和 prompt `usage`；command-code 测试额外覆盖写文件权限、`session/cancel`、取消后继续对话和退出清理，另一个测试用临时 `HOME` 覆盖未登录的 `-32000` / `authMethods`；codex 测试走 vendored bundle + 系统 `codex app-server`，断言 session/new、model config 和一轮回复。调用模型的测试默认跳过。发现阶段的只读 `initialize` 探测已由 `internal/session.Probe` 实现，UI 启动时逐 agent 并行执行。

Command Code 的协议细节见 [command-code.md](command-code.md)，Codex 见 [codex.md](codex.md)；UI 能力见 [features.md](features.md)。

Feature tests:

```bash
go test ./internal/ui -run 'TestSessionConfig|TestSaved|TestCloseChat|TestSendImage|TestPromptUsageFallback|TestGitPanel' -v
go test ./internal/git -v
go test ./internal/tasks -v
go test ./internal/settings -v
go test ./internal/logs -v
go test ./internal/ui -run 'TestSettings|TestShutdownPersists|TestCorruptSettings|TestExplicitWorkspace|TestSessionWritesAgentLog' -v
```


### 手动验收

1. 从 Finder 启动 `.app`（不是终端）——验证 PATH 解析。
2. 本机已装 `command-code`：应出现在 Agent 列表，能 Connect、流式回复、审批权限、取消。
3. 本机未装 `command-code` 或 `codex`：不应阻塞已安装的 agent，也不应出现空列表崩溃。
4. 本机未登录 Command Code：Connect 显示 `authMethods` + `command-code login`，应用不代登录。
5. 用 fixture 模拟同名非 ACP 命令：Connect 失败并显示 stderr，该 agent 状态显示 `installed-not-acp`。
6. 界面只列出 pi、command-code、codex，不出现 registry 里的其他 agent。
7. pi：离线（断网）启动，仍能连接（vendored pi-acp），且能聊天。
8. 权限弹窗、取消、关闭窗口：无残留 `pi` / `command-code` / `codex` / agent 进程。
9. 退出再启动：上次 agent / 工作区恢复。
10. pi 未登录：只显示 `authMethods` / 登录命令，应用不尝试登录、不打开内嵌终端。
10b. codex：已装 Codex CLI 时应出现在列表；`codex login` 后能 Connect、流式回复、取消；未登录时只显示 `authMethods` + `codex login`，不代登录。向 Codex 请求一个可并行的任务时，child session 状态、任务、工具和回复应出现在父聊天，子权限弹窗标注 Subagent。
10c. codex 自定义供应商：设置里填 Base URL、key 和模型，勾选推理档位，保存后新会话的模型面板出现这些模型且 Thinking 列出现勾选的档位；切换模型/档位后 `session/prompt` 使用新值；标题生成使用设置里的标题模型（空则跟随当前模型）；关闭供应商后 provider 路由回到 Codex 自己的配置，但模型/推理档位/标题覆盖仍生效；清空模型列表后模型目录才回到 Codex 自己的目录；非法 URL/模型 id 或一个档位都没勾选时不保存并显示错误。
11. 多会话：在侧栏开两个会话分别发 `/slow` 和普通消息，切换会话时后台继续流式；后台会话请求权限时弹窗标明会话，回答不影响另一个会话。
12. 已保存会话：重启后在对应 workspace 下直接从 `sessions.json` 看到历史会话，不等 agent；点击按存档的 agent + sessionId 用 `session/load` 重放历史。重命名后重启仍显示 `showName`；删除后自动同步/重启都不再出现，可从 “Show deleted” 恢复。
12b. 工作区软删除：工作区 `⋯` → `Remove workspace` 后分组隐藏但存档/会话不变，重启仍不出现；“Show removed (N)” → `Restore workspace` 恢复。
13. 设置持久化：改 agent/工作区/model/thinking/mode 后退出，再启动应恢复（模型面板与草稿都停在原选择）；手改 `lastSelections` 成已下架的 model 或 mode 后重启，Connect 应跳过并保持 agent 默认，不报错；手工损坏 `settings.json` 或写入相对路径，应正常启动并在工具栏显示警告。
14. 日志：连接后 `logs/<agent>.log` 有连接头和 stderr，超过 1 MiB 后生成 `.1` 备份（当前没有查看面板）。
15. Git：打开非仓库 workspace，右侧提示 “Not a Git repository” 并可 `git init`；PATH 中无 git 时提示安装；仓库内可 stage/commit、查看提交图、切换 workspace 后不串仓库；提交行右键创建分支不切换，Git 标题行和 composer 草稿可切换分支，脏工作区冲突时 `Stash & switch` 暂存后切换并恢复，新分支首次 Push 设置 upstream、Pull 匹配同名远端分支。
16. Tasks：创建两个任务并 Run pending；每个任务应新建会话，checker 先拒绝再通过时同一 executor 会话继续，第二次 checker 应能看到第一轮总结和反馈；完成后 `tasks/` 有 Markdown、每个任务各一次 commit、没有 push；失败任务应回滚 executor 文件、保留用户已有改动、写 failure 总结且不 commit；Stop 后任务回 pending，重启后仍未完成的任务仍可编辑。

### 常用命令

```bash
go test ./...
go tool mygo vet
go tool mygo dev
go tool mygo build
go tool mygo build -skip-dmg   # 本地快速打包
node scripts/pi-acp-smoke.mjs        # vendored pi-acp 冒烟
node scripts/codex-acp-smoke.mjs     # vendored codex-acp 冒烟
scripts/vendor-pi-acp.sh             # 仅在升级 pi-acp 时运行
scripts/vendor-codex-acp-local.sh    # 重打本地补丁 codex-acp（日常）
scripts/vendor-codex-acp.sh          # 官方 codex-acp，不含补丁
```

## MVP

- [x] 内置 agent（pi、command-code、codex）路径/版本解析 + 可选列表 + 单 agent 失败不阻塞另一个
- [x] 内置 agent 的 `initialize` 探测（5 秒，不缓存）+ `ready` / `not-installed` / `installed-not-acp` / `needs-auth` 状态展示
- [x] 发现后异步配置探测（临时 `session/new` 读 `configOptions` / `modes`），草稿即可选 provider / model / thinking / mode，Connect 后自动补齐；provider 列表以 agent 当前返回的 model 选项为准，面板打开时节流刷新
- [x] agent 选择 + 工作区选择 + 绝对路径/可写校验（上次使用由 settings.json 恢复）
- [x] ACP 会话：initialize、session/new、session/prompt、agent_message_chunk 流式 Markdown、思考、工具调用、权限、取消
- [x] 计划面板、配置选择、已保存会话（list/load/close）
- [x] 模型/effort/模式配置 + `session/set_config_option` / `set_mode`；model/thinking/mode 按 agent 持久化（`lastSelections`），新建草稿恢复、Connect 校验、切换 agent 恢复各自的选择
- [x] 图片附件（文件选择 + 剪贴板粘贴）、输入 `/` 触发的命令面板、`@` 文件引用（只发路径文本）
- [x] 侧栏按 workspace 分组、已保存会话直接展示、折叠/展开、超过 5 条收起、重命名/删除/软删除恢复
- [x] 会话行与标题栏的 agent 品牌图标（pi、Command Code、Codex，SVG 内嵌并跟随主题）
- [x] 本地 `sessions.json` 会话存档：agent/session 关联、启动直接展示、show 软删除、showName 重命名、最近上下文用量、点击 load 重放历史
- [x] composer：可拖动高度、附件/git 分支切换/mode/上下文环/模型面板/thinking/发送；首条消息自动连接
- [x] 右侧 Git 面板：可折叠、无 git/无仓库提示、status、紧凑可展开提交图、逐文件 diff 预览、stage/commit/fetch/pull/push/undo soft reset、本地分支读取/切换/从提交新建，新分支首次 push 设置 upstream、pull 匹配同名远端分支
- [x] 右侧 Tasks 面板：任务创建/排序/删除、pending/ongoing/complete + pass/failure、串行 executor + 隔离 checker（多轮上下文）、成功 Markdown 总结 + 每任务一次 commit（不 push）、失败回滚 + 无 commit
- [x] pi-acp 单文件 bundle vendored（`resources/agents/pi-acp/`）
- [x] codex-acp 官方单文件 bundle vendored（`resources/agents/codex-acp/`）+ 系统 Codex CLI（`CODEX_PATH`）
- [x] 系统 Node / pi / command-code / codex 定位（登录 shell + 绝对路径覆盖）+ 登录指引（只展示，不代登录）
- [x] Command Code 启动与完整聊天流程：initialize、session/new、流式、权限、取消、退出清理
- [x] Codex 启动与完整聊天流程：本地补丁 codex-acp bundle + 系统 codex、initialize、session/new、流式、取消、ACP v1 draft 子代理、退出清理
- [x] 退出清理（Setpgid + 进程组 SIGTERM/SIGKILL）
- [x] settings.json 持久化（上次 agent/工作区、每个 agent 的模型/thinking/mode、侧栏工作区列表 + `removedWorkspaces` 软删除、paths 覆盖、外观/主题；原子写入 + 容错加载）
- [ ] 诊断面板（stderr / 日志查看；目前只有 `logs/<agent>.log` 文件写入）
- [x] 基础测试：framing、launch spec、登录 shell 解析、fake agent 会话、UI 发送/流式/停止/权限、进程组清理
- [x] 发现测试：三种内置 agent 的 ready/not-installed/installed-not-acp/error/needs-auth 状态、缺 agent 不阻塞、draft 补选、保存 agent 回退、刷新探测
- [x] 失败路径测试：probe 非 JSON/立即退出/stderr/超时、认证 `-32000`、agent 中途退出、权限挂起时退出、权限 Reject/Allow always
- [x] Git 远程测试：临时 bare remote 下的 Fetch/Pull/Push、UnstageAll、新分支首次 Push 设置 upstream、UI Fetch/Pull/Push/Undo/Unstage all；分支读取/创建/切换与右键创建分支、composer 草稿切换
- [x] Task 断言：成功任务文件和 commit message、executor 跨轮同 session、checker 每轮独立进程/cwd、prompt 不重叠（串行）
- [x] 新能力测试：probe、config/mode、list/load/close、侧栏已保存会话 UI、图片 prompt、命令面板、图片发送 UI、附件数量/大小/删除、设置持久化、agent 日志写入
- [x] 存档测试：文件读写/容错、重命名与软删除跨重启、删除后同步不复活、按记录的 agent load、工作区软删除/恢复跨重启
- [x] 主题系统：System/Light/Dark 外观 + Default/Nord/Solarized/Catppuccin/Claude/Green/Green Soft/VS Code/Everforest/One Dark/Tokyo Night 配色，设置面板切换、跨重启恢复、原生窗口外观同步（见 [themes.md](themes.md)）

## 后续

- [x] `session/load` / `close` 已保存会话列表（直接显示在侧栏 workspace 下）
- [x] 本地会话存档与软删除/重命名（启动不依赖 `session/list`，点击 load 重放历史）
- [x] 图片输入（文件选择 + 剪贴板粘贴）
- [x] `@` 文件引用面板（只发 `@路径` 文本，agent 自行读文件）
- [x] 多会话并行（侧栏每个会话一个 agent 进程；后台流式、权限按会话路由、重命名/删除/切换）
- [x] settings.json 持久化（上次 agent/工作区）
- [x] 移除 CrychicUI：通用外观改到 `internal/ui/widgets.go` / `components.go`，图标内嵌 Lucide 子集，release 二进制约减 1.3 MB（见 [components.md](components.md)）
- [ ] MCP servers 配置 UI（JSON 编辑 + 校验 + 持久化；当前协议层只发送空 `mcpServers` 数组）
- [ ] ACP 合规收口（详见 [acp-conformance.md](acp-conformance.md)）：明确 `subagents` 默认策略（严格稳定 v1 关闭 / 完整 v1 draft 支持），删除不存在的 `SessionCapabilities.Subagents`
- [x] 工具卡片按 ACP `kind` 分类型渲染（edit/write 内联 diff、execute 等宽输出 + exit、read/search 代码块），见 [toolcards.md](toolcards.md)
- [x] Markdown 里的 Mermaid 图表：对话与查看卡片静态 SVG（flow/sequence 用 streameraid，其他用 go-mermaid）、主题配色、缓存与失败回退，见 [mermaid.md](mermaid.md)
- [ ] 补齐 draft 子代理语义：`session_message_chunk`、`session_message` upsert、`subagent_update` 的 `null` 清除、idle 可复用
- [ ] 给 `session/load` / `session/list` / `session/close` 加 capability 预检，不支持时静默降级
- [ ] 用稳定 `ContentChunk.messageId` 做 replay 行身份；`_meta.piAcp.replay` 仅作 fallback
- [ ] 支持 grouped/boolean `configOptions`（或逐项跳过坏选项）和 `session_info_update` 的 `null` 清除
- [ ] ACP 报文监视器
- [ ] Windows / Linux 打包验证

## 明确不做

- 不实现 ACP agent 端（pi-acp / codex-acp 已经做了）
- 不重写 pi（pi 仍然是 Node 程序）
- 不做 Mac App Store 沙箱版本
- MVP 不自动安装任意 agent
- 不做 registry 全量发现/在线目录；只支持内置表（当前 pi、command-code、codex）
- 不声明 `fs` / `terminal` 能力
- 不捆绑、不安装、不升级 Node；默认用户电脑上已有 Node 22+
- 不实现登录流程、不内嵌终端；模型凭据/可用性由用户自理
- 不做用户 JSON 主题、在线主题库或逐主题代码高亮（当前 token 色仍由 GitHub 浅/深色板提供）

## 待定问题

1. Windows 的进程树清理与 PATH 解析优先级。

## 参考

- ACP 协议：https://agentclientprotocol.com；v1 schema stable/unstable：https://github.com/agentclientprotocol/agent-client-protocol/tree/main/schema/v1
- `@geohar/pi-acp`：npm 包（MIT）；fork `https://github.com/georgeharker/pi-acp`，原始项目 `https://github.com/svkozak/pi-acp`。
- `@agentclientprotocol/codex-acp`：官方 ACP 适配器（Apache-2.0），`https://github.com/agentclientprotocol/codex-acp`；Codex CLI：`https://github.com/openai/codex`。
- pi RPC 文档（本机安装包内）：`docs/rpc.md`、`docs/rpc-commands.md`、`docs/json.md`、`docs/rpc-extension-ui.md`
- MyGo 文档：`github.com/egoist/mygo/docs`，重点是 `docs/native.md`、`docs/ui/markdown.md`、`docs/ui/list.md`、`docs/ui/views.md`、`docs/distribution.md`
- MyGo 示例：`examples/markdown-chat`
- 早期 Node 参考实现（已移除，见 git 历史）；假 agent fixture：`scripts/fake-agent.mjs`
