# 架构

## 目标

原生桌面 ACP GUI：Go + [MyGo](https://github.com/egoist/mygo) 写界面，自动发现本机已安装的 ACP agent，选择 agent 和工作区后聊天。

早期 Node 原型（`server/` + `public/`）已移除，只保留在 git 历史中；当前实现是仓库根目录的 Go + MyGo 工程。

## 整体架构

```
MyGo 原生窗口 (Go + ui)
  ├─ Agent 选择 / 工作区选择 / 会话视图
  ├─ ui/markdown.Stream / View      ← agent_message_chunk
  ├─ ui.List(FollowEnd)             ← 消息列表
  ├─ ui.Collapsible                 ← thought / tool_call
  ├─ ui.Select / Segmented          ← model / thought_level
  └─ ACP client (Go)
        ↕ newline-delimited JSON-RPC 2.0 (stdin/stdout)
     agent 子进程（pi-acp、command-code、opencode、goose、cursor…）
        ↕
     agent 自己的模型/工具运行时
```

## Go 包划分

```
仓库根/
  main.go                  MyGo 应用入口与根视图
  main_test.go             UI 冒烟测试
  mygo.json                App 元数据与打包配置
  internal/acp/            JSON-RPC、请求/通知、权限、行分帧（已实现）
  internal/agent/          launch spec、目录、探测、缓存（计划）
  internal/session/        每个 agent 进程 + 会话生命周期（计划）
  internal/catalog/        官方 registry + 内置覆盖 + 用户自定义（计划）
  internal/ui/             MyGo 视图与状态（计划）
  resources/
    agents/pi-acp/
      pi-acp.bundle.mjs    单文件 bundle，启动时直接 node 执行（见 pi.md）
      package.json/VERSION/LICENSE/licenses/THIRD_PARTY_NOTICES.md/SHA256SUMS
    registry.json          官方 registry 快照（计划）
```

分层原则：`internal/acp` 不 import UI；探测/目录逻辑可独立测试；UI 只读状态、发命令。

## 工程与构建

- `go.mod`：module `github.com/reed/acp-gui`，Go 1.27.3（`GOTOOLCHAIN=auto` 会自动拉取）。
- MyGo CLI 通过 `tool github.com/egoist/mygo/cmd/mygo` 固定在 `go.mod`，统一用 `go tool mygo`。
- 无前端构建：原生 UI，入口 `main.go`，配置在 `mygo.json`。
- App：name `ACP GUI`、slug `acp-gui`、identifier `dev.reed.acp-gui`、version `0.1.0`。
- `go tool mygo build` 产物在 `build/<os>-<arch>/`（已 gitignore）；`resources/` 整目录进 `.app`。
- 当前已实现 `internal/acp`：行分帧、请求/响应匹配、通知、agent→client 请求、权限结果、能力固定 `fs:false`/`terminal:false`。

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
  - JSON-RPC error 保留 `code` / `data`；`-32000` 或 `authMethods` → 认证流程
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

- Unix：`Setpgid: true`，退出时 `kill(-pid, SIGTERM)`，宽限后 `SIGKILL`。
- Windows：Job Object 或 `taskkill /T /F`。
- 先关 stdin 请求优雅退出。
- 应用退出 / 窗口关闭必须清理所有 agent 进程。

## MyGo UI

- 消息流：`markdown.Stream` 显示进行中的回复，结束后切换 `markdown.View`（同一位置）。
- 聊天列表：`ui.List`，`FollowEnd` + `FollowAnimation = &ui.Smooth`；用户上滑后停止跟随。
- 思考/工具：`ui.Collapsible`；工具卡片显示标题、状态、可展开的入参/输出。
- 权限：`ui.Dialog` / `ui.AlertDialog` 或输入框上方的内联卡片；按钮对应 `optionId`。
- 配置：`ui.Select` / `ui.Segmented`；模型和思考强度从 `configOptions` 生成，不要硬编码。
- 输入：`ui.TextInput`，Enter 发送、Shift+Enter 换行。
- 跨 goroutine 改状态：`win.Update(func(){ ... })`。
- 资源：`mygo.App.Path(mygo.PathResources)`；数据：`mygo.App.Path(mygo.PathData)`。
- 登录：`plugins/terminal` 可作为内嵌终端。
- 参考 MyGo 示例：`examples/markdown-chat`、`examples/terminal`。

## 数据与配置

存储在 `mygo.App.Path(mygo.PathData)`：

```
settings.json        上次使用的 agent、工作区、node/pi 路径
agents.json          用户自定义 agent（命令、参数、env、cwd）
probe-cache.json     探测结果（路径、mtime、版本、能力、时间）
logs/                每个 agent 的 stderr / 协议日志（滚动）
```

registry 快照计划放 `resources/registry.json`；联网刷新成功后写回数据目录作为缓存，失败时回退内置快照，不阻塞启动。

## 安全与边界

- 启动扫描只对已知 / 用户添加 / registry 条目做探测，不遍历所有可执行文件。
- 探测超时 5 秒；每次探测都要能杀掉进程组，不能残留。
- 不静默安装 registry 里的 agent；MVP 只展示安装提示/命令。
- agent 继承用户环境；不在日志里打印 API key。stderr 可能含敏感信息，默认折叠。
- 不声明 `fs/*`、`terminal/*` 能力；未知 agent→client 请求一律拒绝。
- vendored pi-acp 固定版本并附 LICENSE；更新走显式脚本，不自动跟随 npm。

## 相关文档

- [Agent 发现](discovery.md)
- [pi 集成](pi.md)
- [测试与计划](plan.md)
