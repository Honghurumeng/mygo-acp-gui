# Mermaid 图表

## 目标

对话回复和查看卡片里的 Markdown 代码块，` ```mermaid ` 直接画成图表，而不是停在源码。
纯 Go 渲染：不启动 Node、不内嵌浏览器、不调用 mermaid-cli；着色跟随当前主题。

实现见 `internal/ui/mermaid.go`，Markdown 钩子来自 MyGo 的 `markdown.Options.Code`。

## 依赖

- `github.com/momaek/streameraid`：flowchart / sequenceDiagram 的解析、布局和 SVG；对 subgraph、指向 subgraph
  的边和 `A -- 更新/写入 --> B` 这类旧式边标签支持更好。未打 tag，`go.mod` 固定 pseudo-version。
- `github.com/zkrebbekx/go-mermaid v0.1.6`：其他图表类型的解析、布局和 SVG。
- 两者都是纯 Go；只 import 核心/ SVG 包，不引入 go-mermaid 的 `raster`，二进制不会编入
  `oksvg`/`rasterx` 等 PNG 光栅化依赖。
- MyGo 的 SVG 查看器支持两者的输出（`Unsupported()` 为空），包括 `text`/`tspan`、`marker`、`path`。

## 接入点

`internal/ui/app.go` 每帧在 `View` 开头调用 `installMermaidHooks()`，给两个 Markdown 选项注册
`Code["mermaid"]`：

| 选项 | 使用处 | 标签字号 |
|---|---|---|
| `a.MD` | 对话消息、归档会话 | `chatBodyFont * 0.9` |
| `a.TaskMD` | 浮动查看卡片、任务总结 | `gitFont(theme.FontSize)` |

- 钩子是幂等的；`&App{}` 这类测试构造也能在首帧装上。
- `CodeBlockView.Open` 为真（代码块还在流式写入）时继续显示默认代码块，闭尾后再画图。
- 空内容、超过 256 KiB、未知图表类型、解析失败都回退到 `markdown.DefaultCodeBlock`，源码不会丢。
- 图片带无障碍标签 `T("Mermaid diagram")`（中文「Mermaid 图表」），测试也按这个标签查找。

## 图表类型

| 渲染器 | 类型 |
|---|---|
| streameraid | `flowchart` / `graph`（TD、TB、BT、LR、RL，含 subgraph、`classDef`、`style`）、`sequenceDiagram` |
| go-mermaid | classDiagram、stateDiagram-v2、erDiagram、pie、journey、quadrantChart、gitGraph、timeline、mindmap、gantt、C4Context / C4Container、requirementDiagram、sankey-beta、xychart-beta、block-beta、kanban、packet-beta、radar-beta |

flowchart / sequence 先走 streameraid；它解析失败时还会再试 go-mermaid。仍未识别或解析失败的语法显示源码；
渲染器自身对个别语法（如 sequence 的 `loop` 标签排版）不完美，属于上游限制，不在本仓库 fork。

## 尺寸

- 图片使用 SVG 的固有宽高，不放大；比 Markdown 列窄的图保持原尺寸，避免小图被拉成满屏。
- `Width(w).MaxWidthPercent(100).AspectRatio(w/h)`：宽图等比缩到列宽，高度随比例变化，外层滚动条负责纵向。
- 字号只影响布局和标签大小；同一份源码换字号会重新布局，不共用旧缓存。

## 放大预览与下载

对话流和查看卡片里的图本身仍是静态 SVG，不带控件；点击图（`CursorPointer` + `Click to view larger`）打开与附件共用的
图片预览 modal（`internal/ui/preview.go`），由 `previewSource` 决定显示附件位图还是图的 SVG。

- 打开时按窗口可用空间、图的固有尺寸等比缩放，上限 100%（小图不放大）；`Image preview` 标签在固定大小的视口盒子上。
- 滚轮以指针为锚点缩放：0.25×–6×，一格（约 40 DIP）约 10%，触控板捏合走 `InputZoom`，`Momentum` 事件只吞掉不缩放。
- 缩放不逐帧重画 SVG：`ImageView.Sharp` 按 1.25 的步进取值，并且不超过 MyGo 的 4096 设备像素上限（按 2× 屏折算成 2048 DIP），
  画面用 `Scale(Zoom/Sharp)` 显示到目标比例；连续滚轮只在跨过步进时重画，而不是每一帧，停下 180ms 后 `c.After` 排一帧
  按精确比例重画，既流畅又清晰。
  图放进固定大小、`Clip` 的盒子里，超出部分裁掉，`ImageView.X/Y` 负责摆放。
- 指针拖动画布平移（`Box.Dragged`，光标 `CursorGrab`/`CursorGrabbing`），拖到边缘停住；图小于盒子时保持居中。
- 从浮动查看卡片打开预览时，卡片在预览期间不构建（`ImageOpen` 时跳过 `fileViewerPopover` / `taskSummaryPopover` /
  `gitDiffPopover`）：popover 会把预览里的第一次按下当成「点击外部」关掉自己，并把点击透传到下面的卡片；关闭预览后卡片原样回来。
- 工具栏：`−`、当前百分比（点击重置为 100%）、`+`、`Download`、`Close`；状态存在 `App.ImageView`，关闭预览时和
  `DiagramPreview` 一起清空。
- `Download` 用 `mygo.Dialog.Save` 让用户选路径，写出的就是渲染器的原始 SVG（`mermaidDiagram.source`，随缓存的 `*mui.SVG`
  一起存），默认文件名 `diagram.svg`；保存结果（`Saved to …` / `Could not save: …`）显示在工具栏下方。
- 预览 modal 用 `mui.DialogBase` 加固定 `Key`：位置化的 overlay id 会被其他 overlay 的出现/消失带偏，
  键化 backdrop 后预览里的点击始终落在自己的按钮上。

## 主题

两个渲染器各自接收一套从当前 `mui.Theme` 派生的颜色，取值一致：

| 字段 | 取值 |
|---|---|
| `Background` | `t.Background`（透明时按深浅回退） |
| `NodeFill` | 背景与 `t.Accent` 10% 混合 |
| `NodeStroke` | 背景与 `t.Accent` 62% 混合 |
| `Text` | `t.Text` |
| `Edge` | 背景与 `t.Text` 62% 混合 |
| `ClusterFill` / `ClusterStroke` / `ClusterText` | streameraid 的 subgraph 颜色，按文字色与 `TextMuted` 派生 |
| `EdgeLabelBg` | 背景色，盖住穿过的边 |

- go-mermaid 的调色板存在包级 map 里，`Register` 和 `Render` 由 `mermaidRenderMu` 串行化，避免多个
  App/测试同时渲染时换错颜色。
- streameraid 的 `Theme` 是每次渲染传入的值，不共享全局状态。
- 主题、深浅色或字号变化都会改变缓存 key，下一帧自然重画；图内文字颜色跟随主题，不会出现浅色图配深色底。

## 缓存

- 每次渲染 Markdown 都会重建视图，所以按「渲染器 + 源码 + 字号 + 颜色」的 SHA-256 缓存解析后的 `*mui.SVG`。
- 上限 32 份，FIFO 淘汰；只缓存在 `App.mermaidCache`，不落盘，退出即释放。
- 首次出现某张图时同步解析；两个渲染器解析几十个节点的图都很快，只有缓存未命中才付出一次成本。

## 非目标与后续

- 图内文字不可选中；缩放、下载只在图片预览 modal 里，对话流里没有悬浮按钮，也没有拖拽平移（缩放锚在指针上）。
- 不实现 mermaid.js 的交互、`click` 动作或 `%%{init}%%` 的全部配置。
- 若以后要流式逐节点绘制，可换成原生 MyGo 绘制库；当前静态 SVG 方案优先覆盖图表类型和布局正确性。

## 测试

```bash
go test ./internal/ui -run 'TestFileViewerRendersMermaid|TestFileViewerFallsBackToMermaidSource|TestMermaidDiagramOpensThePreview|TestPreviewSourceDownloadsAnAttachment' -v -count=1
go test ./internal/i18n -v
```

覆盖：查看卡片中 Mermaid 画成图而不是源码、带 `/` 的旧式边标签、classDiagram 走 go-mermaid、未知类型回退源码、
点击打开预览（滚轮缩放按步进重画、停下后变清晰、拖动平移、百分比重置、Escape 关闭、SVG 可下载）、
预览不把点击透传给下面的查看卡片、附件预览的下载字节、i18n 词典完整性。
