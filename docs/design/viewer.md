# 浮动文件查看 / 编辑卡片

本文记录 Git、Tasks、Files 三个右侧面板共用的浮动卡片：Diff / View / Edit 三种模式、Markdown 渲染、
可调宽度和安全边界。实现见 `internal/ui/viewer.go`、`internal/ui/gitpanel.go`、`internal/ui/tasks.go`、
`internal/ui/filepanel.go` 和 `internal/files/text.go`。

## 目标与入口

一张卡片覆盖三种查看场景，右侧面板保持可见、可继续操作：

- Git：点 Staged/Changes 文件名或展开提交后的文件行打开。Staged 与提交历史只读；未暂存的工作区文件
  额外提供 Edit，保存后写回工作区。
- Tasks：点 complete 任务行或 `View` 打开总结，支持 Markdown、源码编辑和 all-additions diff。
- Files：点文件行打开文件内容；目录行仍然只展开/折叠。保存后刷新 Files 与 Git 状态。

卡片是 `ui.PopoverBase` 的浮层，右缘贴对应面板左缘（`AttachTo(anchor, AnchorStart, AnchorEnd)` +
`MarginEnd(10)`，anchor 是面板整列），垂直居中，高 86%（任务总结 80%），圆角、边框和阴影。
点卡片外、按 Esc 或标题栏 `✕` 关闭；关闭右栏、切走 tab、切换 workspace 时由各自面板立即丢弃。

## 三种模式

标题栏的模式按钮由调用方给出顺序，卡片只负责显示和回调：

| 模式 | 内容 | 默认入口 |
|---|---|---|
| Diff | `editor.DiffView`，对比 old/new 两份完整文本 | Git、任务总结 |
| View | Markdown 文件用 `markdown.View` 渲染；其他文件用只读 `editor.View` | 全部 |
| Edit | 同一个 `editor.State` 切到可编辑，标题栏出现 `Save` | 有可写文件的入口 |

Markdown 里的 ` ```mermaid ` 代码块在 View 模式画成图（静态 SVG，见 [mermaid.md](mermaid.md)，点击可在图片预览里放大、
拖动和下载；预览期间卡片本身隐藏，不接收预览的点击，关闭后回来）；流式写入未闭尾、未知类型或解析失败时仍显示源码。

- 当前模式按钮用 Accent 浅底 + Accent 文字；点击其他模式切换，状态由调用方持有。
- Git 的 View/Edit 标题为 `File · <path>` / `Edit · <path>`；Staged 的 diff 仍为 `Staged diff · <path>`，
  提交预览仍为 `Commit <7 位 hash> · <path>`。
- Git 只有未暂存、非删除、非冲突的工作区文件可编辑；暂存区（index）和提交历史不能改写。
- 任务总结把整个 Markdown 当 new、old 为空，所以 Diff 模式显示全为新增行。
- Files 的 Markdown 文件默认渲染；切到 Edit 编辑其源码，保存后回到查看仍显示渲染结果。

## 宽度

- 默认宽度：`clamp((windowW - rightPaneWidth) * 0.62, 480, 900)`，再按
  `windowW - rightPaneWidth - 38 - 24` 的上限收窄，保证对话和右侧面板仍然可见；
  `rightPaneWidth` 是右侧列当前宽度（默认 300 DIP，可拖动，见 [layout.md](layout.md)）。
- 卡片左缘有 6 DIP 的拖动热区（`CursorResizeEW`）：向左拖变宽、向右拖变窄，范围为 420 DIP 到当前窗口的
  可用宽度。
- 拖动结束后把宽度写入 `settings.json` 的 `viewerWidth`；重启后恢复，仍受窗口大小限制。
  没有保存值时继续使用响应式默认宽度。
- 宽度跨 Git / Tasks / Files 共用；切换 700 DIP 阈值时 diff 在 Split / Unified 之间切换，保留同一个
  `editor.DiffState`，滚动位置不丢。

## 状态、保存与安全

- 每个文件/任务在卡片内有一份 `viewerCardState`：共享的编辑 buffer、diff state、内容来源文本和字号。
  文档 key 变化时重建；同一文档切换 View/Edit 保留滚动位置和未保存文字。
- 标题栏下方显示 `Saving…`、保存错误或 `Saved.`；有未保存修改时副标题追加 `unsaved changes` 并改用
  Warning 色。View / Diff 模式下若仍有未保存修改，`Save` 按钮继续显示。
- `Save` 或 `⌘S`（非 macOS 为 Ctrl+S）把编辑 buffer 交给调用方；保存都在 goroutine 中执行，结果通过
  `App.Post` 回到 UI 线程，并用 generation + workspace + path 守卫。
- 写文件统一走 `internal/files.SaveText`：拒绝越界路径、目录、符号链接、二进制和超过 512 KiB 的文本；
  保留原文件权限，在同目录写临时文件后 `rename`，失败不破坏原文件；文件已被删除时不会重新创建。
- 读取走 `internal/files.ReadText`，同样限制 512 KiB、NUL 字节和符号链接，避免跟随链接走出 workspace。
- Git 预览的保存只允许工作区一侧；任务总结保存只接受 `tasks/` 下由任务记录解析出的相对路径。
- 保存后：Git tab 可见时自动重读 status/图；Files tab 可见时重读文件树和状态；隐藏的 tab 只标记 stale，
  下次打开再读。
- 加载中显示 `busyLine`，读取失败显示 `dangerAlert`，Diff 两边相同显示 `emptyState`；View 的空 Markdown
  显示空状态，空文本文件仍打开空编辑器。

## 测试

```bash
go test ./internal/files -v
go test ./internal/ui -run 'TestFileViewer|TestGitPanelEditsWorkingTreeFile|TestTaskRunnerChecksContinuesAndCommits|TestViewer' -v -count=1
```

覆盖：文本读写/权限保留/临时文件清理、二进制/超大/目录/越界/符号链接拒绝、Files 打开与 Markdown 渲染、
Mermaid 画图与失败回退、任务总结编辑保存、Git 未暂存编辑保存、暂存与历史只读、⌘S/Ctrl+S 保存、宽度响应式与拖动 clamp、
settings 持久化。
