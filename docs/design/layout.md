# 窗口布局与可拖动分栏

本文记录窗口三栏的宽度、左右分栏的拖动交互、边界收口与持久化。实现见 `internal/ui/layout.go`、
`sidebar.go`、`rightpanel.go`、`gitpanel.go`、`tasks.go`、`filepanel.go`。

## 结构

窗口是一个横向 Row：

```
[左侧工作区/会话栏] [对话 + composer] [右侧面板（可选）] [38 DIP 图标栏]
```

- 左侧栏由标题栏的侧栏按钮开关；右侧面板由最右缘图标栏开关，Git / Tasks / Files 三个 tab 共用同一宽度
  （见 [git.md](git.md) / [tasks.md](tasks.md) / [files.md](files.md)）。
- 两栏各自的「内侧边缘」（左栏右缘、右栏左缘）是拖动分栏热区；对话始终 `Grow(1)` 吃掉剩余空间，
  分栏只改两栏自己的宽度，不动其余布局。

## 宽度

| 栏 | 默认 | 最小 | 窗口比例上限 | 绝对上限 |
|---|---|---|---|---|
| 左侧栏 | `sidebarDefaultWidth` 292 | `sidebarMinWidth` 200 | `windowW * 0.35` | `sidebarMaxWidthCap` 460 |
| 右侧面板 | `rightPanelDefaultWidth` 300 | `rightPanelMinWidth` 220 | `windowW * 0.45` | `rightPanelMaxWidthCap` 560 |

- 画出的宽度是 `min(max(设置值, 最小), 最大)`；窗口变窄时先按比例收口，比例低于最小值时保留最小值，
  所以窗口缩放不会覆盖用户拖出来的记录。
- 比例上限保证大屏上对话仍是最宽的一栏；绝对上限避免右栏变成长列表阅读器。
- 宽度都是 DIP，且不随主题或分区字号变化（[fonts.md](fonts.md) 只管文字大小）。

## 拖动与重置

- 热区宽 6 DIP（`resizeGrip`），`CursorResizeEW`；hover 或拖动时用 `t.Accent.Alpha(0.12)` 覆盖，
  与查看卡片左缘的拖动反馈一致。
- 左栏热区画在**对话一侧**的 6 DIP 上（`conversation` 左缘，仍紧贴边框），不压在侧栏自己的右缘：
  侧栏最后 12 DIP 是滚动条轨道（`scrollbarWidth + 6`），压上去会抢走滚轮和滑块的拖动。右栏热区画在
  面板左缘内侧 6 DIP，那里只有表头/列表的留白。
- 左栏向右拖变宽，右栏向左拖变宽（对话在那一侧）。每帧把 `Dragged()` 的横向增量叠加到当前宽度再收口；
  纵向移动不触发。
- 双击热区恢复默认宽度，语义与 `ui.Split` 的 `SplitReset` 相同。
- 拖动过程不写 `settings.json`：`DragEnded()` 或双击落定时才 `persistSettings`。
- 热区是装饰性的 `RoleNone`（与查看卡片一致），不进入无障碍树；宽度只能用指针拖动，不支持键盘。

## 持久化

- `settings.json` 的 `sidebarWidth` / `rightPanelWidth`（DIP，缺省 0 = 内置宽度）。加载时
  `normalizeSidebarWidth` / `normalizeRightPanelWidth` 丢掉小于最小值、非有限值或超过绝对上限的值，绘制时
  再按当前窗口收口。
- 与 `viewerWidth` 同属「用户拖动结果」：不因窗口大小变化写回，重启或换显示器后仍按当前窗口收口。
- 右栏开关 `GitPanelOpen` 仍不持久化，重启后默认打开 Git 面板。

## 与其他组件的关系

- 浮动查看卡片的宽度上限用右侧列的**实际**宽度计算（`App.rightPaneWidth(windowW)`），所以拖动右栏后
  卡片不会压住面板；右栏仍是默认 300 DIP 时与旧公式完全一致（见 [viewer.md](viewer.md)）。
- 侧栏滚动条轨道完整保留在侧栏内；对话左缘 6 DIP 内的滚轮不滚动消息列表（该条只有 padding，
  没有正文），其余面板的表头左留 12 DIP、列表右留 12 DIP 滚动条槽，热区只压在留白上。
- 分栏拖动与 composer 高度拖动、Git 面板 `ui.SplitVertical` 的上半区高度拖动互不影响：各管一个方向。

## 测试

- `internal/ui/layout_test.go`：默认/拖动/窗口收口、`normalize*`、两侧热区拖动（Press / Move / Release）、
  跨重启持久化。
- 手动：拖动左栏右缘与右栏左缘；双击恢复；缩窄窗口确认收口且对话仍可见；重启确认宽度恢复。
