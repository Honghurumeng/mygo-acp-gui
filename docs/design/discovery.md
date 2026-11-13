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

当前实现：`agent.BuiltinLaunchResults` 解析 pi 和 command-code 的路径/版本；对每个成功的 spec 并行执行 `session.Probe`（仅 `initialize`，5 秒）。单个 agent 失败只记录状态，不隐藏另一个。侧栏状态行显示 agent 名称、CLI 版本和 `ready` / `not-installed` / `installed-not-acp` / `needs-auth` / `error`；pi 用 `pi --version`，不显示 vendored pi-acp 的 `VERSION`（`initialize.agentInfo.version` 是适配器版本）。状态行右侧的齿轮打开设置面板。

状态定下之后再跑一轮**配置探测**（`session.ProbeConfigModels`）：用临时工作区做 `initialize` + 一次用完即弃的 `session/new`，把 `configOptions` 和 `modes` 分别缓存到内存的 `AgentConfigs[agentID]` / `AgentModes[agentID]`，让输入框在连接前就能选 provider / model / thinking / mode。`session/new` 一返回就先发布模型列表和当前模型的档位，同一个进程再继续扫其余模型。这一轮 30 秒总预算（`session/new` 12 秒、每次模型切换 5 秒）、异步执行，失败只丢缓存，既不延迟启动也不改变状态。

`AgentConfigs` 只是“当前模型”的快照；每个模型自己的 thinking 档位单独存在 `<data>/model-config.json`（`internal/modelconfig`，agent → model → 那次切换后返回的完整 `configOptions`）。启动扫模型时先读这个文件，用 `session/new` 返回的模型列表和它对照：已在文件里的模型不再发 `set_config_option`，只对缺的模型逐个切换并读取，读到一条就落盘一条。缓存完整时启动只做 `session/new`；agent 发了新模型才多花几次切换。超时或切换失败的模型保持缺失，下次启动继续补。

ACP 没有独立的 provider 可用性查询；`session/new` 返回的 model `configOptions` 是 agent 对当前可用模型的声明（例如 pi-acp 只列出已配置凭据的 provider）。因此缓存会持续更新：草稿打开模型面板时异步重读（正常 30 秒节流，缓存为空时 5 秒重试；失败或无 session 时保留旧列表），连接成功后用真实 session 的选项和 modes 覆盖，`config_option_update` / `current_mode_update` / `set_config_option` / `set_mode` / 断开时也同步。刷新后不存在的 provider 或 mode 不再展示。pi-acp v1 把 thinking levels 放在 `modes` 的兼容块，如果全部命中 `thought_level` 选项值，读取时直接丢弃，避免草稿或已连接时出现 stale 的重复 mode。

草稿切到另一个 model 时，优先用 `model-config.json` 里该模型的快照渲染 thinking 和其他随模型变化的选项；模型列表本身仍取最新一次 `session/new` 的结果（新模型可能刚出现）。只有快照缺失（首次启动还没扫到、或切换失败）时才隐藏旧档位并提示 `Connect to load levels`，同时清掉该 model 的 thinking 草稿；Connect 后由真实 session 的 `config_option_update` 给出新档位。已连接会话每次 `set_config_option` / `config_option_update` 也把结果写回这张表，所以手动切过一次的模型不必等下一次启动。

路径覆盖：`ACP_GUI_NODE`、`ACP_GUI_PI`、`ACP_GUI_COMMAND_CODE` 必须是绝对路径；`ACP_GUI_SHELL` 可换登录 shell。优先级为环境变量 > `settings.json` 的 `paths` > 登录 shell 解析；无效覆盖保留 shell 结果并警告。

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

`session/new` 是第二层验证（能否真正开会话），用户点“连接”时做；`session/new` 返回 `-32000` 或 `auth_required` 时进入“需要登录”状态，只展示 agent 的 `authMethods`（名称、描述、可复制命令）；应用不代登录、不内嵌终端，由用户自己在外部终端解决后重连。

配置探测同样走 `session/new`，但它只读 `configOptions` 和 `modes`：失败被忽略（需要登录的 agent 本来就拿不到），不影响状态机；探测出的选项用于草稿的模型面板和 mode chip，草稿上的选择在真正 Connect 后由 `session/set_config_option` / `session/set_mode` 补齐。

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
