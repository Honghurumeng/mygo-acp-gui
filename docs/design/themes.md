# 主题系统

## 目标

- 默认跟随系统明暗；允许强制 `Light` / `Dark`。
- 内置 `Default`、`Nord`、`Solarized`、`Catppuccin`、`Claude` 五个配色家族，每个都有浅色和深色版本。
- 不引入 UI/主题库：全部基于 MyGo 的 `mui.Theme`，配色目录在 `internal/ui/theme.go`。
- 不做用户自定义 JSON 主题、在线主题目录和按主题的代码高亮；新配色走代码 + 发版。
- 分区字体大小是独立设置，不随主题变化；见 [fonts.md](fonts.md)。

## 设置与持久化

`settings.json` 增加两个可选字段：

```json
{
  "appearance": "system",
  "theme": "default"
}
```

- `appearance`：`system`、`light`、`dark`。空值或未知值按 `system` 处理。
- `theme`：`default`、`nord`、`solarized`、`catppuccin`、`claude`。空值或未知值按 `default` 处理。
- 旧文件没有这两个字段时保持 `system/default`，启动不报错、不阻塞。
- `internal/settings.File` 只存字符串，不认识具体配色；`App.loadSettings` 调 `normalizeAppearance` /
  `normalizeTheme` 归一化，`persistSettings` 原样写回。

## 解析与应用

`App.View` 第一行调用 `a.applyTheme(c)`，在构建任何元素之前把窗口切到目标主题：

| appearance | theme | 结果 |
|---|---|---|
| `system` | `default` | 不调用 `SetTheme`，完全使用 MyGo 默认主题；继续跟随系统 Accent、高对比度、字号缩放 |
| `light` / `dark` | `default` | 用 `mui.LightTheme()` / `mui.DarkTheme()` + 桌面偏好，保证没有原生窗口时（测试）结果也确定 |
| 任意 | 命名配色 | `mui.LightTheme()` / `DarkTheme()` 作基底，套用该家族的 palette，再应用高对比度和字号缩放 |

- `system` + 命名配色时，用 `c.Theme().Dark`（当前帧的默认主题）选择该家族的浅色或深色 palette。
- 命名配色保留自己的 Accent；系统 Accent 只用于 `default` 家族，避免主题被桌面强调色改掉。
- 高对比度和 `TextScale` 对所有主题生效；`ReduceMotion` 继续由 MyGo 自己处理。
- `Inverse` / `InverseText` 不覆盖，沿用 MyGo 的“文字色/背景色反转”规则，tooltip 和 toast 在深浅色下都清楚。

原生窗口外观单独处理：

- `mygo.Theme.SetSource(system/light/dark)` 让 macOS 标题栏、红绿灯、Linux GTK 菜单等跟内容一致。
- `main.go` 在 `NewWindow` 之前调用 `gui.ApplyAppearance()`，避免强制深色时先闪一下系统浅色。
- 设置里切换 `Appearance` 时立即调用；测试没有真实窗口，`setAppearance` 检查 `a.window() != nil` 后跳过，
  不会碰 MyGo 的后端。

## 主题目录

| id | 浅色 | 深色 | Accent |
|---|---|---|---|
| `default` | MyGo `LightTheme` | MyGo `DarkTheme` | 系统 Accent / `#2563eb` / `#3b82f6` |
| `nord` | Snow Storm | Polar Night | `#4c6a92` / `#88c0d0` |
| `solarized` | Solarized Light | Solarized Dark | `#1f73af` / `#3aa0e8` |
| `catppuccin` | Latte | Mocha | `#8839ef` / `#cba6f7` |
| `claude` | Warm paper | Warm charcoal | `#b8562f` / `#d97757` |

每个 palette 只覆盖颜色：Background、Surface/Hover/Pressed、Border、Text/TextMuted、Accent/Hover/Pressed/Text、
Danger、Warning、Success、Selection、Focus、Scrollbar。圆角、间距、字号、滚动条宽度等指标继续取自 MyGo 的
`LightTheme` / `DarkTheme`，这样主题不会破坏布局。

Nord、Solarized、Catppuccin、Claude 的浅色版本是各自色板在浅色背景下的映射；Nord 没有官方浅色版，
使用 Snow Storm 背景与 Polar Night 文字/边框。部分浅色主题的 Accent/Warning/Success 为了文字对比度做了
加深，不保证与官方色值逐字一致。配色来源记录在 `themeFamilies` 定义上方，不复制第三方代码，只使用颜色值。

## 派生色

右侧图标栏（Git / Tasks 窄栏）的底色不写进 palette，而是由 `sunkenSurface(t)`（`widgets.go`）从 Surface 派生，
比右侧面板深一阶，看起来像面板旁的凹槽：

- 浅色：`Surface.Mix(black, 0.07)`；深色：`Surface.Mix(black, 0.18)`。深色色域被压缩（Surface 只比 Background
  高几个色阶），需要更大比例才能读出同样的分隔感；结果与面板的亮度差在浅色约 16–17、深色约 7–12。
- 不新增 palette 字段：每个家族有浅/深两套，新增字段要同步 10 份配色，Default 家族又直接用 MyGo 的主题、
  没有这个字段；派生自动覆盖全部家族、后续新家族、高对比度和系统 Accent。
- 图标栏按钮的 hover/pressed 不能用 `SurfaceHover` / `SurfacePressed`（个别浅色主题的 SurfacePressed
  与图标栏底色几乎相同），改用 `Text.Alpha(0.04)` / `Text.Alpha(0.10)` 叠在图标栏底色上：浅色变暗、深色变亮，
  与其它按钮的状态方向一致。
- 设置界面的主题预览仍只画 Background/Surface/Accent/Text 四个色块，不需要为图标栏加色块。

## 设置界面

设置面板新增 `Appearance` 分类（默认仍打开 `Notifications`）：

- 顶部 `System` / `Light` / `Dark` 三个胶囊按钮，当前项用 Accent 填充。
- 下面 `Theme` 列表：`Default`、`Nord`、`Solarized`、`Catppuccin`、`Claude`。每行展示当前明暗变体的四个色块
  （Background、Surface、Accent、Text）、名称、简介和选中勾。
- 点击立即生效：状态改在 `App` 上，`persistSettings` 落盘；下一帧 `View` 重新 `SetTheme`，
  设置弹窗、popover、代码块和 Git 面板一起换色。
- 色块用 `mui.Box` 直接画，不依赖 `mui.Themed`；主题预览在强制 `Light`/`Dark` 时也跟随当前明暗。

## 代码高亮

`components.go` 的 `codeBlock` 调用 `editor.DefaultTheme(c.Theme())`。编辑器主题只读取 `Theme.Dark` 和
背景/文字色，token 颜色固定为 GitHub 浅色或深色色板。因此：

- 换主题会改变代码块背景、边框、文字、行号和选择色。
- 语法 token 不会变成 Nord/Solarized 的专属配色，仍是 GitHub 浅色/深色。
- 这是当前范围的有意取舍；逐主题 token 色板需要额外约 200–400 行映射，留给后续。

阴影仍是 `RGBA(0,0,0,0.18~0.24)`，在现有浅色/深色主题下都可用；不为每套配色单独调阴影。

## 测试

- `internal/settings`：`appearance` / `theme` 的保存读取往返。
- `internal/ui/theme_test.go`：目录唯一性、归一化、各家族浅/深色解析、字号缩放、原生 source 映射。
- `internal/ui/settings_test.go`：打开 `Appearance`、切换 `Dark` + `Solarized`、落盘、重启恢复。
- 手动检查：System/Light/Dark × 五个家族，重点看设置弹窗、权限弹窗、代码块、Git 图、侧栏状态色。
