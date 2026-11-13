# UI 组件库（CrychicUI）

## 目标

界面继续由 MyGo 自绘；通用外观（弹窗、按钮、空状态、代码块）改用 [CrychicUI](https://github.com/ELY-Open-Source/CrychicUI)，避免每个面板手写 padding/圆角/hover。业务状态、ACP 数据流和 `internal/ui` 的渲染器不变。

## 依赖与版本

```go
require (
	github.com/ELY-Open-Source/CrychicUI v0.0.0-20261110123803-1c55e6a884bb
	github.com/egoist/mygo v0.8.3
)
```

- CrychicUI 要求 MyGo v0.8.3，因此 MyGo 从 `v0.7.1-0.2026…` 升级到 `v0.8.3`；升级后 `go build`、`go tool mygo vet`、`go test ./...` 全绿，现有手写 `mui` 代码无需改动。
- 仓库没有 tag，只有伪版本。`go get` 会解析到最新 commit，**必须**把 `go.mod` 里的伪版本当成锁：升级时手动指定 commit，跑完检查再提交。
- 不 import `diagram`（pele-go，约 +6 MB）；其余包按需 import，未用到的不会链接。

## 别名约定

`internal/ui` 里 CrychicUI 的包统一加 `cry` 前缀，避免与 `internal/agent`、`internal/session` 以及局部变量（`chat := a.activeChat()`）重名：

```go
crycode    "github.com/ELY-Open-Source/CrychicUI/code"
cryfeedback "github.com/ELY-Open-Source/CrychicUI/feedback"
crykit      "github.com/ELY-Open-Source/CrychicUI/kit"
crylayout  "github.com/ELY-Open-Source/CrychicUI/layout"
cryoverlay "github.com/ELY-Open-Source/CrychicUI/overlay"
```

## 已替换

- 权限弹窗 `internal/ui/permission.go`：MyGo `ui.DialogBase` + `crykit.Arrive`/`crykit.Shade` 复刻 CrychicUI 模态外观
  （遮罩、宽度、圆角、阴影、入场动画），不用 `cryoverlay.Modal`：后者点遮罩或 Esc 会关闭并让请求回 `cancelled`，
  而权限请求必须由弹窗内的按钮作答。内容用 `crycode.CodeBlock`（工具入参）+ 按 `option.kind` 选按钮
  （`allow*` → `primaryButton`，`reject*` → `dangerButton`，其余与 `Cancel` → `quietButton`）。
  ACP 的 option 列表原样保留，不压缩成 Allow/Deny 两键；按钮改用项目统一样式后不再有 `crybutton` 的音效。
- 空状态 `internal/ui/app.go`：`crylayout.Centered` + `cryfeedback.EmptyState`；连接/探测中用 `cryfeedback.Busy` 转圈。
  图标必须是 Lucide 里存在的名字，否则 `icon.Must` 会 panic。
- Git diff 卡片 `internal/ui/gitpanel.go`：`ui.PopoverBase` 定位在 Git 面板左侧，内容直接用 MyGo `editor.DiffView`
  （不再用 `crycode.DiffView`，因为它只收固定高度、不能设字号和换行），加载/错误/无变化用
  `cryfeedback.Busy` / `Alert` / `EmptyState`。
## 自有组件层

CrychicUI 只覆盖通用外观；界面里反复出现的「小样式」放在 `internal/ui/widgets.go`，一处定义、处处调用（MyGo
没有样式表，组件就是给元素设好样式的 Go 函数）：

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

Git 历史不是 CrychicUI 的 `code.CommitGraph`，而是 `internal/ui/gitgraph.go` 的自绘列表：行高 22 DIP、整行可点、
点击展开该提交改动的文件（`git show --name-status`，异步读）。CrychicUI 的组件行高固定 36 DIP 且行不可点，
没法缩间距或插入文件列表；lane 数据仍来自 `internal/git.assignLanes`（0..7、父提交在子提交之后），只有 ref 保留
第一个并截到 12 字。

按钮约定：项目里不再直接调用 `mui.Button` / `mui.PrimaryButton` / `crybutton.*`；新增按钮一律走上面三个变体，
需要更宽的点击区或字号时在返回值上叠加 `.Padding` / `.FontSize`，不要另起一套背景和边框。ButtonBase 的列表行
（会话、存档、workspace 标题、git 文件行）不是按钮外观，保留各自的选中/hover 背景。

权限弹窗里原本由 `crybutton` 播的点击音效随之去掉；CrychicUI 的其它组件（`crycode`、`cryfeedback`）仍按需播音效。

约定：

- 只抽颜色、尺寸、圆角这类会被复制粘贴的样式，不抽业务状态和布局。函数返回 `mui.Element`，调用方
  继续链式覆盖，后写的生效（等价于外部覆盖 class）。
- 有跨帧状态的组件才用 `mui.Local`/`mui.Key`；上面这些样式函数保持无状态。
- `caption` 强制 `SingleLine`，可能很长、需要换行的文本用 `mutedText`。
- `a.composerChip(c)` 已并入 `chipBase(c)`，不要再按 App 方法添加同类样式。
- 新增共享组件先加进 `widgets.go`，并在本文件补一行；可交互组件保留 label，现有 `Tester` 断言
  （`tt.Click("Allow once")` 一类）不依赖样式。

已迁移：`app.go`、`composer.go`、`attachments.go`、`diagnostics.go`、`gitpanel.go`、`permission.go`、
`saved.go`、`settings.go`、`sidebar.go` 中的重复次要文字、工具卡片、胶囊、「⋯」菜单，以及全部
`mui.Button` / `mui.PrimaryButton` / `crybutton` 调用（统一为 `quietButton` / `primaryButton` / `dangerButton`）。
Git 历史改为 `internal/ui/gitgraph.go` 的自绘可展开列表。

## 不替换

- 消息渲染：CrychicUI 的 `chat.Message` 只有 `Sender/Text/Mine/At`，装不下本项目的角色、thinking、tool call、附件、
  plan/usage；继续用 `App.message` 自己的渲染器。需要时只替换其中的零件（如 `code.CodeBlock`、`agent.Reasoning`）。
- 侧栏与 composer：现有状态（多会话、存档、拖拽高度、git 分支芯片、模型弹层）与组件库模型不匹配，混用会花；
  换之前先确认数据能映射，不做一次性重写。

## 交互约定

- diff 卡片直接用 `editor.DiffView`（`editor.DiffState`）：它收两份完整文本（`editor.NewBuffer`），不是 patch；由
  `internal/git.Diff` 负责取 old/new，组件只画。切换文件时重建 `editor.DiffState`，同一份文本不重建。
  `DiffState` 支持 `Wrap`、`Font`、`LineHeight`；卡片宽时 `DiffSplit`，窄时 `DiffUnified`，`Wrap` 始终开启。

- 组件自己播音效（`sound` 包合成，默认开启）。不需要时 `sound.SetEnabled(false)`，或 `sound.SetVolume`。
  测试二进制默认静音，断言音效才用 `sound.Recent()`。
- 可交互组件通过 `.Label()` 暴露标签，`tt.Click("Allow once")` 这类按标签点击仍然有效。
- 事件返回值与 ACP 语义解耦：先拿到点击/变更，再调用 `finishPermission` 等现有函数。
- `ui.DialogBase` 在点遮罩或 Esc 时会把 `*open` 置 false 并标记 `Dismissed()`；权限弹窗传入只存在于当前帧的
  `open`，所以这些关闭动作被吞掉。只有 `finishPermission`（选项或 `Cancel`）会结束请求，不要改回
  `cryoverlay.Modal` 或复用 `a.PermissionOpen` 指针。
- `showNextPermission` 只取出属于当前会话的请求；其他会话的请求留在 `permissionQueue`，切走时把可见请求放回
  队首并隐藏弹窗，切回时再显示。后台请求不能抢焦点或自动切换会话。
- 组件对必填参数 panic（空 label、空 code、无效图标、非法索引）。列表数据要先补齐（如 `At` 非零），不要给组件塞空值。

## 升级检查

```sh
go get github.com/ELY-Open-Source/CrychicUI@<commit>
go mod tidy
go build ./... && go vet ./... && go tool mygo vet . && go test ./...
```

再跑一次真实界面：空状态、权限弹窗、深浅色各看一遍（组件库按 `c.Theme()` 取色，主题由 MyGo 提供）。
