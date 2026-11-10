# 测试与计划

## 测试

### 单元 / 集成

- `internal/acp`：行分帧（多 chunk、CRLF、非 JSON、U+2028 内容）、请求/响应匹配、权限回执
- `internal/agent`：候选生成、去重、launch spec 解析、缓存命中/失效、超时与进程组清理
- `internal/catalog`：registry 快照解析、缺字段/坏 JSON、内置覆盖 user override
- 探测 fixture：正常 agent、慢 agent、只打印 stderr 的 agent、立即退出的 agent、stdout 混入日志的 agent

复用 `scripts/fake-agent.mjs` 作为假 agent，跑 UI/客户端集成测试；fixture 放在 `testdata/`。

pi-acp bundle 冒烟（只 `initialize`，不 spawn pi、不碰凭据）：

```bash
node scripts/pi-acp-smoke.mjs
```

### UI

按 MyGo 约定用 `ui.NewTester`：

- 只测真实会变的行为：点击 agent 切换状态、断开、发送、停止、权限选择、配置切换
- 不测标签文案、默认路径排序
- 新增 UI 测试跑 `-count=5`，避免 flaky

### 真实 agent（可选、本机）

```bash
ACP_REAL_AGENTS=1 go test ./internal/agent -run TestRealAgents -v
```

只做 `initialize`，不做 `session/new`，避免触发登录/网络。真实 pi 的冒烟单独跑，要求 Node + pi 已配置。

### 手动验收

1. 从 Finder 启动 `.app`（不是终端）——验证 PATH 解析。
2. 本机已装 `command-code`：应显示为 ready，能开会话。
3. 本机未装某 registry agent：显示 `not-installed` + 安装提示，不能卡住。
4. 本机装了非 ACP 命令：显示 `installed-not-acp` + stderr。
5. pi：离线（断网）启动，仍能连接（vendored pi-acp），且能聊天。
6. 权限弹窗、取消、关闭窗口：无残留 `pi` / agent 进程。
7. 退出再启动：上次 agent / 工作区恢复。

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

- [ ] agent 发现（registry 快照 + 内置 launch spec + PATH/托管目录 + 自定义）
- [ ] `initialize` 探测、超时、缓存、状态展示
- [ ] agent 选择 + 工作区选择 + 最近使用
- [ ] ACP 会话：流式 Markdown、思考、工具调用、计划、权限、取消、新会话
- [ ] 模型/思考强度配置
- [x] pi-acp 单文件 bundle vendored（`resources/agents/pi-acp/`）
- [ ] 系统 Node 定位 + 认证引导
- [ ] 退出清理、错误诊断、日志
- [ ] 基础测试：framing、探测、目录、UI 关键行为

## 后续

- [ ] registry 在线刷新 + 自动安装（sha256、用户确认）
- [ ] `session/load` 历史会话列表
- [ ] 图片/资源输入
- [ ] 多会话并行
- [ ] ACP 报文监视器
- [ ] 导入 Zed `agent_servers`
- [ ] Windows / Linux 打包验证

## 明确不做

- 不实现 ACP agent 端（pi-acp 已经做了）
- 不重写 pi（pi 仍然是 Node 程序）
- 不做 Mac App Store 沙箱版本
- MVP 不自动安装任意 agent
- 不声明 `fs` / `terminal` 能力

## 待定问题

1. registry 刷新策略：内置快照 + 每周检查？还是只在用户点“刷新”时联网？
2. 是否要把 Node 运行时也打进 app？MVP 建议依赖系统 Node，打包阶段再评估。
3. 首批要保证的 agent 列表（建议：pi、command-code、opencode、goose、cursor、gemini、claude-acp、codex-acp）。
4. 认证统一用内嵌 terminal，还是先只展示命令、让用户自己去终端登录？
5. Windows 的进程树清理与 PATH 解析优先级。

## 参考

- ACP 协议与 registry：https://agentclientprotocol.com / https://github.com/agentclientprotocol/registry
- registry JSON：`https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json`
- `@geohar/pi-acp`：npm 包（MIT）；fork `https://github.com/georgeharker/pi-acp`，原始项目 `https://github.com/svkozak/pi-acp`。
- pi RPC 文档（本机安装包内）：`docs/rpc.md`、`docs/rpc-commands.md`、`docs/json.md`、`docs/rpc-extension-ui.md`
- MyGo 文档：`github.com/egoist/mygo/docs`，重点是 `docs/native.md`、`docs/ui/markdown.md`、`docs/ui/list.md`、`docs/ui/views.md`、`docs/distribution.md`、`docs/plugins/terminal.md`
- MyGo 示例：`examples/markdown-chat`、`examples/terminal`
- 早期 Node 参考实现（已移除，见 git 历史）；假 agent fixture：`scripts/fake-agent.mjs`
