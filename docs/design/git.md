# 右侧 Git 面板

本文记录右侧 Git 面板的布局、探测、操作和边界。实现见 `internal/git` 与 `internal/ui/gitpanel.go`。

## 布局

- 窗口右侧固定宽 300 的可折叠面板，跟随当前会话的 workspace；没有会话时跟随草稿 workspace。
- 顶部标题栏（MCP 左侧）的 `Git` 按钮显示/隐藏面板；面板打开时按钮为主色（蓝色），关闭后恢复普通样式。
- 初始打开。面板不被持久化，重启后恢复默认打开。
- 有仓库时从上到下：带边框的 commit message 输入框、Commit 按钮、错误/成功提示、暂存/更改列表、Git 图表和功能按钮；输入框和按钮之间不再放常驻状态条。
- 标题行右侧显示未提交统计：`N files` 加绿色 `+新增`、红色 `−删除`，零变化或读取中不显示。
  数字由 `git diff --numstat -z HEAD` 得到（首个提交前用空树 hash），未跟踪文件按整文件新增行数计入；
  二进制或超过 512 KB 的未跟踪文件只计文件数。统计是装饰性的，失败只清空数字，不阻塞面板。
- 变更行可点：点文件名在 Git 面板左侧弹出 diff 卡片（见下节），右侧 `＋`/`－` 仍只做暂存/取消暂存。
- 变更区域和图表区域用 `ui.SplitVertical` 分隔，可拖动调整高度，二者总高度不变。
- 状态区在 workspace 切换时立即清空并异步重读，避免显示上一个仓库的内容。

## 探测与提示

- 用 `exec.LookPath("git")` 判断 git 是否可用；不绑定、不安装 Git。
- `git` 不存在：显示 “Git is not installed.”、PATH 提示和 Refresh，不显示仓库操作。
- git 存在但 workspace 不在仓库中：用 `git rev-parse --show-toplevel` 判断；显示 “Not a Git repository” 和 “Initialize Git repository”（`git init`）。
- 从子目录打开时使用仓库根目录执行状态、提交和图表命令，但面板仍属于当前 workspace。
- 其他错误（目录不可读、超时、`git status` 失败）显示错误和 Refresh，不伪造空仓库。

## 状态

- `git status --porcelain=v1 -z --branch --untracked-files=all` 一次读出分支、ahead/behind、暂存、未暂存、未跟踪和重命名/复制信息。
- 解析按 NUL 分隔，文件名不经过 shell；重命名记录保留 `OrigPath` 用于提示。
- 同一文件可以同时出现在 Staged 和 Changes（例如 `RM`）。冲突显示为 `!`，归入 Changes。
- “Staged” 和 “Changes” 都显示状态字母、文件名和目录；目录在文件名同一行后面（灰色小字，超长截断），
  不再另起一行。`＋` 暂存、`－` 取消暂存，标题旁提供 Discard all / Unstage all / Stage all；
  Discard all 用危险色放在 Stage all 左侧，点击先弹确认框。
- 列表内容右侧留 10 DIP 内边距：滚动条是覆盖在内容上的 overlay，不占布局宽度，留白保证 `＋`/`－`
  和 Stage all / Unstage all 不被滚动条盖住。
- 图表用 `git log --topo-order --all -n 80 --pretty=format:%H%x1f%P%x1f%an%x1f%s%x1f%D%x1f%cI`，解析成 `internal/git.LogCommit`（hash、parents、author、subject、refs、committer time），由 `internal/git` 计算 lane，界面由 `internal/ui/gitgraph.go` 自绘泳道、分支/标签芯片和提交行。
  不用通用提交图组件：它的行高固定、行不可点，没法缩间距或插入文件列表。
- 行高 22 DIP，整行可点（含左侧 lane 区域）；hover/展开时背景覆盖整行，lane 线用 `DrawOver` 画在行上层，不会遮住文字。
- 提交行不再显示 7 位 hash，只保留 ref 芯片和 subject；完整 hash 放在 hover 详情卡里。
- 指针在提交行停留 350 ms 后弹出详情卡（`ui.PopoverBase`）：显示完整 subject、Author、Date（本地时间）、全部 refs、
  修改文件数与代码增删行数（同一行显示），以及完整 hash 和 `Copy` 按钮；点 `Copy` 把完整 hash 写入剪贴板并弹 toast。
  卡片放在行的左侧（`AnchorStart`/`AnchorEnd`，离面板 8 DIP），不遮住 Git 图表，放不下时按 `AttachTo` 规则移位；
  离开行和卡片 160 ms 后关闭，便于指针移入卡片。点行本身、按 Esc 或点卡片外会立即关闭，
  并在指针离开该行前不再弹出；不再悬停的提交会随刷新自动清理。
- 修改统计用 `git show --no-color --numstat -z --format= --find-renames --first-parent <hash>` 读取，与展开文件列表一样
  只比较第一父提交；二进制文件计文件数但不计行数。读取在 goroutine 中异步进行，带 generation + workspace + hash 守卫；
  卡片打开时显示 `Reading…`，失败显示 git 错误，不阻塞图表。
- 点击提交行展开该提交相对第一父提交的文件列表，再点收起；同屏只展开一个。文件行显示状态字母 + 文件名 + 目录（同一行），
  `A` 绿、`D` 红、`M`/`T` 黄、`R`/`C` 用强调色。展开高度计入 lane 的 y 坐标，泳道在文件列表左侧继续。
- 文件行可点，打开与 Staged/Changes 相同的 diff 卡片，标题为 `Commit <7 位 hash> · <path>`；侧文本用
  `git show <hash>^:<oldPath>` 与 `git show <hash>:<path>` 读取（新增 old 为空、删除 new 为空，重命名用 `OrigPath`
  作旧侧），二进制/超大文件与普通预览一样报错。读取沿用 `PreviewGen` + workspace + hash + path 守卫。
- 展开数据用 `git show --no-color --name-status -z --format= --find-renames --first-parent <hash>`：NUL 分隔，
  重命名/复制记录依次是状态、旧路径、新路径，merge 只取第一父提交的差异。读取在 goroutine 中进行，
  generation + workspace + hash 三重守卫防止旧结果落到新行；刷新后发现该提交已不在 80 条历史里时自动收起。
- 必须用 `--topo-order`：绘图要求父提交排在所有子提交之后，普通逆时间顺序不保证这一点。
- lane 最多 8 条（`GraphLanes`）。算法维护“仍等待的父提交”列表：子树在父子的 lane 上继续，合并的第二个及以后的父提交占用空闲 lane；超过 8 条并发时共用最后一条，不报错、不 panic。
- refs 来自 `%D`：去掉 `HEAD ->` / `origin/HEAD ->` 的目标前缀和 `tag:` 前缀，去重。面板只有 300 DIP，提交行只保留第一个 ref（通常是当前分支）并截到 12 个字符；作者不在行内显示，把宽度留给 subject，详情卡里列出全部 refs。
- 提交 subject 为空时显示 `(no subject)`（提交行必须有文字）；无法识别的日志行（例如混入的 warning）直接跳过。
- merge 提交只按第一父提交展开和预览，不展示相对每个父的 combined diff；也不画 rebase/merge/revert 进行中的状态。
- 没有提交时显示 “No commits yet.”；`Undo` 对首个提交也有效（`update-ref -d HEAD`），其余提交用 `reset --soft HEAD~1`。

## 操作

- Commit：`git commit -m <message>`，只提交已暂存内容；message 为空或没有暂存文件时禁用。
- Fetch：`git fetch --prune`。
- Pull：`git pull --no-edit`，并设置 `GIT_MERGE_AUTOEDIT=no` 防止等待编辑器。
- Push：`git push`，只推当前分支已配置的 upstream；没有 upstream 时显示 git 错误。
- Refresh：重新执行状态和图表读取。
- 一轮对话结束时（`session/prompt` 返回或报错）自动重读一次，agent 刚写入的文件立即出现在列表里。
  只刷新当前会话所在的 workspace；面板隐藏时只把状态标记为待刷新（下次打开再读），正在读取或操作进行中时跳过，
  与窗口获得焦点时的规则一致。
- 窗口获得焦点时自动重读状态和图表；面板关闭、正在读取或操作进行中时跳过，避免打断进行中的 Git 命令。
- Undo：`reset --soft`，提交内容保留在暂存区，不丢文件改动。
- Discard all：`git reset -q` 清空暂存 → 有 HEAD 时 `git checkout -- .` 恢复工作区 → `git clean -fd` 删除未跟踪文件和目录。
  已暂存、未暂存和未跟踪的更改都会被丢掉；忽略文件保留（不用 `clean -x`）。必须先在确认框里点 `Discard`，
  Cancel、Esc 或点遮罩外都不执行；确认框显示 staged/unstaged 数量，面板关闭或切换 workspace 时自动关闭。
- 所有命令通过 `exec.Command` 直接传参数，不经过 shell；不使用 force push、reset --hard。

## 异步与安全

- 所有 git 命令在 goroutine 中执行，结果通过 `App.Post` 回到 UI 线程。
- 每次读取/操作带 generation 和 workspace 校验；切换会话或再次刷新后，过期结果直接丢弃。
- 状态/图表超时 10 秒，普通操作 15 秒，Commit 与 Discard all 30 秒，Fetch/Pull/Push 60 秒；应用退出时由 `lifeCtx` 取消。
- 环境固定 `GIT_TERMINAL_PROMPT=0`、`GIT_PAGER=cat`、`LC_ALL=C`；不弹出隐藏的凭据输入，不内嵌终端。
- 不改写用户仓库之外的内容；面板只操作当前 workspace 所在仓库。

## Diff 预览

- 点“Staged”/“Changes”里的文件名，或展开提交后的文件行，在 Git 面板左侧弹出浮动卡片（`ui.PopoverBase`）：
  右缘贴面板左缘（`AttachTo(anchor, AnchorStart, AnchorEnd)` + `MarginEnd(10)`，anchor 是 `gitPanel` 返回的整列），
  垂直居中，高 86% 窗口，圆角、边框和阴影，像放大的 Git 图表详情卡；对话布局不变，面板不被遮挡。
  标题写明 `Diff · <path>`、`Staged diff · <path>` 或 `Commit <7 位 hash> · <path>`，右侧只有 `✕`（无障碍名 Close）。
- 宽度响应式：`gitDiffCardWidth(windowW)` = `clamp((windowW - 300) * 0.62, 480, 900)`（300 是 Git 面板宽），
  窗口变宽时卡片跟着变宽，仍留下部分对话可见。
- 布局响应式：卡片宽 `≥ gitDiffSplitWidth`（700）时用 Split 左右对比，否则用 Unified 上下对比；
  切换布局保留同一个 `editor.DiffState`（滚动位置和状态不丢），两栏都开启 `Wrap`，横向不滚动。
- 点卡片外、按 Esc 或 `✕` 关闭（前两者是 `PopoverBase` 的 `Dismissed`）；隐藏 Git 面板或切换 workspace 时也会关闭。
- 内容直接用 MyGo `editor.DiffView`（`editor.DiffState`）：带行号、行内高亮和 +/− 统计。
  字号 11 DIP（比编辑器默认 13 小）、行高 1.4，并开启 `Wrap`：超宽的行自动换行，不需要左右滚动；纵向滚动照常。
- 两侧文本的来源（`internal/git.Diff`）：
  - 已暂存：old = `git show HEAD:<path>`，new = `git show :<path>`；新增（`A`）的 old 为空，删除（`D`）的 new 为空。
  - 未暂存：old = `git show :<path>`，new = 工作区文件；未跟踪（`??`）的 old 为空，工作区已删除（`D`）的 new 为空。
  - 重命名用 `OrigPath` 作 old；冲突（`UU` 等）不猜内容，提示先解决冲突。
  - 提交（`internal/git.CommitDiff`）：old = `git show <hash>^:<oldPath>`，new = `git show <hash>:<path>`；新增的 old 为空，删除的 new 为空。
- 限制：单侧上限 512 KiB；含 NUL 字节按二进制处理。两种情况都显示错误而不是乱码，不截断展示。
- 预览状态在 `gitPanelState`（`Preview*`），带 generation + workspace + path（提交预览再加 hash）校验；切换 workspace
  时丢弃，后台读完成也不会覆盖更新的一次。
- 只读：预览不暂存、不修改文件，所有操作仍走原来的 stage/unstage/commit。

## 测试

```bash
go test ./internal/git -v
go test ./internal/ui -run 'TestGitPanel' -v -count=1
```

`internal/git` 测试覆盖状态/重命名解析、分支头解析、init、stage/unstage、commit、首提交撤销、discard all（已暂存/未暂存/未跟踪/首提交前、忽略文件保留）、嵌套工作区，以及 diff 两侧（未暂存/已暂存/未跟踪/新增/删除/重命名、二进制与超大文件、越界路径）、提交日志解析（含 committer time）、refs 解析、commit 文件列表与提交 diff 两侧（首提交/单文件/重命名/删除）、提交文件数与增删行数统计、merge 的 lane 分配与 8 lane 上限、父提交必须在子提交之后；UI 测试覆盖无仓库提示与 init、无 git 提示、窗口获得焦点后刷新、一轮对话结束后刷新、Stage all + Commit 全链路、Discard all 确认/取消与位置、diff 卡片打开（位于 Git 面板左侧、面板仍可见、点外部关闭）、响应式布局（窄卡片
Unified/宽卡片 Split、点阈值宽度）与二进制报错、提交图渲染与窄面板裁剪、点击提交展开/收起文件列表、从展开列表打开提交 diff、hover 详情卡（位于图表左侧、无短 hash/父提交、含文件与行数统计）与复制 hash。
