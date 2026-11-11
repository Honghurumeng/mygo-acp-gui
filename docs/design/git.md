# 右侧 Git 面板

本文记录右侧 Git 面板的布局、探测、操作和边界。实现见 `internal/git` 与 `internal/ui/gitpanel.go`。

## 布局

- 窗口右侧固定宽 300 的可折叠面板，跟随当前会话的 workspace；没有会话时跟随草稿 workspace。
- 顶部标题栏（MCP 左侧）的 `Git` 按钮显示/隐藏面板；面板打开时按钮为主色（蓝色），关闭后恢复普通样式。
- 初始打开。面板不被持久化，重启后恢复默认打开。
- 有仓库时从上到下：commit message 输入框、Commit 按钮、错误/成功提示、暂存/更改列表、Git 图表和功能按钮。
- 变更行可点：点文件名打开全窗口 diff 预览（见下节），右侧 `＋`/`－` 仍只做暂存/取消暂存。
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
  不再另起一行。`＋` 暂存、`－` 取消暂存，标题旁提供 Stage all / Unstage all。
- 列表内容右侧留 10 DIP 内边距：滚动条是覆盖在内容上的 overlay，不占布局宽度，留白保证 `＋`/`－`
  和 Stage all / Unstage all 不被滚动条盖住。
- 图表用 `git log --topo-order --all -n 80 --pretty=format:%H%x1f%P%x1f%an%x1f%s%x1f%D`，解析成 `internal/git.LogCommit`（hash、parents、author、subject、refs），由 `internal/git` 计算 lane，界面由 `internal/ui/gitgraph.go` 自绘泳道、分支/标签芯片和提交行。
  不用 CrychicUI `code.CommitGraph`：它的行高固定 36 DIP、行不可点，没法缩间距或插入文件列表。
- 行高 22 DIP，整行可点（含左侧 lane 区域）；hover/展开时背景覆盖整行，lane 线用 `DrawOver` 画在行上层，不会遮住文字。
- 点击提交行展开该提交相对第一父提交的文件列表，再点收起；同屏只展开一个。文件行显示状态字母 + 文件名 + 目录（同一行），
  `A` 绿、`D` 红、`M`/`T` 黄、`R`/`C` 用强调色。展开高度计入 lane 的 y 坐标，泳道在文件列表左侧继续。
- 文件行可点，打开与 Staged/Changes 相同的全窗口 diff 遮罩，标题为 `Commit <7 位 hash> · <path>`；侧文本用
  `git show <hash>^:<oldPath>` 与 `git show <hash>:<path>` 读取（新增 old 为空、删除 new 为空，重命名用 `OrigPath`
  作旧侧），二进制/超大文件与普通预览一样报错。读取沿用 `PreviewGen` + workspace + hash + path 守卫。
- 展开数据用 `git show --no-color --name-status -z --format= --find-renames --first-parent <hash>`：NUL 分隔，
  重命名/复制记录依次是状态、旧路径、新路径，merge 只取第一父提交的差异。读取在 goroutine 中进行，
  generation + workspace + hash 三重守卫防止旧结果落到新行；刷新后发现该提交已不在 80 条历史里时自动收起。
- 必须用 `--topo-order`：绘图要求父提交排在所有子提交之后，普通逆时间顺序不保证这一点。
- lane 最多 8 条（`GraphLanes`）。算法维护“仍等待的父提交”列表：子树在父子的 lane 上继续，合并的第二个及以后的父提交占用空闲 lane；超过 8 条并发时共用最后一条，不报错、不 panic。
- refs 来自 `%D`：去掉 `HEAD ->` / `origin/HEAD ->` 的目标前缀和 `tag:` 前缀，去重。面板只有 300 DIP，界面只保留第一个 ref（通常是当前分支）并截到 12 个字符；作者不显示，把宽度留给 subject。
- 提交 subject 为空时显示 `(no subject)`（提交行必须有文字）；无法识别的日志行（例如混入的 warning）直接跳过。
- merge 提交只按第一父提交展开和预览，不展示相对每个父的 combined diff；也不画 rebase/merge/revert 进行中的状态。
- 没有提交时显示 “No commits yet.”；`Undo` 对首个提交也有效（`update-ref -d HEAD`），其余提交用 `reset --soft HEAD~1`。

## 操作

- Commit：`git commit -m <message>`，只提交已暂存内容；message 为空或没有暂存文件时禁用。
- Fetch：`git fetch --prune`。
- Pull：`git pull --no-edit`，并设置 `GIT_MERGE_AUTOEDIT=no` 防止等待编辑器。
- Push：`git push`，只推当前分支已配置的 upstream；没有 upstream 时显示 git 错误。
- Refresh：重新执行状态和图表读取。
- Undo：`reset --soft`，提交内容保留在暂存区，不丢文件改动。
- 所有命令通过 `exec.Command` 直接传参数，不经过 shell；不使用 force push、reset --hard、clean 等破坏性操作。

## 异步与安全

- 所有 git 命令在 goroutine 中执行，结果通过 `App.Post` 回到 UI 线程。
- 每次读取/操作带 generation 和 workspace 校验；切换会话或再次刷新后，过期结果直接丢弃。
- 状态/图表超时 10 秒，普通操作 15 秒，Commit 30 秒，Fetch/Pull/Push 60 秒；应用退出时由 `lifeCtx` 取消。
- 环境固定 `GIT_TERMINAL_PROMPT=0`、`GIT_PAGER=cat`、`LC_ALL=C`；不弹出隐藏的凭据输入，不内嵌终端。
- 不改写用户仓库之外的内容；面板只操作当前 workspace 所在仓库。

## Diff 预览

- 点“Staged”/“Changes”里的文件名，或展开提交后的文件行，打开全窗口遮罩（CrychicUI `overlay.FullscreenModal`）；
  标题写明 `Diff · <path>`、`Staged diff · <path>` 或 `Commit <7 位 hash> · <path>`；X/Esc 关闭。
- 内容用 CrychicUI `code.DiffView`（MyGo `editor.DiffState`）：默认 Split，可切 Unified，带行号、行内高亮和 +/− 统计。
- 两侧文本的来源（`internal/git.Diff`）：
  - 已暂存：old = `git show HEAD:<path>`，new = `git show :<path>`；新增（`A`）的 old 为空，删除（`D`）的 new 为空。
  - 未暂存：old = `git show :<path>`，new = 工作区文件；未跟踪（`??`）的 old 为空，工作区已删除（`D`）的 new 为空。
  - 重命名用 `OrigPath` 作 old；冲突（`UU` 等）不猜内容，提示先解决冲突。
  - 提交（`internal/git.CommitDiff`）：old = `git show <hash>^:<oldPath>`，new = `git show <hash>:<path>`；新增的 old 为空，删除的 new 为空。
- 限制：单侧上限 512 KiB；含 NUL 字节按二进制处理。两种情况都显示错误而不是乱码，不截断展示。
- 预览状态在 `gitPanelState`（`Preview*`），带 generation + workspace + path（提交预览再加 hash）校验；切换 workspace
  或关闭面板时丢弃，后台读完成也不会覆盖更新的一次。
- 只读：预览不暂存、不修改文件，所有操作仍走原来的 stage/unstage/commit。

## 测试

```bash
go test ./internal/git -v
go test ./internal/ui -run 'TestGitPanel' -v -count=1
```

`internal/git` 测试覆盖状态/重命名解析、分支头解析、init、stage/unstage、commit、首提交撤销、嵌套工作区，以及 diff 两侧（未暂存/已暂存/未跟踪/新增/删除/重命名、二进制与超大文件、越界路径）、提交日志解析、refs 解析、commit 文件列表与提交 diff 两侧（首提交/单文件/重命名/删除）、merge 的 lane 分配与 8 lane 上限、父提交必须在子提交之后；UI 测试覆盖无仓库提示与 init、无 git 提示、Stage all + Commit 全链路、diff 预览打开与二进制报错、提交图渲染与窄面板裁剪、点击提交展开/收起文件列表、从展开列表打开提交 diff。
