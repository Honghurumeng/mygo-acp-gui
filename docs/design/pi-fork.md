# pi-acp fork 修改清单

目标：fork `https://github.com/georgeharker/pi-acp`（vendored 0.4.0 的来源），让 pi 通过 ACP 发出上下文用量、单轮用量，正确暴露当前模型的推理强度，在历史回放里保留 thinking 和图片，并让工具调用标题带上具体目标。客户端仍然只运行 vendored bundle，不引入运行时 npm/npx。

状态：已实施。本地源码在仓库根目录的 `pi-acp-main/`（被 `.gitignore` 忽略，不随主仓库提交），产物已重新打包到 `resources/agents/pi-acp/`。

- bundle sha256：`61f648af1c1a4365904ce076ac78c4bb582db566d106ac59a2a93990c25bb8ee`
- 体积：460 KiB
- 上游基线：`@geohar/pi-acp@0.4.0`

## 1. fork 源码

### `session/list` 的工作区语义

上游用 `params.cwd ?? lastSessionCwd` 决定过滤条件，导致客户端省略字段或传 `null` 时都只返回当前进程工作区的会话。GUI 需要一次导入所有工作区的历史，因此改为：

- `cwd` 缺失（undefined，Zed 的 `{}`）→ 仍回退到 `lastSessionCwd`，保留项目内 `/resume` 语义。
- `cwd: null` → 明确表示“全部工作区”，不再回退。
- `cwd: "<绝对路径>"` → 只过滤该工作区。

分页仍按 50 条一页、数字 offset 游标。`listSessions()` 每页原本都会重新扫描全部会话文件（O(n²)），现在用 10 秒 TTL 的进程内缓存复用同一次扫描，GUI 拉 460 条历史从约 14 秒降到约 4 秒（含进程启动）。

### 会话标题

pi 没有自动起名：JSONL 里的 `session_info` 只在用户显式命名（`/name`、`pi --name`）时出现，`session/list` 的标题是读取时用首条 user 消息算出的兜底值，不是事件。带图片的 prompt 会被 pi 写成 `<临时图片路径> <正文>`，兜底标题因此变成 `/var/folders/.../pi-clipboard-....png`；GUI 只能等下次 `session/list` 同步才更新本地存档。

fork 改动：

- 新增 `src/acp/fallback-title.ts`：去掉开头的附件路径（要求绝对路径、至少两个斜杠，避免误伤 `/name` 和 URL），保留后面的正文；只有路径时返回 `[Image]`；标题按 80 个 Unicode code point 截断。
- `src/acp/pi-sessions.ts` 的 `pickFallbackTitleFromHead()` 改用 `fallbackTitleFromContent()`，同时识别 content 里的 `image` 块。
- `src/acp/session.ts` 新增 `announceFallbackTitle()`：仅新建会话（`newSession`）启用；首次 `session/prompt` 前用 `get_state.sessionName` 判断是否已有显式名字，没有就合成一条 `session_info_update`（标题来自 ACP prompt 文本，不含 pi 注入的图片路径）。恢复的会话不合成，避免用「继续」覆盖原有的首条消息标题。`get_state` 失败时下次 prompt 重试；已有显式名字时绝不覆盖。
- `announceFallbackTitle()` 在 `prompt()` 里 fire-and-forget，不再 `await`：查 `get_state` 会让 `prompt()` 在 `startTurn()` 前让出事件循环，紧跟其后的 cancel / agent 退出会看到空的 `pendingTurn`，turn 永远不结算（组件测试里表现为 cancel、排队、proc-exit 一组用例失败）。标题是元数据，晚一个微任务发不影响 UI。
- 测试：`test/unit/fallback-title.test.ts`、`test/unit/session-title-update.test.ts`。

GUI 侧收到 `session_info_update.title` 后会走 `applyUpdate → setArchivedTitle` 直接写 `sessions.json`；启动读存档时也会清洗旧版本留下的路径标题（见 `internal/ui/title.go`）。

### 工具调用标题

ACP 客户端折叠工具卡片时只显示 `title`，`rawInput` 要展开才看得到。上游只把 bash 的 title 设为命令（`bashCommand(args)`），其他工具统一发 `title: toolName`，于是 read/write/edit/grep 的卡片只有“read”“edit”，看不出动了哪个文件；GUI 的详情面板又优先显示输出，参数在完成后会被顶掉。

fork 改动（`src/acp/translate/pi-tools.ts`、`src/acp/session.ts`）：

- 新增 `toToolCallTitle(toolName, args)`：
  - `read` / `write` / `edit` → `read <path>`（兼容 `file_path`）；
  - `grep` → `grep <pattern>`，带 `path` 时 `grep "<pattern>" in <path>`（pattern 含空白时加引号）；
  - `find` → `find <pattern>` / `find <pattern> in <path>`；
  - `ls` → `ls <path>`，缺省 `ls .`；
  - 其他工具（MCP、扩展自定义）返回 `undefined`，沿用工具名。
- 参数未流完时返回 `undefined`，首次 `tool_call` 仍用工具名；`toolcall_delta` / `tool_execution_start` 的既有工具调用更新会补发 title，把卡片标题升级成带目标的版本（之前只在首次 `tool_call` 设 title，之后不再更新）。
- `loadSession` 回放同样带目标：pi 把参数存在 assistant 的 `toolCall` 块上，`toolResult` 里没有；回放前按 `toolCallId` 建参数索引，历史卡片用 `bashCommand(args)` / `toToolCallTitle(toolName, args)`，并把 `rawInput` 设为原始参数（之前是 `null`，非 bash 标题只有工具名）。
- 测试：`test/unit/tool-call-titles.test.ts`（纯函数映射 + 流式参数补齐 title 的会话级验证）、`test/component/session-load-toolresult.test.ts`（历史回放 title/rawInput）。

真实 pi 实测：实时 `read` 标题为 `read /Users/.../AGENTS.md`，`session/load` 回放的历史 `read` 也是同一标题且带 `rawInput`；bash 实时和回放都直接显示命令。

### 上下文用量

`src/acp/session.ts`（`PiAcpSession`）：

- 新增 `toAcpUsage()`：把 pi 的 `input` / `output` / `cacheRead` / `cacheWrite` / `reasoning` / `totalTokens` 映射成 ACP `Usage`。
- 新增 `emitContextUsage()`：调 pi RPC `get_session_stats`，把 `contextUsage.tokens/contextWindow` 发成 `usage_update`；`stats.cost > 0` 时附带 `{ amount, currency: 'USD' }`。`tokens` 为 `null`、字段缺失或查询失败时静默跳过。
- `handlePiEvent` 的 `message_update` 读取顶层 `ev.usage`，保存当前 turn 的最新非空 usage。
- `startTurn()` 清空当前 usage；turn 结算时把 usage 快照推进 `settledTurnUsage` 队列。
- 新增 `consumeTurnUsage()`，按 prompt 结算顺序取出一份 usage。用队列而不是单个字段，避免排队 prompt 在下一轮 `startTurn()` 清空后丢上一轮结果。

`src/acp/agent.ts`（`prompt()` 普通路径）：

```ts
const result = await session.prompt(message, images)
const usage = session.consumeTurnUsage()
await session.emitContextUsage()

const stopReason: StopReason = /* error -> cancelled/end_turn */
return { stopReason, ...(usage ? { usage } : {}) }
```

每个 turn 结束发一次上下文用量；不要在每次 `message_update` 都查 stats。`/compact` 结束后也调用一次 `emitContextUsage()`，压缩后立即刷新环。

### 单轮 usage

- `message_update.usage` 是 pi 当前 assistant response 的累计 usage；持续覆盖当前 turn 的 `turnUsage`，provider 在完成前可能一直报 0，因此只在非空时覆盖。
- ACP 映射：`input -> inputTokens`、`output -> outputTokens`、`reasoning -> thoughtTokens`、`cacheRead -> cachedReadTokens`、`cacheWrite -> cachedWriteTokens`、`totalTokens` 原样保留（缺失时按各项相加）。
- turn 结果没有有效 usage 时，`PromptResponse` 不带 `usage`，UI 不会显示 `turn …`。

### 推理强度

- `src/pi-rpc/process.ts`：新增 `get_available_thinking_levels` 命令和 `getAvailableThinkingLevels()`；`ThinkingLevel` / `setThinkingLevel()` 补上 pi 1.5 的 `max`。
- `src/acp/agent.ts`：`getThinkingState()` 从 pi RPC 读取当前模型支持的真实 level 列表，不再写死 `off..xhigh`；当前值仍来自 `get_state.thinkingLevel`。
- 回退顺序：`get_available_thinking_levels` → `state.model.thinkingLevelMap` 中非 null 的 key → 旧版固定列表。当前值若不在列表中会补进去，避免 UI 显示空值。
- 切模型后 `emitConfigOptionsUpdate()` 会重新调用同一套逻辑，所以 thinking 选项和当前值跟着新模型变。
- 真实 pi 实测：默认 `deepseek-v4.1-flash` 返回 `low/high/max`、当前 `max`；切到 `opencode-go/hy4` 返回 `off/high`、当前 `high`；切到 `openai/gpt-5` 返回 `minimal/low/medium/high`、当前 `high`。

### 历史回放里的 thinking

上游 `loadSession` 回放 assistant 消息时只发文本块（`normalizePiAssistantText` 的注释就是 “only replay text blocks for MVP”），而 pi 把推理持久化成 `{ type: 'thinking', thinking: '…' }` 块，于是恢复历史后推理全部消失（实时流式路径本来就有 `thinking_delta → agent_thought_chunk`，不受影响）。

fork 改动：

- `src/acp/translate/pi-messages.ts`：新增 `normalizePiAssistantBlocks()`，把 assistant content 按顺序拆成 `{ kind: 'text' | 'thought', text }`；`normalizePiAssistantText()` 改为复用它的 text 部分。空块、`toolCall` 块、非数组 content 直接跳过。
- `src/acp/agent.ts`（`loadSession`）：按块顺序逐条发 `agent_thought_chunk` / `agent_message_chunk`，保持“先推理后回答”的消息顺序。
- 测试：`test/unit/pi-messages.test.ts`、`test/component/session-load-thinking.test.ts`。

GUI 侧不用改：`internal/session/events.go` 已解析 `agent_thought_chunk`，`ui.replayMessage` 把同角色连续 chunk 并进同一条消息，thought 仍显示在「Thinking」折叠块里。真实会话 `session/load` 实测：5 个 thinking 块（390 字符）全部回放，文本字节与改动前一致（无重复）。

### 历史回放里的图片

上游 `loadSession` 的 user 消息只取文本（`normalizePiMessageText`），而 pi 把用户粘贴的截图持久化成 `{ type: 'image', data, mimeType }` 块。恢复会话后这些图片全部消失。

fork 改动：

- `src/acp/translate/pi-messages.ts`：新增 `normalizePiUserBlocks()`，把 user content 按原顺序拆成 text / image 块；`normalizePiMessageText()` 复用它的 text 部分。
- `src/acp/agent.ts`（`loadSession`）：逐块发 `user_message_chunk`，图片块带 `mimeType` + base64 `data`，不再只发拼接后的文本。
- 测试：`test/unit/pi-messages.test.ts`、`test/component/session-load-images.test.ts`。

GUI 侧 `internal/session/events.go` 保留消息块的完整 `Content`，`internal/ui/attachments.go` 在 replay 时按 ACP image 块异步解码成缩略图，因此图片与文本顺序一致，且和 Command Code 的回放行为一致。

### ACP 状态文本

pi-acp 会自己生成一些状态文本，它们不是模型回答：

- startup prelude（pi 版本、Extensions、Skills、Prompts 等）仍通过 `agent_message_chunk` 发给不认识 `_meta` 的客户端，但带上 `_meta.piAcp.startupInfo: true`。
- 重试、自动压缩、排队、未支持的扩展 UI 请求统一走 `emitNotice()`，带 `_meta.piAcp.notice: { kind, level }`。`auto_retry` / `auto_compaction` / `queue` 的开始和结束使用同一 kind，客户端可原地更新一行。
- 扩展 `ctx.ui.notify` 继续带 `_meta.piAcp.notify.level`；GUI 也把它当系统提示。

GUI 侧丢弃 `startupInfo`（状态栏已有 pi 版本），把 notice 画成居中的系统行；notice 会关闭当前 assistant 行，后续文本另起一行，避免状态文字混进回答。这样 pi 的“Retrying…”和真正的回复不会粘在一起。

## 2. 本仓库 vendor 接入

`scripts/vendor-pi-acp.sh` 支持两个变量：

```bash
PI_ACP_PACKAGE="${PI_ACP_PACKAGE:-@geohar/pi-acp}"
PI_ACP_SOURCE="${PI_ACP_SOURCE:-$PI_ACP_PACKAGE@$VERSION}"
```

替换了所有硬编码处：`npm install`、包版本/名称校验、esbuild 入口、LICENSE 路径、bundle 旁的 `package.json`、`THIRD_PARTY_NOTICES.md`。另外：

- 输出先在同级 staging 目录生成，冒烟通过后才替换 `resources/agents/pi-acp/`；失败保留旧 bundle。
- 本地 tarball 不会把 mktemp 路径写进 NOTICE；可用 `PI_ACP_SOURCE_LABEL` 固定来源文本。

日常使用本地 fork 直接跑固定脚本：

```bash
scripts/vendor-pi-acp-local.sh                 # npm ci -> build -> pack -> vendor
PI_ACP_SKIP_INSTALL=1 scripts/vendor-pi-acp-local.sh  # 复用 node_modules
```

脚本默认读 `./pi-acp-main`，也可传目录或设 `PI_ACP_SRC`。手动等价流程：

```bash
(cd pi-acp-main && npm ci && npm run build && npm pack --pack-destination /tmp)
PI_ACP_SOURCE=/tmp/geohar-pi-acp-0.4.0.tgz \
  PI_ACP_SOURCE_LABEL='local fork (pi-acp-main)' \
  scripts/vendor-pi-acp.sh 0.4.0
```

发布到自己的 npm scope 后用 `PI_ACP_SOURCE=@yourscope/pi-acp@版本`。不要手改 `resources/agents/pi-acp/pi-acp.bundle.mjs`；bundle、`VERSION`、`package.json`、`SHA256SUMS` 都由脚本重新生成。

## 3. 验证

```bash
cd pi-acp-main
npm run typecheck
npm run lint
npm test
cd ..
node scripts/pi-acp-smoke.mjs
ACP_REAL_PI=1 go test ./internal/session -run TestRealPi -v -count=1
go test ./... -race
```

`TestRealPi` 会断言两件事：收到可用的 `usage_update`（`used > 0`、`size > 0`），且 `PromptResponse.usage.totalTokens > 0`。本机 pi 1.5.2 实测：

```text
context=1609/1000000 turn={TotalTokens:1609 InputTokens:1350 OutputTokens:3 CachedReadTokens:256}
```

真实会话中确认会话栏出现 `context x / y` 和 `turn … in · … out`，并检查 `logs/pi.log` 里没有协议错误。

## 4. 注意事项

- `$VERSION` 用于校验解析后的包版本；fork 版本号变化时同步修改参数或放宽脚本断言。
- 保持 `@agentclientprotocol/sdk@0.26.0`、`zod@3.25.76` 不变；升级 SDK 属于独立变更。
- `pi-acp-main` 缺少的 `LICENSE` 已从上游 0.4.0 补回；`.github` / `assets` 等发布用文件不需要。
- bundle 旁的 `package.json` 决定 `agentInfo.name/version`；包名改了会显示在新状态行/日志里，但 Agent 下拉仍是内置的 pi。
- 当前 `SessionManager.closeAllExcept` 让一个 ACP 进程只保留最新会话；如果未来要支持并发多会话，这是 fork 侧需要改的点（见 [multi-session.md](multi-session.md)）。
- 长期维护要 rebase 上游；若 usage / 推理强度映射是通用需求，优先给上游提 PR，届时可以直接回到 `scripts/vendor-pi-acp.sh` 的上游流程。
- 接入后同步更新 `docs/design/pi.md` 的版本表、sha256、来源和用量说明。
