# ACP 规范符合性

审计基线：ACP v1 稳定 schema `1.32.0`（2026-11-14）。项目在 `initialize` 中声明 `protocolVersion: 1`，所以本文的“稳定规范”特指上游 `schema/v1/schema.json`；`schema/v1/schema.unstable.json`、RFD 和 `_meta` 私有字段都只算 draft/扩展，不能写成“标准 ACP v1”。

## 结论

| 类别 | 现状 |
| --- | --- |
| 基础 ACP v1 链路 | 合规：`initialize`、`session/new`、`session/prompt`、`session/cancel`、`session/update`、`session/request_permission` |
| 客户端能力 | `fs: false` / `terminal: false` 合规；`subagents` 属于 v1 draft，且默认发送 |
| 子代理语义 | 声明范围大于实现：缺 `session_message_chunk`、upsert、`null` 清除、idle 语义 |
| pi-acp 状态/回放 | `_meta.piAcp.*` 是 fork 私有扩展，不是 ACP 标准 |
| 旧 codex-acp | `subagent_spawned` / `subagent_state_update` 是历史私有事件 |
| 可选方法 | 未按 `loadSession` / `sessionCapabilities.list` / `close` 做能力预检 |
| 稳定字段覆盖 | `ContentChunk.messageId`、grouped/boolean config option、`title: null` 清除尚未实现 |

## 稳定 v1 合规项

- `initialize` 发送 `protocolVersion: 1`、`clientInfo`、`fs`/`terminal = false`；不声明 `fs/*`、`terminal/*`。
- `session/new` / `session/load` 使用绝对 `cwd`，始终发送 `mcpServers`（当前为空数组）。
- `session/prompt` 使用 `text` / `image` 内容块；发送图片前检查 `promptCapabilities.image`。
- `session/cancel` 作为 notification 发送；权限回执使用 `selected` + `optionId` 或 `cancelled`。
- JSON-RPC 错误保留 `code` / `data`；`-32000` 按认证错误处理。
- `session/update` 的稳定类型已覆盖：message/thought chunk、tool call、plan、`available_commands_update`、`current_mode_update`、`config_option_update`、`session_info_update`（标题非空时）、`usage_update`。
- 未知 `sessionUpdate` 安静忽略；未知 agent → client 请求回 `-32601`。

## 超出稳定 v1：子代理

### 默认发送 `clientCapabilities.subagents`

`internal/acp.DefaultClientCapabilities` 无条件设置 `Subagents: &SubagentCapabilities{}`，因此每次 `initialize` 都发送 `"subagents": {}`；`internal/acp/client_test.go` 的 `TestCall` 也强制要求它存在。

上游 v1 稳定 schema 的 `ClientCapabilities` 只有 `fs`、`terminal`、`session`、`auth`、`elicitation`（随版本略有增减），没有 `subagents`。`subagents` 只存在于 `schema/v1/schema.unstable.json`，对应尚未稳定的 Subagent Sessions RFD。因此：

- 它可以作为 draft 能力使用，但文档不能称为“标准 ACP v1 子代理”。
- 如果目标改为严格稳定 v1，应默认不发送，或做成设置项；`TestCall` 也要同步调整。

### `subagents: {}` 的语义大于当前实现

上游 draft 对这个 capability 的定义是：客户端理解 child association、work-state 快照、session-directed messages **和 chunks**、以及 restricted-session 语义。当前只完成了一部分：

| Draft 要求 | 当前实现 | 影响 |
| --- | --- | --- |
| `session_message_chunk` | `internal/session/events.go` 没有该常量和解析分支 | 流式会话间消息会丢失内容 |
| `session_message` upsert | 每收到一条就新建 UI 行；`MessageID` 解析后未用于身份 | 同一 `messageId` 的后续 patch 会重复显示 |
| 字段省略=保持、`null`=清除 | `parseSubagentUpdate` 用 `*string` / `Known` 标志，无法区分省略和 `null`；`applySubagentUpdate` 只应用非 nil / Known 字段 | `title`、`description`、`cwd`、`state`、`capabilities` 无法被清空 |
| `state.idle` 可复用、不是终态 | `subagentStatusLabel` 把 idle 显示为 Completed，`subagentStateTerminal` 把 idle 算终态 | UI 把可继续接收任务的 child 显示成已完成 |
| child 可跨父回合复用 | `finishSubagents` 在父 prompt 结束时把运行中的 child 强制改为 completed/failed/cancelled | UI 状态可能覆盖 agent 的真实状态 |

结论：保留 `subagents: {}` 就必须补齐上述 draft 语义；否则这个 capability 的声明不真实。

### `SessionCapabilities.Subagents` 不存在

`internal/acp.SessionCapabilities` 里有 `Subagents json.RawMessage`，但上游 v1 stable 和 unstable 的 `SessionCapabilities` 都没有 `subagents`。只有 **client → agent** 的 `ClientCapabilities.subagents`；没有 agent 侧的 `sessionCapabilities.subagents`。该字段目前未被使用，应删除；`docs/design/discovery.md` 的旧示例已同步删掉 `sessionCapabilities.subagents`。

## 规范外的兼容/私有扩展

这些不是稳定 ACP，但都有明确用途；文档和代码注释必须标明来源。

### pi-acp `_meta.piAcp`

`internal/session/events.go` 读取以下私有字段：

- `_meta.piAcp.startupInfo`：pi 启动信息，属于 fork 生成的伪消息。
- `_meta.piAcp.notice` / `_meta.piAcp.notify`：重试、压缩、排队、扩展通知。
- `_meta.piAcp.replay`：`session/load` 时每条持久化消息的 key 和时间戳。

`_meta` 本身是 ACP 允许的扩展点，但这些字段的语义是 pi-acp fork 私有的，其他 agent 不保证。稳定 v1 的 `ContentChunk` 已经有标准 `messageId`；当前客户端忽略它，只优先用 `_meta.piAcp.replay` 做 replay 行身份，见下文“稳定字段覆盖”。

### 旧 codex-acp 事件

`subagent_spawned` / `subagent_state_update` 是旧版 codex-acp 的私有 child 通知，`internal/session/events.go` 把它们归一化为 `subagent_update`。它们不在 ACP 规范中，保留只是为了兼容旧 bundle。

### legacy `models` 与 `session/set_model`

`internal/session.applySessionState` 会把旧 agent 的 `models` 块合成一个 `id=model` 的 `configOptions` 项，之后仍然调用 `session/set_config_option`。文档曾写“旧适配器回退 `session/set_model`”，但代码没有这个回退；只支持 legacy `models` 的 agent 在切换模型时会失败。

可选处理：

- 补 `session/set_model` 兼容路径；或
- 删除“回退”承诺，并明确 legacy `models` 只用于展示，切换模型需要 `session/set_config_option`。

### `dropLegacyThinkingModes`

`internal/session.dropLegacyThinkingModes` 用于过滤 pi-acp v1 把 thinking levels 塞进 `modes` 的非标准行为。它是 pi 兼容补丁，不是 ACP 模式语义；如果真实 mode id 恰好与 thinking 选项值重合，可能误删合法 modes。

### 宽容读取

以下行为只影响读取，不会向 agent 发送非标准字段：

- `content` 被编码成字符串（`internal/session/events.go` 的 `contentBlocks`）。
- `session/update` 的 update 放在 envelope 顶层而不是 `update` 字段。
- `models`、`subagent_spawned`、`subagent_state_update` 等旧字段/事件。

## 能力协商缺口

ACP 规定可选方法只有在 agent 声明对应 capability 后才可用。以下调用目前没有预检：

| 方法 | 当前调用点 | 需要 |
| --- | --- | --- |
| `session/load` | `internal/session.Start` / `LoadSessionAt`、`internal/ui` 打开存档 | 检查 `agentCapabilities.loadSession`；不支持时隐藏 load 入口或提示不可用 |
| `session/list` | `internal/session.ListSessions` / `ListSessionsIn`（probe.go）、`internal/ui` 启动同步 | 检查 `agentCapabilities.sessionCapabilities.list`；不支持时跳过而不是报错 |
| `session/close` | `Session.Close` / `CloseSessionID`、UI 关闭会话 | 检查 `agentCapabilities.sessionCapabilities.close`；不支持时直接关进程/忽略 |

`session/set_mode` 和 `session/set_config_option` 通常由返回的 `modes` / `configOptions` 隐式表明可用；当前 UI 也只在有选项时展示。`session/delete`、`session/resume` 等可选方法没有使用，不属于缺口。

## 稳定字段覆盖不足

这些方向不是“超出规范”，而是规范内字段没有消费完整：

### `ContentChunk.messageId`

稳定 v1 的 `agent_message_chunk` / `user_message_chunk` / `agent_thought_chunk` 可以带 `messageId`。`internal/session.Event` 没有这个字段；`ui.applyUpdate` 在 replay 时用 `Event.Replay`（pi 私有 `_meta`）判断新行，否则按角色合并。codex-acp 使用标准 `messageId`，因此 codex 的 `session/load` 重放可能把同角色的相邻消息合并错。

建议：优先用 `messageId` 作为 replay 行身份，`_meta.piAcp.replay` 只作为没有 `messageId` 时的 fallback。

### `SessionConfigOption` 的完整联合类型

稳定 v1 的 `SessionConfigOption` 是 `select` / `boolean` 的联合，`select` 的 `options` 还支持 grouped（`{group, name, options}`）和 flat 两种形式。当前 `internal/acp.ConfigOption` 只按 flat select 解码：

- `CurrentValue string` 遇到 `boolean` 的 `currentValue: true` 会让整个 `session/new`/`session/load` 解码失败。
- grouped select 会被解成空 `value` 的选项，UI 显示空白项。
- `ui.composer` 只把 `Type` 为空或 `select` 的选项当 select。

当前客户端没有声明 `session.configOptions.boolean`，所以 agent 不应发 boolean；但 grouped select 是 `select` 的合法形式，应该支持。至少应按 schema 的 `x-deserialize-skip-invalid-items` 思路逐项容错，而不是整份失败。

### `SessionInfoUpdate` 的 `null`

稳定 v1 的 `session_info_update.title` / `updatedAt` 为 nullable，`null` 表示清除。当前 parser 用 `string`，UI 只在标题非空时更新，所以 `null` 被当成“没更新”。需要保留“字段是否存在/是否为 null”的信息。

### `PromptResponse.usage`

`usage` 在 v1 stable 的 `PromptResponse` 中不存在，属于 `schema.unstable.json`。当前 `internal/acp.PromptResult.Tokens()` 解析并显示为 turn usage；如果继续使用，文档必须标注为 draft 字段，不能写成“ACP 标准用量”。

### 未处理的稳定/通知扩展

`notice`、`compaction_update`、`compaction_summary_chunk`、`plan_update`、`plan_removed` 等当前未解析，但客户端也没有声明对应 capability（`session.notices`、`session.compaction`、`plan` 等），所以这是“没接”而不是协议违规；启用前需要先声明能力并补解析。

## 待办

- [ ] 明确子代理目标：严格稳定 v1（默认关闭 `subagents`、删除 `SessionCapabilities.Subagents`、修正文档）或继续完整实现 v1 draft。
- [ ] 若保留 draft：处理 `session_message_chunk`、`session_message` upsert、`subagent_update` 的 `null` 清除、idle 可复用语义，并决定父回合结束时是否结束 child。
- [ ] 给 `session/load` / `session/list` / `session/close` 加 capability 预检，不支持时静默降级。
- [ ] 用稳定 `ContentChunk.messageId` 作为 replay 行身份，`_meta.piAcp.replay` 仅作 fallback。
- [ ] 支持 grouped select / boolean config option，或至少逐项跳过坏选项，避免整份 `session/new` 失败。
- [ ] 支持 `session_info_update.title = null` / `updatedAt = null` 的清除。
- [ ] 补 legacy `models` → `session/set_model` 兼容，或删除相关文档承诺。

## 相关测试

- `internal/acp/client_test.go`：`TestCall` 目前强制 `subagents` 存在。
- `internal/session/events_test.go`：`TestParseEventSubagentUpdate` 覆盖 `subagent_update` 的基本解析（不覆盖 `null` 清除 / chunk / upsert）。
- `internal/acp/usage_test.go`：`PromptResponse.usage` 解析。
- `internal/ui/replay_test.go`：replay 行合并（目前主要覆盖 pi `_meta` 路径）。
