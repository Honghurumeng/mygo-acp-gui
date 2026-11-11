# Agent 发现

## 范围

**不做 ACP registry 全量发现**，也不扫描本机所有可能的 agent。应用只支持**内置表**里的 agent，目前两个：

| id | 名称 | 启动方式 |
| --- | --- | --- |
| `pi` | pi | vendored `pi-acp` bundle；需要系统 Node 22+ 和 PATH 上的 `pi`（见 [pi.md](pi.md)） |
| `command-code` | Command Code | `command-code acp` |

- 不联网拉取/刷新 agent 目录，不内置 `registry.json`。
- 不扫描 npm / pnpm / bun / uv 全局包，不导入 Zed `agent_servers`。
- 不自动安装 agent；未安装时只显示安装提示。
- 新增 agent = 修改内置表 + 测试 + 发版；运行时不会发现表外的 agent。

`command-code` 是本机已验证的第二个 agent；其余 agent 不进入默认实现范围。

## 内置表

内置表是编译进 `internal/agent` 的 Go 常量，不读资源文件，也不合并用户覆盖。每条需要：

```go
type BuiltinAgent struct {
	ID          string
	Name        string
	InstallHint string
}
```

具体 command/args/version 在 `BuiltinLaunchResults` 里按 ID 分派（pi 是 vendored 特例）。示意：

| id | command | args | versionArgs | installHint |
| --- | --- | --- | --- | --- |
| `pi` | （vendored，不走 command） | — | `pi --version` / `node --version` | 见 pi.md |
| `command-code` | `command-code` | `acp` | `--version` | 以官方安装文档为准；需要 Node 22+ |

pi 是特例：命令由解析后的 `node` + `resources/agents/pi-acp/pi-acp.bundle.mjs` 拼成，不使用 `Command` 字段；Node/pi 的定位与版本要求见 [pi.md](pi.md)。

只有两个 agent，所以表保持手写、可读；不引入 JSON 目录、registry 解析或 catalog 包。

## 启动检测

对内置表逐条执行：

1. 解析绝对路径：继承登录 shell 环境；pi 需要 node、bundle 和 `pi`，command-code 用 `command -v command-code`。
2. 版本：跑 `versionArgs`，5 秒超时；失败只影响版本显示。
3. `initialize` 探测（5 秒），确认它真的是 ACP agent。
4. 结果写入内存状态；启动异步执行，不阻塞窗口。
5. “重新检测”重跑以上流程。

当前实现：`agent.BuiltinLaunchResults` 解析 pi 和 command-code 的路径/版本；对每个成功的 spec 并行执行 `session.Probe`（仅 `initialize`，5 秒）。单个 agent 失败只记录状态，不隐藏另一个。状态行展示 `ready` / `not-installed` / `installed-not-acp` / `needs-auth` / `error`。

路径覆盖：`ACP_GUI_NODE`、`ACP_GUI_PI`、`ACP_GUI_COMMAND_CODE` 必须是绝对路径；`ACP_GUI_SHELL` 可换登录 shell。优先级为环境变量 > `settings.json` 的 `paths` > 登录 shell 解析；无效覆盖保留 shell 结果并警告。

不写探测缓存文件：只有两个 agent，启动探测成本可控；缓存会额外引入 mtime/TTL 失效逻辑，收益低。

## 探测（probe）

“是不是 ACP agent”的唯一可靠判断是握手。探测流程：

1. spawn 候选进程（cwd 用用户工作区或临时目录），继承登录 shell 环境。
2. 写一行：

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":false,"writeTextFile":false},"terminal":false}}}
```

3. 等待 stdout 上的一行 JSON，超时默认 **5 秒**。
4. 判定：
   - 有 `result.protocolVersion` / `result.agentInfo` / `result.agentCapabilities` → **ACP OK**
   - 返回 JSON-RPC `error` → 记录错误，标记失败
   - stdout 出现非 JSON → 标记“不是 ACP / 输出污染”
   - 超时 / 退出 → 标记失败，保留 stderr 末尾
5. 关闭 stdin，先 SIGTERM，宽限后杀**进程组**。

探测时声明 `fs: false`、`terminal: false`，与本应用实际能力一致；不要为了“探测成功”而声明用不到的能力。

`session/new` 是第二层验证（能否真正开会话），只在用户选中 agent / 点“连接”时做。`session/new` 返回 `-32000` 或 `auth_required` 时进入“需要登录”状态，只展示 agent 的 `authMethods`（名称、描述、可复制命令）；应用不代登录、不内嵌终端，由用户自己在外部终端解决后重连。

## 状态模型

| 状态 | 含义 | 动作 |
| --- | --- | --- |
| `ready` | 路径解析成功，`initialize` 探测通过 | 可连接 |
| `needs-auth` | `initialize` 成功，但 `session/new` 要求认证 | 展示 `authMethods`/命令，用户自行在终端登录后重连 |
| `installed-not-acp` | 命令存在但握手失败 | 显示 stderr，提示不是 ACP |
| `not-installed` | 内置表有，但本机没找到 | 显示安装提示 |
| `error` | 超时/崩溃/协议错误 | 重试、查看日志 |

实现：`ready` 通过 initialize 探测；路径解析失败为 `not-installed`；握手失败/非 JSON 为 `installed-not-acp`；超时/崩溃为 `error`；`needs-auth` 在用户点 Connect 后由 `session/new` 触发，并更新该 agent 状态。只有 `ready` 的 agent 进入可连接下拉框。

## 手动命令行测试

macOS/Linux（需有 `timeout`；否则用 probe 脚本）：

```bash
# command-code
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":false,"writeTextFile":false},"terminal":false}}}' \
  | timeout 5 command-code acp
```

```bash
# pi（路径按实际资源目录替换）
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":false,"writeTextFile":false},"terminal":false}}}' \
  | timeout 5 node resources/agents/pi-acp/pi-acp.bundle.mjs
```

Windows PowerShell：

```powershell
'{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":false,"writeTextFile":false},"terminal":false}}}' `
  | & command-code acp
```

预期看到一行 JSON：

```json
{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentInfo":{"name":"Command Code","version":"1.88.5"},"agentCapabilities":{"loadSession":true,"promptCapabilities":{"audio":false,"image":true,"embeddedContext":true}},"authMethods":[{"id":"command-code-cli","name":"Command Code CLI login","description":"Sign in once with `cmd login` in a terminal; this session reuses that account."}]}}
```

`command-code` 会把 `[acp] initialize: ...` 写到 stderr。stdout 必须只有协议 JSON；探测时 stderr 单独收集，不能当作响应。Command Code 的完整实测流程、权限选项、未登录错误与测试命令见 [command-code.md](command-code.md)。

跨平台探测已由 `internal/session.Probe` 提供：发送 `initialize`、5 秒超时、收集 stderr、关闭进程组；UI 在发现阶段并行调用。

## 安全

- 只探测内置表里的两个 agent，不遍历全盘，不接受运行时的表外 agent。
- 每次探测都必须能杀掉进程组。
- 不自动安装 agent；MVP 只展示安装提示/命令。
- 不把 API key 写入应用配置或日志。
