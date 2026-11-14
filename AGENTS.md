# AGENTS.md

## 目标
用 Go + MyGo 做原生 ACP GUI：检测内置的 pi / command-code / codex 是否可用，选择 agent 和工作区后聊天。
当前实现是仓库根目录的 Go + MyGo 工程。

## 文档规则
- 本文件最多 40 行，只放目标、铁律和文档索引。
- 其余设计必须写进 `docs/design/` 下的单独文档；只要会超过 40 行，绝对改用单独文档记录。
- 新增或修改设计后，同步更新下面的索引和对应文档。

## 设计文档
- `docs/design/architecture.md`：整体架构、Go 包划分、ACP 客户端、MyGo UI、数据与安全。
- `docs/design/discovery.md`：内置 agent 表（pi、command-code、codex）、launch spec、路径解析、initialize 探测、状态机、手动测试。
- `docs/design/pi.md`：pi-acp vendor、SDK/zod 依赖、Node/pi 版本、PATH、认证、更新。
- `docs/design/pi-fork.md`：fork pi-acp 的最小改动、vendor 接入与验证。
- `docs/design/command-code.md`：Command Code 启动、已验证 ACP 流程、权限/取消、测试。
- `docs/design/codex.md`：官方 codex-acp bundle、系统 Codex CLI/`CODEX_PATH`、认证、更新与测试。
- `docs/design/features.md`：多会话、发现探测、配置/模式、历史、图片、MCP、命令面板、消息跳转、计划/用量。
- `docs/design/multi-session.md`：pi / Command Code 单进程多会话能力调查与结论。
- `docs/design/git.md`：右侧 Git 面板、仓库探测、状态/图表、提交与 fetch/pull/push、异步与安全。
- `docs/design/tasks.md`：右侧 Tasks 面板、任务持久化、串行执行/检查双 agent、成功提交与失败回滚。
- `docs/design/components.md`：本地 UI 组件层（widgets.go 样式、components.go 有状态组件、图标子集）、交互约定。
- `docs/design/themes.md`：外观（system/light/dark）、主题目录（default/nord/solarized/catppuccin/claude）、持久化、原生外观同步。
- `docs/design/fonts.md`：分区字体大小（workspace 列表、对话流、输入框、Git 面板）、百分比缩放、持久化。
- `docs/design/plan.md`：测试、MVP 范围、后续、非目标、待定问题、参考链接。

## 铁律
- 客户端能力保持 `fs: false`、`terminal: false`；只做展示和权限审批。
- 不代登录、不内嵌终端、不捆绑 Node；Node 与模型凭据由用户自备，登录只展示 agent 的 `authMethods`/命令。
- 退出清理 agent 进程组；initialize 探测 5 秒超时，配置探测异步执行，失败不阻塞启动。
- 不做 registry 全量发现/在线目录/自动安装；只支持内置表，新 agent 走代码 + 发版。
- pi / codex 不走 npx；直接用 `resources/agents/{pi-acp,codex-acp}/*.bundle.mjs`，不在启动时打包；pi 重打走 `scripts/vendor-pi-acp-local.sh` 或 `scripts/vendor-pi-acp.sh`，codex 重打走 `scripts/vendor-codex-acp.sh`（官方包，`CODEX_PATH` 指向系统 codex）。
- 任务串行执行；每个任务新建 executor 会话，判断另起隔离 checker 进程并携带前几轮上下文；成功只 commit 一次，失败回滚改动并标记 complete/failure，不 push。
- 每次打包发版前先升级 `mygo.json` 的 `version`（semver，当前从 0.4.0 起），并同步 `docs/design/architecture.md` 的版本号，再执行 `go tool mygo build`。
