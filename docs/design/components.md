# UI 组件层

## 目标

界面全部由 MyGo 自绘，不依赖第三方 UI 库。通用外观分两层放在 `internal/ui`：

- `widgets.go`：无状态样式函数，只抽颜色、尺寸、圆角等重复写法。
- `components.go`：带跨帧状态或动画的组件，才用 `mui.Local`、`mui.Key`、`mui.Keyframes`。

`go.mod` 只有 MyGo 一个直接依赖；图标是 `internal/ui/icons/` 里的 SVG 子集，没有图标库。

## 图标

`internal/ui/icons.go` 用 `//go:embed` + `mui.MustParseSVG` 嵌入，方式与 `agenticons.go` 一致。
只加当前界面用到的图标，避免又长成一个图标库：

| 变量 | 来源 | 用途 |
|---|---|---|
| `messageSquareIcon` | Lucide `message-square` | 空状态：新建/无活动会话 |
| `messagesSquareIcon` | Lucide `messages-square` | 空状态：已连接 |
| `unplugIcon` | Lucide `unplug` | 空状态：没有可用 agent |
| `sparklesIcon` | Lucide `sparkles` | 空状态：开始对话 |
| `gitCompareIcon` | Lucide `git-compare` | Git diff：两边相同 |
| `circleXIcon` | Lucide `circle-x` | danger alert |
| `codeIcon` | Lucide `code` | code block 标题栏 |
| `copyIcon` / `checkIcon` | Lucide `copy`、`check` | code block 复制/已复制 |
| `settingsIcon` | Lucide `settings` | 侧栏左下角设置按钮 |

- SVG 用 `stroke="currentColor"`，由 `mui.Icon(...).TextColor(...)` 跟随主题，深浅色不用分支。
- 新增图标时同时改 `icons/LICENSE`（Lucide ISC；部分图标来自 Feather，MIT）。
- `emptyStateCopy` 直接返回 `*mui.SVG`，不再用字符串图标名，拼错名会编译失败而不是运行时 panic。

## widgets.go

界面里反复出现的「小样式」一处定义、处处调用（MyGo 没有样式表，组件就是给元素设好样式的 Go 函数）：

- `mutedText(c, text, size)`：次要文字的基元，颜色取 `c.Theme().TextMuted`；返回元素，调用方继续
  `.Selectable()` / `.Grow()` / `.Tooltip()`。
- `caption(c, text)`：9px 单行次要文字，用于计数、状态、微提示。
- `sectionLabel(c, text)`：9px 加粗组标题；padding、Grow、旁边的数量由调用方叠加。
- `surfaceCard(c, e)`：给任意元素加 8 DIP 圆角与 `t.Surface` 背景；Gap/Padding/方向仍由调用方决定，
  用于工具调用卡片和附件行。
- `buttonBase(c, label)`：所有按钮的共同形状，取自侧栏「⋯」按钮：8 DIP 圆角、`Padding(4, 6)`、无边框；
  只设 label 与几何，文字和填充由下面三个变体加。
- `quietButton(c, label)`：默认按钮（「⋯」那种扁平胶囊）：`t.Background` 填充、hover/pressed 变色、无边框；
  `Cancel`、`Refresh`、菜单/弹窗里的普通动作都用它。
- `primaryButton(c, label)`：主操作，同一形状但用 `t.Accent` 填充；窗口非激活时退回 `quietButton` 的灰底，
  与 MyGo 自带 `PrimaryButton` 行为一致。`Send`、`Save`、`Apply`、`Commit` 用它。
- `dangerButton(c, label)`：破坏性操作，`quietButton` 形状 + `t.Danger` 文字颜色（`Delete`、`Stop`、拒绝授权）。
- `chipBase(c)`：composer 的模型/模式/用量/分支胶囊，与按钮同形状、无边框，padding 收紧为 `3, 8`。
- `menuTrigger(c, label, background)`：行尾「⋯」菜单按钮；label 供无障碍与 `Tester` 使用，背景跟随所在行。

约定：

- 只抽颜色、尺寸、圆角这类会被复制粘贴的样式，不抽业务状态和布局。函数返回 `mui.Element`，调用方
  继续链式覆盖，后写的生效（等价于外部覆盖 class）。
- 这些样式函数保持无状态，不用 `mui.Local`；有跨帧状态的组件放 `components.go`。
- `caption` 强制 `SingleLine`，可能很长、需要换行的文本用 `mutedText`。
- 可交互组件保留 label，`Tester` 断言（`tt.Click("Allow once")` 一类）不依赖具体样式。
- 项目里不再直接调用 `mui.Button` / `mui.PrimaryButton`；新增按钮一律走上面三个变体，需要更宽的点击区或
  字号时在返回值上叠加 `.Padding` / `.FontSize`。ButtonBase 的列表行（会话、存档、workspace 标题、git 文件行）
  不是按钮外观，保留各自的选中/hover 背景。

## components.go

- `shade(a)`：`mui.RGBA(0, 0, 0, a)`，遮罩和阴影用；不引入动画工具包。
- `arrive(e, from)`：一次 `Keyframes`（200 ms，`EaseOut`）把 `from` 的位移/缩放/透明度收到 0，复刻入场动画。
  只做一次入场，不做退出。
- `centered(c, content)`：`Column.Fill().Center()` 包住内容并 `arrive` 缩放淡入。
- `busyLine(c, text)`：`Spinner` + 12px muted 文本，`RoleStatus` + `Label(text)`；连接、探测、加载统一用它。
- `emptyState(c, icon, title, text)`：居中列 + 圆形 tint 背景 + 图标 + `Bold` 标题 + muted 正文，`MaxWidth`
  取 `t.Space(100)`；整块是一个 `RoleStatus`，`Label(title)`。没有 action 参数，当前也不需要。
- `dangerAlert(c, title, body)`：danger 色 8% 背景 + 1 DIP 30% 边框 + `circle-x` 图标 + 正文按 20% 混入 danger；
  上方滑入。只实现 danger 一种色调。
- `codeBlock(c, lang, code, title)`：权限弹窗的工具入参。
  - 容器：`Column` + `Clip` + `Radius(t.Radius)` + `Surface` 背景 + 1 DIP 边框，`RoleGroup` + `Label(title)`。
  - 标题栏：`code` 图标 + 等宽加粗标题（`Input`）+ `copyButton`；标题文字在 description 之后，测试依赖顺序。
  - 代码体：`editor.LanguageNamed(lang)` 取 tokenizer，`editor.DefaultTheme(c.Theme())` 取颜色，逐行
    `Tokenize` 后转成 `mui.Span` 用 `RichText` 画；等宽 `t.Rem(0.8)`、行高 1.6、带行号、长行 `Wrap`。
  - 复制：`c.WriteClipboard(code)`，`mui.Local` 记 2 秒「已复制」，期间图标换成 `check`；不播声音。
- 所有组件从 `c.Theme()` 取色，不硬编码颜色，也不判断深浅色。唯一例外是阴影的黑色 alpha；主题目录和外观切换见 [themes.md](themes.md)。

## 使用位置

- 空状态与连接/探测中：`app.go`，`busyLine` + `emptyState`。
- Git diff 卡片：`gitpanel.go`，加载中 `busyLine`、错误 `dangerAlert`、两边相同 `emptyState`。
- 权限弹窗：`permission.go`，`codeBlock` + `arrive`/`shade` 的遮罩与面板。

## 不替换

- 消息渲染：角色、thinking、tool call、附件、plan/usage 都是 `App.message` 自己的渲染器。
- 侧栏与 composer：多会话、存档、拖拽高度、分支芯片、模型弹层。
- `mui.DialogBase`、`editor.DiffView`、`internal/ui/gitpanel.go` 的 popover、`internal/ui/gitgraph.go`
  的自绘提交列表：它们已经贴合数据流，换成通用组件只会丢功能。
  Git 历史是自绘列表（行高 22 DIP、整行可点、点击展开文件、lane 用 `DrawOver` 画），不是通用提交图组件：
  后者行高固定、行不可点，没法缩间距或插入文件列表。

## 交互约定

- diff 卡片直接用 `editor.DiffView`（`editor.DiffState`）：它收两份完整文本（`editor.NewBuffer`），不是 patch；
  由 `internal/git.Diff` 负责取 old/new，组件只画。切换文件时重建 `editor.DiffState`，同一份文本不重建。
  `DiffState` 支持 `Wrap`、`Font`、`LineHeight`；卡片宽时 `DiffSplit`，窄时 `DiffUnified`，`Wrap` 始终开启。
- 不做组件音效：`mygo/audio` 只为几处点击声引入并不值得，交互反馈靠 hover/pressed 和图标变化。
- 可交互组件通过 `.Label()` 暴露标签，`tt.Click("Allow once")` 这类按标签点击仍然有效。
- 事件返回值与 ACP 语义解耦：先拿到点击/变更，再调用 `finishPermission` 等现有函数。
- `ui.DialogBase` 在点遮罩或 Esc 时会把 `*open` 置 false 并标记 `Dismissed()`；权限弹窗传入只存在于当前帧的
  `open`，所以这些关闭动作被吞掉。只有 `finishPermission`（选项或 `Cancel`）会结束请求，
  不要复用 `a.PermissionOpen` 指针。
- `showNextPermission` 只取出属于当前会话的请求；其他会话的请求留在 `permissionQueue`，切走时把可见请求放回
  队首并隐藏弹窗，切回时再显示。后台请求不能抢焦点或自动切换会话。
- 组件对必填参数有约定（标题、code 非空；`codeBlock` 的 title 固定传 `Input`）。列表数据要先补齐，不要塞空值。

## 验证

```sh
go build ./... && go vet ./... && go tool mygo vet . && go test ./...
go test ./internal/ui -count=5
```

再跑一次真实界面：空状态四种、权限弹窗（遮罩/Esc 不关闭、`Input` 在 description 之后、复制）、
Git diff 四态（加载、错误、两边相同、正常），每个外观和配色各看一遍（组件按 `c.Theme()` 取色，主题目录见 [themes.md](themes.md)）。
