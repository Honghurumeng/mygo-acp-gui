# 测试与计划

## 测试

### 单元 / 集成

- `internal/acp`：行分帧（多 chunk、CRLF、非 JSON、U+2028 内容）、请求/响应匹配、权限回执
- `internal/agent`：内置表、launch spec 解析、路径解析、探测超时与进程组清理
- `internal/settings`：缺失/损坏/未来版本、坏 MCP、相对路径、原子写入
- `internal/logs`：追加、1 MiB 滚动与备份、超大 chunk、并发写入
- 探测 fixture：正常 agent、慢 agent、只打印 stderr 的 agent、立即退出的 agent、stdout 混入日志的 agent

复用 `scripts/fake-agent.mjs` 作为假 agent，跑 UI/客户端集成测试；fixture 放在 `testdata/`。

`scripts/fake-agent.mjs` 目前支持：

- 默认按块流式输出 `agent_message_chunk`；
- `/slow` 放慢输出，用于测 Stop / `session/cancel`；
- `/permission` 先发 `tool_call`，再发 `session/request_permission`；
- `/thought` 先发 `agent_thought_chunk`；
- `FAKE_AGENT_CHILD_PID_FILE` 启动一个子进程，验证关闭时整个进程组都被清理。

对应测试：

```bash
go test ./... -race
go test ./internal/session -run 'TestFakeAgent|TestCloseKillsProcessGroup' -v
go test ./internal/ui -run 'TestChatWithFakeAgent|TestMultipleChats|TestBackgroundChat|TestPermissionFrom|TestCloseTab|TestSavedSessionOpensNewTab|TestGitPanel' -v
```

pi-acp bundle 冒烟（只 `initialize`，不 spawn pi、不碰凭据）：

```bash
node scripts/pi-acp-smoke.mjs
```

真实 agent 冒烟（会创建会话并调用模型，opt-in）：

```bash
ACP_REAL_PI=1 go test ./internal/session -run TestRealPi -v
ACP_REAL_COMMAND_CODE=1 go test ./internal/session -run '^TestRealCommandCode$' -v -count=1
```

### UI

按 MyGo 约定用 `ui.NewTester`：

- 只测真实会变的行为：点击 agent 切换状态、断开、发送、停止、权限选择、配置切换
- 不测标签文案、默认路径排序
- 新增 UI 测试跑 `-count=5`，避免 flaky

### 真实 agent（可选、本机）

当前已实现真实 pi / command-code 的 opt-in 冒烟：

```bash
ACP_REAL_PI=1 go test ./internal/session -run TestRealPi -v
ACP_REAL_COMMAND_CODE=1 go test ./internal/session -run '^TestRealCommandCode$' -v -count=1
ACP_REAL_COMMAND_CODE=1 go test ./internal/session -run '^TestRealCommandCodeAuthRequired$' -v -count=1
```

pi 测试走 `initialize` + `session/new` + 一轮 `session/prompt`，并断言 `usage_update` 和 prompt `usage`；command-code 测试额外覆盖写文件权限、`session/cancel`、取消后继续对话和退出清理，另一个测试用临时 `HOME` 覆盖未登录的 `-32000` / `authMethods`。调用模型的测试默认跳过。发现阶段的只读 `initialize` 探测已由 `internal/session.Probe` 实现，UI 启动时逐 agent 并行执行。

Command Code 的协议细节见 [command-code.md](command-code.md)；UI 能力见 [features.md](features.md)。

Feature tests:

```bash
go test ./internal/ui -run 'TestSessionConfig|TestSaved|TestCloseChat|TestSendImage|TestMCP|TestPromptUsageFallback|TestGitPanel' -v
go test ./internal/git -v
go test ./internal/settings -v
go test ./internal/logs -v
go test ./internal/ui -run 'TestSettings|TestShutdownPersists|TestCorruptSettings|TestExplicitWorkspace|TestDiagnostics' -v
```


### 手动验收

1. 从 Finder 启动 `.app`（不是终端）——验证 PATH 解析。
2. 本机已装 `command-code`：应出现在 Agent 列表，能 Connect、流式回复、审批权限、取消。
3. 本机未装 `command-code`：不应阻塞 pi，也不应出现空列表崩溃。
4. 本机未登录 Command Code：Connect 显示 `authMethods` + `command-code login`，应用不代登录。
5. 用 fixture 模拟同名非 ACP 命令：Connect 失败并显示 stderr，该 agent 状态显示 `installed-not-acp`。
6. 界面只列出 pi、command-code，不出现 registry 里的其他 agent。
7. pi：离线（断网）启动，仍能连接（vendored pi-acp），且能聊天。
8. 权限弹窗、取消、关闭窗口：无残留 `pi` / `command-code` / agent 进程。
9. 退出再启动：上次 agent / 工作区恢复。
10. pi 未登录：只显示 `authMethods` / 登录命令，应用不尝试登录、不打开内嵌终端。
11. 多会话：在侧栏开两个会话分别发 `/slow` 和普通消息，切换会话时后台继续流式；后台会话请求权限时弹窗标明会话，回答不影响另一个会话。
12. 已保存会话：重启后在对应 workspace 下直接从 `sessions.json` 看到历史会话，不等 agent；点击按存档的 agent + sessionId 用 `session/load` 重放历史。重命名后重启仍显示 `showName`；删除后自动同步/重启都不再出现，可从 “Show deleted” 恢复。
12b. 工作区软删除：工作区 `⋯` → `Remove workspace` 后分组隐藏但存档/会话不变，重启仍不出现；“Show removed (N)” → `Restore workspace` 恢复。
13. 设置持久化：改 agent/工作区/MCP 后退出，再启动应恢复；手工损坏 `settings.json` 或写入相对路径，应正常启动并在工具栏显示警告。
14. 诊断：点顶部 “Logs” 能看到当前会话的 stderr；`logs/<agent>.log` 有连接头和 stderr，超过 1 MiB 后生成 `.1` 备份。
15. Git：打开非仓库 workspace，右侧提示 “Not a Git repository” 并可 `git init`；PATH 中无 git 时提示安装；仓库内可 stage/commit、查看提交图、切换 workspace 后不串仓库。

### 常用命令

```bash
go test ./...
go tool mygo vet
go tool mygo dev
go tool mygo build
go tool mygo build -skip-dmg   # 本地快速打包
node scripts/pi-acp-smoke.mjs        # vendored pi-acp 冒烟
scripts/vendor-pi-acp.sh             # 仅在升级 pi-acp 时运行
```

## MVP

- [x] 内置 agent（pi、command-code）路径/版本解析 + 可选列表 + 单 agent 失败不阻塞另一个
- [x] 内置 agent 的 `initialize` 探测（5 秒，不缓存）+ `ready` / `not-installed` / `installed-not-acp` / `needs-auth` 状态展示
- [x] 发现后异步配置探测（临时 `session/new` 读 `configOptions`），草稿即可选 provider / model / thinking，Connect 后自动补齐；provider 列表以 agent 当前返回的 model 选项为准，面板打开时节流刷新
- [x] agent 选择 + 工作区选择 + 绝对路径/可写校验（上次使用由 settings.json 恢复）
- [x] ACP 会话：initialize、session/new、session/prompt、agent_message_chunk 流式 Markdown、思考、工具调用、权限、取消
- [x] 计划面板、配置选择、已保存会话（list/load/close）
- [x] 模型/effort/模式配置 + `session/set_config_option` / `set_mode`
- [x] 图片附件（文件选择 + 剪贴板粘贴）、MCP servers JSON、输入 `/` 触发的命令面板、`@` 文件引用（只发路径文本）
- [x] 侧栏按 workspace 分组、已保存会话直接展示、折叠/展开、超过 5 条收起、重命名/删除/软删除恢复
- [x] 会话行与标题栏的 agent 品牌图标（pi、Command Code，SVG 内嵌并跟随主题）
- [x] 本地 `sessions.json` 会话存档：agent/session 关联、启动直接展示、show 软删除、showName 重命名、点击 load 重放历史
- [x] composer：可拖动高度、附件/git/mode/上下文环/模型面板/thinking/发送；首条消息自动连接
- [x] 右侧 Git 面板：可折叠、无 git/无仓库提示、status、紧凑可展开提交图、逐文件 diff 预览、stage/commit/fetch/pull/push/undo soft reset
- [x] pi-acp 单文件 bundle vendored（`resources/agents/pi-acp/`）
- [x] 系统 Node / pi / command-code 定位（登录 shell + 绝对路径覆盖）+ 登录指引（只展示，不代登录）
- [x] Command Code 启动与完整聊天流程：initialize、session/new、流式、权限、取消、退出清理
- [x] 退出清理（Setpgid + 进程组 SIGTERM/SIGKILL）
- [x] settings.json 持久化（上次 agent/工作区、侧栏工作区列表 + `removedWorkspaces` 软删除、MCP 默认值、paths 覆盖；原子写入 + 容错加载）
- [x] 错误诊断面板、stderr 日志滚动（Diagnostics 模态 + `logs/<agent>.log`，1 MiB 滚动）
- [x] 基础测试：framing、launch spec、登录 shell 解析、fake agent 会话、UI 发送/流式/停止/权限、进程组清理
- [x] 新能力测试：probe、config/mode、list/load/close、侧栏已保存会话 UI、图片 prompt、MCP 校验、命令面板、图片发送 UI、设置持久化、诊断面板与日志滚动
- [x] 存档测试：文件读写/容错、重命名与软删除跨重启、删除后同步不复活、按记录的 agent load、工作区软删除/恢复跨重启

## 后续

- [x] `session/load` / `close` 已保存会话列表（直接显示在侧栏 workspace 下）
- [x] 本地会话存档与软删除/重命名（启动不依赖 `session/list`，点击 load 重放历史）
- [x] 图片输入（文件选择 + 剪贴板粘贴）
- [x] `@` 文件引用面板（只发 `@路径` 文本，agent 自行读文件）
- [x] 多会话并行（侧栏每个会话一个 agent 进程；后台流式、权限按会话路由、重命名/删除/切换）
- [x] settings.json 持久化（MCP、上次 agent/工作区）
- [ ] MCP 行编辑器（当前是校验过的 JSON）
- [ ] ACP 报文监视器
- [ ] Windows / Linux 打包验证

## 明确不做

- 不实现 ACP agent 端（pi-acp 已经做了）
- 不重写 pi（pi 仍然是 Node 程序）
- 不做 Mac App Store 沙箱版本
- MVP 不自动安装任意 agent
- 不做 registry 全量发现/在线目录；只支持内置表（当前 pi、command-code）
- 不声明 `fs` / `terminal` 能力
- 不捆绑、不安装、不升级 Node；默认用户电脑上已有 Node 22+
- 不实现登录流程、不内嵌终端；模型凭据/可用性由用户自理

## 待定问题

1. Windows 的进程树清理与 PATH 解析优先级。

## 参考

- ACP 协议：https://agentclientprotocol.com
- `@geohar/pi-acp`：npm 包（MIT）；fork `https://github.com/georgeharker/pi-acp`，原始项目 `https://github.com/svkozak/pi-acp`。
- pi RPC 文档（本机安装包内）：`docs/rpc.md`、`docs/rpc-commands.md`、`docs/json.md`、`docs/rpc-extension-ui.md`
- MyGo 文档：`github.com/egoist/mygo/docs`，重点是 `docs/native.md`、`docs/ui/markdown.md`、`docs/ui/list.md`、`docs/ui/views.md`、`docs/distribution.md`
- MyGo 示例：`examples/markdown-chat`
- 早期 Node 参考实现（已移除，见 git 历史）；假 agent fixture：`scripts/fake-agent.mjs`
