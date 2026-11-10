# AGENTS.md

## 目标
用 Go + MyGo 做原生 ACP GUI：自动发现本机已安装的 ACP agent，选择 agent 和工作区后聊天。
现有 Node 版本（`server/`、`public/`）只作为参考，新代码放在 `gui/`。

## 文档规则
- 本文件最多 30 行，只放目标、铁律和文档索引。
- 其余设计必须写进 `docs/design/` 下的单独文档；只要会超过 30 行，绝对改用单独文档记录。
- 新增或修改设计后，同步更新下面的索引和对应文档。

## 设计文档
- `docs/design/architecture.md`：整体架构、Go 包划分、ACP 客户端、MyGo UI、数据与安全。
- `docs/design/discovery.md`：官方 registry、launch spec、候选生成、initialize 探测、状态机、手动测试。
- `docs/design/pi.md`：pi-acp vendor、SDK/zod 依赖、Node/pi 版本、PATH、认证、更新。
- `docs/design/plan.md`：测试、MVP 范围、后续、非目标、待定问题、参考链接。

## 铁律
- 客户端能力保持 `fs: false`、`terminal: false`；只做展示和权限审批。
- 退出清理 agent 进程组；探测 5 秒超时，失败不阻塞启动。
- pi 不走 npx；直接用 `gui/resources/agents/pi-acp/pi-acp.bundle.mjs`，不重复打包；升级只跑 `scripts/vendor-pi-acp.sh`。
