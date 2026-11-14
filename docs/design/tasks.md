# 右侧任务面板与串行任务 runner

本文记录右侧 Tasks 面板、任务持久化、执行 / 检查双 agent、失败回滚和完成后的提交。实现见 `internal/tasks`、`internal/ui/tasks.go`、`internal/ui/taskrunner.go`、`internal/git/snapshot.go`。

## 目标与边界

- 每个 workspace 有一组有序任务，状态只有 `pending`、`ongoing`、`complete`；complete 再带 `pass` / `failure` 结果。
- 任务串行执行：同一时间最多一个任务、一个 executor 回合。一个任务结束（pass 或 failure）后自动处理下一个 pending；只有用户 Stop 会停止队列并把当前任务退回 pending。
- 每个任务开始时新建会话和新的 agent 进程，不复用上一个任务的上下文。
- 每个 executor 回合结束后，另起一个隔离的 checker agent 进程。checker 看任务文本和截至目前每轮的 executor 最后一段 + 该轮之后的 checker 反馈，判断完成或继续。
- 成功写 Markdown 总结到 workspace 的 `tasks/`，并执行一次 `git add -A` + `git commit`，不 push。
- 失败不 commit：先恢复到任务开始前的 work tree 快照，丢弃本轮成果，只保留总结和 complete(failure) 标记。
- 不做任务间并行、不做独立成果验证、不改写 agent 的 git 历史、不自动 push、不安装/登录 agent。

## 右侧栏

- 右侧仍是 300 DIP 的可折叠列，`Git` 与 `Tasks` 共用宽度和开关。切换只由标题栏的 `Git` / `Tasks` 按钮完成；面板内部始终只显示当前面板名称，不再放 tab 条或面板内切换按钮。
- 默认面板是 Git；Tasks 跟随当前会话 / 草稿的 workspace。开始任务时自动打开 Tasks。
- Tasks 面板从上到下：当前名称与任务计数 / 运行阶段、`Run pending`、`Stop`、错误与提示、任务列表。
- 任务行显示状态符号：pending `○`、ongoing spinner、complete 成功 `✓ pass`、complete 失败 `✗ failure`；下面显示状态 / 轮次 / note。pending 行有上移、下移、编辑、删除，complete 行有 View 和删除，ongoing 行不可编辑/移动/删除。
- `Run pending` 从当前 workspace 的第一个 pending 开始，随后按列表顺序自动跑完所有 pending。`Stop` 只停当前任务并回到 pending，同时停止队列。
- 失败任务留在 complete 列表里，带 failure 标记和总结；runner 继续按顺序执行剩余 pending，不会因为失败停队列。用户随时可以 Stop。
- 新任务 / 编辑任务使用模态：标题必填，详情可选。只有 pending 可编辑。删除有确认框；删除任务记录不删除已经写出的 Markdown 总结文件，也不删除侧栏里对应的 executor 会话（会话可在侧栏单独删除）。

## 数据

`<data>/tasks.json` 保存任务索引：

```
{version, tasks:[{
  id, workspace, title, text, status, result,
  createdAt, startedAt, completedAt,
  summaryPath, agentId, sessionId, rounds, note
}]}
```

- `id` 是随机 8 字节十六进制串；`workspace` 是绝对路径；`status` 只写三个固定值。
- `result` 只对 complete 有效：`pass` 或 `failure`。旧数据里没有 result 的 complete 行读取时补成 `pass`；pending / ongoing 行清空 result。
- `summaryPath` 是相对 workspace 的路径，固定为 `tasks/<id>-<slug>.md`；UI 读取时再拼回绝对路径，并拒绝 `tasks/` 之外的路径。
- 任务总结文件格式：标题、状态 / result / 任务 ID / workspace / 完成时间 / executor session / 轮次、`## Task`、`## Summary`、`## Checker`、`## Agent's final report`（与 Summary 重复时省略）。
- `tasks.json` 与 `sessions.json` 同在用户数据目录，原子写入、0600；总结文件是 workspace 项目文件，0644。成功的总结会随任务 commit 一起提交；失败的总结只留在 work tree，不提交。
- 启动时读取索引；上次退出遗留的 `ongoing` 自动改成 `pending`，并写入 note（执行进程不会跨重启存活）。索引损坏只显示错误，不阻塞启动。
- DataDir 为空时任务只在内存中，总结仍按需求写入 workspace 的 `tasks/`。

## 串行 runner

只有一个全局 runner（`App.TaskRun`），同一时间不能启动第二个队列：

1. `Run pending` 找到当前 workspace 第一个 pending，标记 `ongoing`，保存索引。
2. 在启动 executor 前异步记录 work tree 快照（临时 Git index；非仓库跳过），用于失败回滚。
3. 新建一个 `Chat` / agent 进程 / ACP 会话（聊天标题 `Task: …`），发送 executor prompt。
4. executor 回合结束（`session/prompt` 返回、取消或进程退出）后取本轮最后一条 assistant 文本的最后一段，追加到本任务的 round 历史。
5. 启动新的 checker agent 进程 / 新会话，发送任务标题、任务详情和完整 round 历史；checker 不允许使用工具，权限请求一律 cancelled。
6. checker 返回严格 JSON：`{complete, reason, summary}`。
7. `complete=false`：把 reason 记到本轮历史，再把 reason 发回同一个 executor 会话继续；轮次 +1，再次检查。
8. `complete=true`：写总结、commit 一次、标记 complete(pass)、释放 executor 会话，然后开始下一个 pending。
9. 执行错误 / checker 错误 / 8 轮仍未通过：恢复任务快照、写总结、标记 complete(failure)、不 commit，然后继续下一个 pending。

- 每个任务都创建新的 Chat / session，任务完成后 executor 进程关闭但对话记录保留在侧栏，可回看。
- 同一个任务内继续时复用 executor 会话，保持上下文；checker 每次判断都是全新进程和新会话，与 executor 上下文完全隔离。
- checker 在系统临时目录（`acp-gui-task-check-*`）启动，判断结束即关闭进程并删除目录：它不会加载项目上下文、不会误改工作区，也不会以工作区 cwd 出现在 `session/list` 历史里。
- `taskMaxRounds = 8`。到上限仍未通过时任务进入 complete(failure)，note / 总结包含最后一轮 checker reason；用户可以修改后重新创建任务。
- executor 回合被取消、`max_tokens`、进程异常或 prompt 错误时，同样进入 complete(failure) 并回滚本轮成果；错误显示在 Tasks 面板。
- Stop 会取消 snapshot / checker context 或关闭 executor 会话，任务回到 pending，保留当前 work tree 供下一次运行；Stop 不触发回滚。
- 正在写总结 / 回滚 / commit 的极短阶段不允许 Stop，让结果完整落盘。

## 提示词与判定

- executor prompt 标记 `TASK_EXECUTION`，要求自主完成、不要 commit / push、结尾用一段话说明本轮做了什么。
- 继续 prompt 只包含 `TASK_EXECUTION`、checker reason 和“继续”，不新建会话。
- checker prompt 标记 `TASK_COMPLETION_CHECK`，明确“没有工具、不要检查文件”，要求只输出 JSON，不能带 code fence。
- checker 历史按轮次累积：第 1 轮是第 1 轮最后一段；第 2 轮是第 1 轮最后一段 + 第 1 轮 checker 反馈 + 第 2 轮最后一段；以后各轮依次追加。这样每次全新的 checker 也知道前几轮改了什么、为什么被退回。
- 已知边界：判定只信任 executor 的总结，不验证真实成果；checker 可能误判，用户仍能在编辑器 / Git 面板里检查。最后一段由连续空行分隔，取最后一条有文本的 assistant 行的最后一段。

## 失败回滚与 complete(failure)

- 任务开始时用临时 index 执行 `git add -A` 并 `write-tree`，同时记录原 index tree；快照包含已跟踪文件和未忽略的 untracked 文件，不改动真实 index / work tree。
- 失败后先恢复快照：`read-tree --reset <snapshot>` → `checkout-index -a -f` → `clean -fd` → 恢复原 index tree。这样 executor 新建的文件被删除、改过的文件被还原，用户任务开始前已有的 staged / unstaged / untracked 改动都回来。
- 回滚不 reset 到 HEAD，因此不会把用户任务前就有的改动一起丢掉；ignored 文件保持不动。
- 非 Git 仓库、没有 git 或快照失败时无法保证丢弃：仍写总结并标记 complete(failure)，note 说明哪些改动没有回滚。
- 总结在回滚之后写入 `<workspace>/tasks/`，所以总结文件不会被回滚删掉；失败路径不执行任何 commit。
- 失败结果在任务行显示 `✗ failure`，总结卡片标题显示 `Task failure`；Markdown 内有 `- Result: failure` 和 `## Checker` reason。
- 失败后队列继续：完成回滚 / 总结 / complete(failure) 后，只要队列仍由用户启动且未被 Stop，就直接开始下一个 pending；全部 pending 结束才清空 Queue。

## 成功、总结与提交

- checker 接受后，先把 Markdown 写入 `<workspace>/tasks/`，再在 workspace 所属仓库执行一次 `git.CommitAll`：
  `git status --porcelain` 有变化 → `git add -A` → `git commit -m "task: <title>"`。
- 不 push、不 force、不 reset。commit message 压成单行，最多约 72 个字符。
- 没有 git、不是仓库或工作区已干净时不报致命错误：任务仍 complete(pass)，note 显示 “no commit” 原因。
- summary 写入失败或 commit 失败时任务仍标记 complete(pass)（成果已经发生），总结路径 / note / 错误提示保留失败原因，队列继续下一个任务。
- 提交后如果 Git tab 正在显示同一 workspace，异步刷新 Git 状态；否则标记 Git 面板待刷新。

## Task 总结卡片

- 点 complete 任务行或 `View` 打开浮动卡片，位置、宽度、阴影和锚点与 Git diff 卡片一致（右侧贴任务面板左缘，宽度 `gitDiffCardWidth`，高 80%）。pass 和 failure 都可打开。
- 默认用 `markdown.View` 渲染；标题栏 `Diff` 按钮切换到 `editor.DiffView` 的 all-additions 视图（old 为空，new 为整个 Markdown），再点 `Markdown` 切回。文件名 / 相对路径显示在标题下方。
- 关闭、切换右侧 tab、切换 workspace 时丢弃卡片和仍在读取的结果；读取由 generation + task id + workspace 守卫。

## 异步与安全

- 所有任务状态、列表、对话框和 `TaskRun` 只在 UI 线程修改；snapshot、checker、回滚 / 总结和 commit 在 goroutine 中执行，结果通过 `App.Post` 回到 UI。
- checker 和 executor 都登记到 `App.sessions`，应用退出时由现有进程组清理逻辑统一关闭。
- snapshot 30 秒超时，checker 3 分钟超时，回滚 30 秒超时，commit 30 秒超时；prompt 文本上限 24 KiB，checker 回复保留上限 256 KiB。
- 任务聊天在 runner 持有期间锁定 composer、会话切换、配置 / mode 和删除；删除该聊天会先停止 runner。
- 不把 task 提交做成隐藏的 force 操作；用户随时可以在 Git 面板看到并撤销（soft reset）上一次任务提交。
- 失败回滚会删除 executor 新建且未被 gitignore 的文件；这是 complete(failure) 的明确语义，任务总结和任务记录不受影响。

## 测试

```bash
go test ./internal/tasks -v
go test ./internal/ui -run 'TestTask|TestParseTask' -v -count=1
go test ./internal/git -run 'TestCommitAll|TestWorktreeSnapshot' -v
```

覆盖：索引读写 / 损坏与重复行 / ongoing 重启回 pending / 旧 complete 行补 pass、总结路径与 slug、任务创建 / 编辑 / 排序 / 删除与跨重启、Run pending 串行执行、`/eventually` 先 incomplete 后 complete、checker 多轮上下文、每任务 commit 次数、失败不 commit + 回滚任务文件 + 保留用户已有改动 + failure 标记 + 继续执行剩余 pending、任务聊天释放与总结卡片打开、Stop 后回 pending、verdict 解析与总结生成。
