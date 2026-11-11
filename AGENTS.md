# AGENTS.md

## 目标
用 Go + MyGo 做原生 ACP GUI：检测内置的 pi / command-code 是否可用，选择 agent 和工作区后聊天。
当前实现是仓库根目录的 Go + MyGo 工程。

## 文档规则
- 本文件最多 30 行，只放目标、铁律和文档索引。
- 其余设计必须写进 `docs/design/` 下的单独文档；只要会超过 30 行，绝对改用单独文档记录。
- 新增或修改设计后，同步更新下面的索引和对应文档。

## 设计文档
- `docs/design/architecture.md`：整体架构、Go 包划分、ACP 客户端、MyGo UI、数据与安全。
- `docs/design/discovery.md`：内置 agent 表（pi、command-code）、launch spec、路径解析、initialize 探测、状态机、手动测试。
- `docs/design/pi.md`：pi-acp vendor、SDK/zod 依赖、Node/pi 版本、PATH、认证、更新。
- `docs/design/pi-fork.md`：fork pi-acp 的最小改动、vendor 接入与验证。
- `docs/design/command-code.md`：Command Code 启动、已验证 ACP 流程、权限/取消、测试。
- `docs/design/features.md`：多会话、发现探测、配置/模式、历史、图片、MCP、命令面板、计划/用量。
- `docs/design/plan.md`：测试、MVP 范围、后续、非目标、待定问题、参考链接。

## 铁律
- 客户端能力保持 `fs: false`、`terminal: false`；只做展示和权限审批。
- 不代登录、不内嵌终端、不捆绑 Node；Node 与模型凭据由用户自备，登录只展示 agent 的 `authMethods`/命令。
- 退出清理 agent 进程组；探测 5 秒超时，失败不阻塞启动。
- 不做 registry 全量发现/在线目录/自动安装；只支持内置表，新 agent 走代码 + 发版。
- pi 不走 npx；直接用 `resources/agents/pi-acp/pi-acp.bundle.mjs`，不重复打包；升级只跑 `scripts/vendor-pi-acp.sh`。
