# 右侧 Git 面板

本文记录右侧 Git 面板的布局、探测、操作和边界。实现见 `internal/git` 与 `internal/ui/gitpanel.go`。

## 布局

- 窗口右侧固定宽 300 的可折叠面板，跟随当前会话的 workspace；没有会话时跟随草稿 workspace。
- 顶部标题栏（MCP 左侧）的 `Git` 按钮显示/隐藏面板；面板打开时按钮为主色（蓝色），关闭后恢复普通样式。
- 初始打开。面板不被持久化，重启后恢复默认打开。
- 有仓库时从上到下：commit message 输入框、Commit 按钮、错误/成功提示、暂存/更改列表、Git 图表和功能按钮。
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
- “Staged” 和 “Changes” 都显示状态字母、文件名和目录；`＋` 暂存、`－` 取消暂存，标题旁提供 Stage all / Unstage all。
- 图表用 `git log --graph --decorate --abbrev-commit --all -n 80 --pretty=format:%h %s%d (%an, %ar)`，等宽字体、可滚动，不解析成图形组件。
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

## 测试

```bash
go test ./internal/git -v
go test ./internal/ui -run 'TestGitPanel' -v -count=1
```

`internal/git` 测试覆盖状态/重命名解析、分支头解析、init、stage/unstage、commit、首提交撤销和嵌套工作区；UI 测试覆盖无仓库提示与 init、无 git 提示、Stage all + Commit 全链路。
