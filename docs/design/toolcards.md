# 对话流工具卡片

本文记录对话流里一次工具调用（ACP `tool_call` / `tool_call_update`）的展示：解析管道、按 `kind`
分派的卡片形态、折叠与展开约定、截断与安全、i18n 与测试。实现见 `internal/ui/toolview.go`（解析）、
`internal/ui/toolcards.go`（渲染）、`internal/ui/session.go`（建立工具消息）、
`internal/ui/components.go`（`codeBlock` / `codeLines`）和 `internal/ui/text.go`（ANSI 与控制字符）。

## 目标

- 折叠态一行说清「做了什么、对谁做的、结果怎样」；展开态给出该工具最有用的内容，不再把 payload
  拍平成字符串。
- 按 ACP `kind`（`read` / `edit` / `delete` / `move` / `search` / `execute` / `think` / `fetch` /
  `switch_mode` / `other`）分派渲染；未知 kind 安全兜底，不丢内容。
- 只消费 agent 已经发出的字段：`kind`、`title`、`status`、`locations`、`content`（含 `diff` 块）、
  `rawInput`、`rawOutput`。
- 非目标：不代执行工具、不内嵌终端、不读工作区文件补 diff（客户端能力保持 `fs: false`、`terminal: false`）。

## 背景：diff 曾经整体丢失

改版前 `Message` 只有 `Title` / `Status` / `Detail`，`toolDetail` 把内容拍平成
`rawText(content)` → `rawText(rawOutput)` → `compactJSON(rawInput)`，并静默截断到 2000 / 240 字符。
pi-acp 与 codex-acp 对 edit/write 发的是标准 ACP `diff` content 块，`rawText` 只认 `text` / `content`，
于是 diff 返回空，卡片展开后显示按键名排序、`\n` 被转义的 `rawInput` JSON，240 字符即断。
`session.ToolCall.Kind` 与 `Locations` 虽然解析了，UI 也没用。

现在 diff 由 `toolview.go` 解析成结构化 `ToolDiff`，卡片内联 `editor.DiffView`；文本兜底仍保留。

## 数据与解析

`Message` 增加结构化工具视图，`Detail` 保留为纯文本兜底：

```go
type Message struct {
	// ... 既有字段
	ToolKind    string     // ACP kind；空表示老 agent
	ToolPaths   []string   // locations 里的路径
	ToolDiffs   []ToolDiff // edit/write/delete/move
	ToolExit    *int       // execute 的退出码
	ToolCwd     string     // execute 的 cwd（tooltip）
	ToolOffset  int        // read 的起始行
	ToolLimit   int        // read 的行数
	ToolPattern string     // search 的高亮子串
	ToolURL     string     // fetch 的链接
	ToolStart   time.Time  // 首个 update
	ToolEnd     time.Time  // 终态 update
}

type ToolDiff struct {
	Path, OldPath string // OldPath 仅 move
	Old, New      string // 允许空侧
	Operation     string // add / update / delete / move
	Partial       bool   // 只覆盖文件的一段，行号不可信
}
```

- `parseToolCall(call)` 是纯函数：解析成功返回 `toolCallView`，失败保留 `Detail`，绝不丢内容。
- diff 解析顺序：`content[]` 里 `type == "diff"` 的块 → `rawInput.oldText/newText`（pi 流式阶段、
  旧载荷）→ `rawInput.edits`（数组或 JSON 字符串，pi 的 `edits[]`、也认 `old_string/new_string`）→
  write 的 `rawInput.content` 合成全新增 → 都没有时退回文本。
- `oldText` 允许为 `null`（codex 新增文件），按空侧处理；操作从 `_meta.kind` 或空侧推断。
- **一个调用可以带多个 diff 块**：pi 一次一个整文件块；codex 的 `apply_patch` 每个文件一块，同一文件
  按 hunk 分多块（`_meta.kind = add|update|delete`，`oldText` 是该 hunk 的旧文本）。按 `path` 分组：
  单块按整文件渲染并显示行号；同一文件多块、或 `_meta.kind = update` 的块按 hunk 处理
  （`Partial`、隐藏行号、`ContextLines: -1`），不伪造跨块行号。
- `_meta.kind` 是 codex 私有字段，只用于增强，缺失按 `update` 处理。
- 文档文本统一 `stripANSI` + 控制字符清理（保留 `\n` / `\t`），文本上限 256 KiB（截断并加省略号）；
  diff 两侧不截断，交给渲染层控制高度。
- 退出码：优先 `rawOutput.exit_code` / `exitCode`（codex），其次正文尾部的 `exit code: N`（pi-acp
  在非 terminal 客户端上拼接），解析后从正文剥离。
- `rawInput` JSON 只在它是工具可读内容时兜底（`other` 等）；`execute` / `read` / `search` / `fetch` /
  `think` 的参数已由标题和 header 表达，不再显示 JSON。
- 老 agent：`Kind == ""` 时按标题动词与 `rawInput` 保守推断，推断不了归 `other`。
- `applyToolPresentation` 只在新更新确实带了数据时覆盖字段：状态-only 的后续更新（codex 完成时只发
  `status`）不会清掉 diff；diff 到达时清掉先前展示的 rawInput JSON。

## 卡片骨架

所有 kind 共用一行 header，折叠时只有这一行：

`[▸] [图标/verb] [目标] [统计] … [耗时] [exit/状态]`

- 图标沿用 `icons.go` 现有集合：`terminalIcon`（execute）、`codeIcon`（edit）、`filePlusIcon`（新增）、
  `folderTreeIcon`（search）、`sparklesIcon`（think）、`gitCompareIcon`（move），read 用
  `fileIconFor(basename)` 的类型图标。不新增 SVG。
- 目标：路径（basename 加粗、目录 muted）、`$ 命令`（等宽）、search 标题、fetch 主机名。
- 统计：edit 的 `+N`（Success）与 `−M`（Danger），search 的 `N matches in M files`（单数单独处理）。
- 耗时：`ToolStart` 到 `ToolEnd`，≥100ms 才显示；到终态后停止计时。
- 状态：保留 `toolStatusLabel` 文案（测试与无障碍依赖）。`failed` 用 `t.Danger`，`in_progress` 用
  `Warning`；`execute` 有 exit code 时用 `exit N` 徽章代替状态词，非 0 一律 Danger。
- 动作放在展开后的面板里，不放 header：Copy（diff patch / 命令 / JSON）与 `Open file`。header 整行是
  折叠开关，嵌套可点击元素会让一次点击同时触发两个动作。
- 可展开判定是「有 diff 或文本」：两者都没有时画一行紧凑事件（编排调用 `wait_agent` 等），
  exit code 仍显示在这一行的状态位。
- 子代理工具用同一渲染器，`SubagentID` 标注与 `SubagentName` 前缀不变。

## 按 kind 的形态

| kind | 折叠态 | 展开态 |
|---|---|---|
| `edit` / `delete` / `move` | 图标 + verb + 路径 + `+N −M` | `editor.DiffView` + `FitContent`：unified、`DiffInlineWordAlt`、只读、按路径上语言 |
| write（新增） | `filePlusIcon` + 路径 + `+N` | old 空侧的全绿 diff（识别靠 `oldText: null` / `_meta.kind: add`） |
| `execute` | `$ 命令`（等宽）+ exit 徽章 | 等宽输出块（尾部 200 行），Copy 命令 |
| `read` | 类型图标 + `Read` + 路径 + 行范围 | `codeLines` 高亮 + 行号（从 offset 起），尾部 300 行，`Open file` |
| `search` | `Search` + 标题 + 命中统计 | 等宽结果，pattern 高亮，尾部 200 行 |
| `think` | `sparklesIcon` + 标题（muted，无卡片表面） | 完整文本 |
| `fetch` | `Fetch` + 主机名 | 完整 URL（`mui.Link`）+ 摘要文本 |
| `switch_mode` | 一行事件 | 无 |
| `other`（含 MCP） | 标题 | JSON 高亮（`codeLines`）或 muted 文本，带 Copy |

### edit / write / delete / move

- diff 内联在卡片里，不跳查看器；`Open file` 打开的是文件当前内容（Files 面板的查看器），不是历史版本。
- `editor.DiffState` 缓存为 `Message` 的非导出字段（diffs 或字体变化时重建），构造与 `viewerDiff`
  （viewer.go:292）一致：`Wrap: true`、`LineHeight: 1.4`、`Font: {Size: chatFont(11)}`、
  `editor.LanguageFor(path)`。
- 整文件块：`ContextLines: 3`、显示行号；hunk 块：`ContextLines: -1`、`NoLineNumbers: true`。
- 高度：总行数 ≤ 400 时 `FitContent`；更大的 diff 固定 280 DIP 高、在卡片内滚动。
- 多文件 patch 每个文件先画一行文件头（图标 + 路径），再画自己的 diff；`Open file` 暂时只开第一个文件。
- 移动显示 `旧路径 → 新路径`（`Operation = move`），两侧语言按新路径；删除的 new 为空。
- Copy 生成 `--- path` / `+++ path` 加 `-old` / `+new` 的简单 patch（按 hunk 边界分段）。

### execute

- 命令来自 `title`（pi-acp 直接发命令，codex 发 `stripShellPrefix(command)`），等宽、单行省略；
  tooltip 给完整命令，`rawInput.cwd` 存在时第二行显示。
- 输出先剥 ANSI 与控制字符；等宽、换行、可选中，画在窗口背景的圆角块里。
- 长输出默认显示尾部 200 行（终端最有用的一段），上方 `Show N earlier lines` 展开全文、
  `Show less` 收起；展开后保留全文（≤256 KiB）。
- 不合并连续 bash 调用：命令与输出一一对应更重要。

### read

- 路径取 `locations[0].path`，缺失时从标题取；行范围来自 `rawInput.offset/limit`，无参数时不显示。
- 展开用 `codeLinesFrom`（components.go）：行号从 offset 起，语法高亮、换行；默认只渲染尾部 300 行，
  用同一组展开/收起按钮。
- codex 的 read 只有标题没有正文：卡片退化成一行紧凑事件，不产生空折叠面板。

### search

- 标题原样显示（pi 的 `grep "x" in path`、`find "glob" in path`、`ls path`；codex 的 `searchTitle`）。
- 统计按 `path:line:` 行分组计数；二进制或非标准输出解析不了就不显示统计，不猜。
- 展开：等宽结果，命中行里 pattern 的每处出现都用 `Warning` 淡底高亮（纯子串，不执行正则）。
- `ls` / `find` 结果暂时仍按等宽文本显示（文件行图标未接）。

### think

- codex 把 reasoning item 发成 `kind: "think"` 的工具调用（含 `Guardian Review`）。它是思考，不是动作。
- 不画卡片表面：一行 `sparklesIcon` + muted 文本，有正文时可展开，与 assistant 的 `Thinking`
  collapsible 视觉一致；仍保持独立消息，顺序由 ACP 事件决定。

### fetch

- 主机名来自 `rawInput.url` 或标题里的第一个 URL；header 只显示主机。
- 展开给完整 URL（`mui.Link`，由 MyGo 校验并打开浏览器）与返回文本。

### other / MCP

- MCP 标题形如 `MCP <tool> on <server>`，暂时原样显示，未拆 chip。
- 正文是 JSON 时用 `codeLines` 高亮 + Copy（全文，不做 `compactJSON` 的 240 字符截断）；否则 muted 文本。

## 截断与安全

- 文本在解析时一次截到 256 KiB；显示层再按工具收口：命令/搜索尾部 200 行、read 尾部 300 行、
  diff 400 行、平铺文本 2000 rune。截断都可展开（`Show N earlier lines` / `Show all` / `Show less`），
  展开后是全文。
- 所有进入 UI 的文本先剥 ANSI 与控制字符；工具输出按字面显示，不做 Markdown 解释。
- diff 只来自 ACP `content` / `rawInput`，**不读工作区文件**：`fs: false` 不因渲染改变。
- `Open file` 走 Files 面板既有的 `internal/files.ReadText`（512 KiB、越界与符号链接校验），且路径
  必须落在当前 workspace 内；越界时只显示 Copy，不显示按钮。

## i18n

- 新增动词与文案：`Edited` / `Created` / `Deleted` / `Moved` / `Read` / `Search` / `Fetch` /
  `exit %d` / `Show %d earlier lines` / `Show all` / `Show less` / `Open file` /
  `1 match in 1 file` / `1 match in %d files` / `%d matches in 1 file` / `%d matches in %d files`。
- 走 `T` / `Tf`，`go test ./internal/i18n` 校验字面量与动词顺序；`Copy` 复用既有 key 与 `copyButton`。
- 状态文案（`Pending` / `In progress` / `Completed` / `Failed` / `Cancelled`）不变，测试与无障碍名稳定。

## 兼容与回放

- `session/load` 重放走同一 ACP 事件，diff 与 kind 会重新解析；`sessions.json` 不涉及工具消息结构。
- 老 agent（无 `kind`、无 `diff`）全部走兜底，不出现空卡片；只有 `rawInput` 的 edit 也能重建 diff。
- 协议面不变：客户端能力、请求/响应形状、权限流程都不动。

## 测试

```bash
go test ./internal/ui -run 'TestParseToolCall|TestApplyToolPresentation|TestToolCard|TestEditToolCard|TestExecuteToolCard|TestReadAndSearch|TestThinkFetch' -v
go test ./internal/i18n
```

- 解析：ACP diff 块（整文件、新增/删除、多 hunk、多文件）、`rawInput` 与 `edits` 兜底、退出码两种来源、
  ANSI/控制字符、read 范围、search 统计、fetch URL、未知 kind、`Detail` 兜底。
- UI（`mui.Tester`）：edit 折叠不出现 diff 正文与 JSON、展开后可展开；execute 显示命令、exit 徽章与输出；
  read 显示 `Read`/路径/行范围；search 显示命中统计；think 只有一行；fetch 显示主机与链接；结构化 JSON
  展开后可读；`Open file` 切到 Files 并打开查看器；超长平铺文本可 `Show all`；稀疏/异常载荷不 panic。
  回归覆盖折叠开关、`chatMaxWidth`、编排调用不暴露传输 JSON。

## 已知取舍与后续

- header 的 Copy/Open 放在展开面板里（避免嵌套点击冲突）；悬停显示的紧凑动作未做。
- 多文件 patch 的 `Open file` 只开第一个文件；MCP 标题未拆 `server · tool` chip；hunk 数量未标注。
- `ls` / `find` 结果未用文件图标；`think` 与 assistant 的 Thinking 仍是两套 collapsible。
- search 统计每帧重算，输出很大时可加缓存。
