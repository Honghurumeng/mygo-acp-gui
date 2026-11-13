# 字体大小

## 目标

- 四个区域各自缩放文字：workspace 列表、对话流、输入框、Git 面板。
- 设置立即生效，写入 `settings.json`，重启恢复。
- 不做字体家族、逐控件字号、按主题字号；系统无障碍字号（MyGo `TextScale`）继续生效。

## 设置模型

`settings.json` 新增可选对象：

```json
{
  "fonts": {
    "workspaceList": 100,
    "chat": 100,
    "composer": 100,
    "git": 100
  }
}
```

- 值是百分比，默认 `100`，范围 `80`–`150`，UI 以 `10` 为步长。
- 缺失字段或旧文件没有 `fonts` 时按 `100`；`settings.Fonts.withDefaults` 负责补缺，UI 的
  `normalizeFontScales` 再把越界值夹到范围内。
- `internal/settings.File` 只存整数百分比；`App.Fonts` 保存当前值，`persistSettings` 写盘，
  `loadSettings` 读取并归一化。版本号不变，旧文件可继续读取。

## 缩放方式

`internal/ui/fonts.go` 提供四个区域函数：

```go
a.workspaceFont(size) // workspace 列表
a.chatFont(size)      // 对话流
a.composerFont(size)  // 输入框
a.gitFont(size)       // Git 面板
```

- 每个函数把内置字号乘以该区域的百分比，标题、正文、caption 一起按比例变化，保持原有层级。
- 正文（Markdown、输入框、提交信息）从 `c.Theme().FontSize` 起算，再乘区域百分比；
  这样 MyGo 的系统字号缩放不会被覆盖。
- Markdown 只设置 `a.MD.Theme = &markdown.Theme{FontSize: ...}`，颜色和间距仍由当前主题的
  `markdown.DefaultTheme` 解析；标题和代码块按 em 跟随正文字号。
- 区域内的 `mutedText` / `caption` / `sectionLabel` 用 `workspaceFont`、`chatFont`、
  `composerFont`、`gitFont` 包裹；`fonts.go` 另有 `workspaceCaption`、`composerSectionLabel`
  等区域版本，避免漏掉固定 9px 的小字。
- composer 的动作行高度 `composerControlHeight` 随输入框百分比缩放，避免 150% 时按钮文字
  被 22 DIP 的固定高度裁掉；Git 图和侧栏行高不变，最大字号仍在现有行高内。

## 区域范围

| 区域 | 主要文件 | 覆盖内容 |
|---|---|---|
| Workspace list | `sidebar.go`、`saved.go` | 标题、分组、打开/存档会话行、筛选、展开/收起、侧栏底部 agent 状态 |
| Conversation | `app.go`、`jump.go`、`scrollbottom.go`、`turntimer.go`、`attachments.go` | 用户/assistant/系统/错误消息、Markdown、thinking、tool card、plan、Took 行、跳转列表、Latest 和运行计时 |
| Input box | `composer.go`、`mentions.go`、`jump.go` | 输入框正文、附件/Jump/分支/mode/usage/model chip、模型面板、斜杠命令、@ 文件面板 |
| Git panel | `gitpanel.go`、`gitgraph.go` | 面板头、提交框、暂存/更改行、提交图、提交详情卡、diff 卡片和 diff 编辑器字号 |

固定不缩放：顶部会话标题栏、错误/警告 banner、设置/权限/诊断等全局模态标题、
空状态组件和 MyGo 自带的菜单/提示。它们属于窗口 chrome 或跨区域组件。

## 设置界面

设置面板新增 `Fonts` 分类（默认仍打开 `Notifications`）：

- 四行：`Workspace list`、`Conversation`、`Input box`、`Git panel`。
- 每行显示说明、`−`、当前百分比、`+`；按钮 label 为 `Decrease/Increase … font size`，供无障碍和测试使用。
- 点击后立即修改 `App.Fonts` 并 `persistSettings`，下一帧重绘。
- 底部 `Reset font sizes` 恢复四个区域的 `100`。

## 测试

- `internal/settings`：旧文件默认 100、四字段往返、缺字段补齐。
- `internal/ui/fonts_test.go`：归一化/夹取、四个缩放函数、设置面板点击后落盘并在重启后恢复。
- 手动检查：四个区域分别调到 80% 和 150%，确认侧栏行高、输入框、Git 图行高和 diff 不重叠；
  在 System/Light/Dark × 五个主题下各看一次。
