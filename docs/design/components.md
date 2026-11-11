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
crybutton  "github.com/ELY-Open-Source/CrychicUI/button"
crycode    "github.com/ELY-Open-Source/CrychicUI/code"
cryfeedback "github.com/ELY-Open-Source/CrychicUI/feedback"
crykit      "github.com/ELY-Open-Source/CrychicUI/kit"
crylayout  "github.com/ELY-Open-Source/CrychicUI/layout"
cryoverlay "github.com/ELY-Open-Source/CrychicUI/overlay"
```

## 已替换

- 权限弹窗 `internal/ui/permission.go`：`cryoverlay.Modal` + `crycode.CodeBlock`（工具入参）+ 按 `option.kind` 选按钮
  （`allow*` → `button.Solid`，`reject*` → `button.Destructive`，其余与 `Cancel` → `button.Outline`/`button.Ghost`）。
  ACP 的 option 列表原样保留，不压缩成 Allow/Deny 两键。
- 空状态 `internal/ui/app.go`：`crylayout.Centered` + `cryfeedback.EmptyState`；连接/探测中用 `cryfeedback.Busy` 转圈。
  图标必须是 Lucide 里存在的名字，否则 `icon.Must` 会 panic。
- Git diff 预览 `internal/ui/gitpanel.go`：`cryoverlay.FullscreenModal` + `crycode.DiffView`，加载/错误/无变化用
  `cryfeedback.Busy` / `Alert` / `EmptyState`；高度用 `crykit.Settle` 跟随遮罩实际高度（`DiffView` 只收固定高度）。
- Git 提交图 `internal/ui/gitpanel.go`：`crycode.CommitGraph`，数据来自 `internal/git.LogCommit` 的结构化 log + lane。
  300 DIP 的面板里只传第一个 ref（截 12 字）并留空 author，把宽度给 subject；组件要求 hash 唯一、lane 0..7、父提交在子提交之后，
  否则 panic（`internal/git` 用 `--topo-order` 和 `assignLanes` 保证）。

## 不替换

- 消息渲染：CrychicUI 的 `chat.Message` 只有 `Sender/Text/Mine/At`，装不下本项目的角色、thinking、tool call、附件、
  plan/usage；继续用 `App.message` 自己的渲染器。需要时只替换其中的零件（如 `code.CodeBlock`、`agent.Reasoning`）。
- 侧栏与 composer：现有状态（多会话、存档、拖拽高度、git 分支芯片、模型弹层）与组件库模型不匹配，混用会花；
  换之前先确认数据能映射，不做一次性重写。

## 交互约定

- `code.DiffView` 收的是两份完整文本（`editor.NewBuffer`），不是 patch；由 `internal/git.Diff` 负责取 old/new，
  组件只画。切换文件时重建 `editor.DiffState`，同一份文本不重建。

- 组件自己播音效（`sound` 包合成，默认开启）。不需要时 `sound.SetEnabled(false)`，或 `sound.SetVolume`。
  测试二进制默认静音，断言音效才用 `sound.Recent()`。
- 可交互组件通过 `.Label()` 暴露标签，`tt.Click("Allow once")` 这类按标签点击仍然有效。
- 事件返回值与 ACP 语义解耦：先拿到点击/变更，再调用 `finishPermission` 等现有函数。
- `overlay.Modal` 关闭时会把 `*open` 置 false（关闭按钮、Esc、点遮罩都算）；权限弹窗据此回 `cancelled`，
  已用 `answered` 标记避免与按钮结果重复处理。
- 组件对必填参数 panic（空 label、空 code、无效图标、非法索引）。列表数据要先补齐（如 `At` 非零），不要给组件塞空值。

## 升级检查

```sh
go get github.com/ELY-Open-Source/CrychicUI@<commit>
go mod tidy
go build ./... && go vet ./... && go tool mygo vet . && go test ./...
```

再跑一次真实界面：空状态、权限弹窗、深浅色各看一遍（组件库按 `c.Theme()` 取色，主题由 MyGo 提供）。
