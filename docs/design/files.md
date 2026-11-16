# 右侧 Files 文件面板

本文记录右侧 Files 面板的树、Git 状态、文件操作和拖动。实现见 `internal/files`、`internal/ui/filepanel.go`。

## 目标与边界

- 浏览当前对话（或草稿）workspace 内的文件和文件夹，不跟随 `@` 索引，也不使用 ACP 的 `fs` 能力。
- 文件夹懒加载、可折叠；每个文件右侧显示 Git 状态字母。
- 右键文件/文件夹：`Reveal in folder`、`Rename`、`Delete`。
- 拖动文件或文件夹到文件夹行（或根区域）移动位置；不实现同一目录内的排序。
- 只操作当前 workspace 内的路径；不做跨工作区移动、不内嵌终端、不移动系统回收站。点文件行在共享查看卡片里查看和编辑文本文件。

## 右侧栏

- Files 是右侧列第三个 tab，和 Git / Tasks 共用 300 DIP 宽度、`GitPanelOpen` 开关和窗口最右缘 38 DIP 的图标栏。
- 图标栏顺序：Git `git-branch`、Tasks `list-checks`、Files `folder-tree`。label / tooltip 为
  `Show Files panel` / `Hide Files panel`；当前 tab 用 Accent 底和 Accent 图标。
- 默认仍打开 Git；切到 Files 时才开始读树。关闭右栏、切到其他 tab 或切换 workspace 会关闭 Files 的
  新建 / 重命名 / 删除对话框。
- 面板头部：`Files` 标题、加载 spinner、`New file`、`New folder`、`Refresh files` 三个图标按钮；
  第二行是 workspace 目录名、`⑂ 分支`、`N changed`。没有 Git 时显示 `no Git`，Git 读取失败但树正常时
  显示 `status unavailable` 并把完整错误放在 tooltip。
- 面板正文：错误 / 提示 / Git status 警告 → 文件树。workspace 根下的项目立即可见，文件夹默认折叠。
- 文件行：折叠箭头 / 文件占位、folder/open-folder 或按类型区分图标（代码 `<>`、JSON `{}`、图片画框、
  文档文本线、shell `>_`、配置齿轮、未知类型页面）、名称、Git 状态字母（文件）或
  Accent 圆点（该文件夹下有变化）。行高随 Git 字号缩放，右侧留 12 DIP 给滚动条；每深一层左侧多缩进
  12 DIP，根层缩进也是 12 DIP，子项与仓库列表左缘对齐、层级一眼可辨。缩进放在左侧 padding，不能用
  右侧 padding 代替，否则行只会变窄而不右移。
- 点文件行打开查看卡片后，该行保持主题 `Selection` 选中底色（与 hover 的 `SurfaceHover` 区分），
  指针移开也不消失；关闭卡片或打开另一个文件时清除，同时给行设置 `AccessSelected`。
- 被 `.gitignore` 忽略的文件/文件夹不隐藏：名称和图标降低 25% / 45% 透明度，右侧显示小号 `ignored`，
  tooltip 追加 `Ignored by Git`。被忽略的文件夹展开后，后代通过路径前缀继承标记；workspace 本身落在仓库的
  忽略目录里时（如 `build/app`）整棵树都标记为忽略。有状态字母/圆点的行不显示 `ignored`。
- 类型图标只按文件名的扩展名 / 常见无扩展名文件（`Makefile`、`LICENSE`、`.env.*` 等）判定，不读文件内容、
  不 stat，符号链接和超大目录都不增加开销；映射表在 `internal/ui/fileicons.go`，未知类型用普通页面图标。

## 树与加载

- 每个目录只读自己的直接子项：打开文件夹时异步 `os.ReadDir`，结果按 `workspace + 路径` 存进
  `FilePanel.Entries`，并带 generation + workspace 校验；切换 workspace 或刷新后旧结果直接丢弃。
- 根目录在打开 Files tab 时读取一次；展开文件夹按需读取。文件夹内容读取中显示 spinner，读取失败在该层
  显示错误，不影响其他分支。
- 排序：目录在前、文件在后，各自按名称大小写不敏感排序；跳过每个目录里的 `.git`。
- 符号链接按文件显示，不展开；因此浏览不会跟着链接走出 workspace。
- 展开状态只存在内存：重启或切换 workspace 后文件夹恢复默认折叠；同一 workspace 内开关 tab / 右栏会保留。
  tab 本身不持久化，重启恢复默认打开的 Git。
- 创建、重命名、移动、删除成功后清空已加载的子目录并重读根目录；已展开的文件夹保持展开并重新懒加载。
  文件夹重命名 / 移动时同步重写 `Expanded` 中的路径前缀。
- 名字校验：去首尾空格后必须是一个普通文件名，不能为空、`.`、`..`、包含 `/` 或 `\`、包含 NUL，或叫 `.git`。
  新建文件 / 文件夹固定建在当前 workspace 根；重命名在当前父目录内；目标已存在时在对话框里直接报错。

## 文件查看与编辑

- 点文件行打开共享的浮动查看卡片（`internal/ui/viewer.go`，详见 [viewer.md](viewer.md)）；点目录行仍然只展开/折叠。
- 默认 View：Markdown（`.md` / `.markdown` / `.mdown` / `.mkd`）用 `markdown.View` 渲染，其他文本用只读
  `editor.View` 并带语法高亮、行号和 `Wrap`。`Edit` 切到同一个编辑 buffer，标题栏出现 `Save`，也支持
  `⌘S`（非 macOS 为 Ctrl+S）。
- 读取和保存都走 `internal/files.ReadText` / `SaveText`：限制 512 KiB，拒绝 NUL 字节（二进制）和符号链接，
  防止跟随链接走出 workspace；保存保留原权限，在同目录写临时文件后 `rename`，文件已被删除时不重建。
- 保存成功后更新卡片正文、显示 `Saved.`，并让 Git 面板重读状态；Files tab 仍显示时重读文件树和状态，
  隐藏时只标记 stale。保存失败在标题栏下方显示错误，原文件不动。
- 关闭右栏、切到其他 tab 或切换 workspace 会关闭卡片；读取与保存用 generation + workspace + path 守卫，
  过期结果直接丢弃。
- 卡片宽度可拖左缘调整，和 Git / Tasks 共用 settings 的 `viewerWidth`。

## Git 状态

- 树只调用 `git.LoadStatus`：`rev-parse --show-toplevel` + `status --porcelain=v1 -z --branch --untracked-files=all`，
  不读提交图、diff 统计或文件内容，所以打开 Files 不会跑 `git log`。
- 忽略路径由同一次 `LoadStatus` 里的 `git ls-files -z --others --ignored --exclude-standard --directory` 提供：
  只列未跟踪且被忽略的路径，已跟踪文件不受 `.gitignore` 影响；整目录被忽略时只返回 `build/` 一行，避免
  `node_modules` 级别的展开。读失败只丢忽略标记，不影响状态字母和文件树。
- 状态按路径映射到 workspace 相对路径：workspace 是仓库根时直接用 status 路径；workspace 是仓库子目录时
  只保留它下面的路径，仓库其他目录的改动不显示。
- 字母：`U` 未跟踪、`!` 冲突、工作区状态优先于暂存状态（`M`/`D`/`R`/`C`/`T`/`A`），没有状态则不显示。
  颜色：`A`/`U` 绿、`D`/`!` 红、`M`/`T` 黄、`R`/`C` Accent；tooltip 同时列出 staged / unstaged 两侧和原路径。
- 文件夹不显示自己的字母，只在有任一后代变化时显示 Accent 圆点；已删除的文件不在树里出现。
- 不是 Git 仓库、没安装 Git 时文件树照常工作，只是没有状态；其他 Git 错误显示为不阻塞树的警告。
- 一轮对话结束、任务成功提交或失败回滚、窗口重新获得焦点、Git 面板所在的文件被操作后都会重读 Files：
  当前显示时立即刷新；隐藏时只标记 stale，下次打开 tab 再读，避免打断正在进行的文件操作。

## 右键与对话框

- 行右键使用 MyGo `ContextMenu`，顺序固定：`Reveal in folder`、`Rename`、`Delete`。
  - `Reveal in folder` 调 `mygo.Shell.ShowItemInFolder`（测试通过 `Options.RevealItem` 注入），只展示，
    不打开编辑器。
  - `Rename` 打开模态输入框；`Delete` 先打开危险色确认框，确认后删除文件或整个文件夹。
- 新建文件（`O_CREATE|O_EXCL`，0644）、新建文件夹（0755 由 umask 调整）、重命名和移动都在 goroutine 中执行；
  UI 线程只改状态。失败显示在面板顶部，对话框已关闭时可重新打开重试。
- 删除是永久删除，不进回收站；确认框写明文件夹会连同内容一起删除。不执行 `git rm`，Git 状态随下一次
  status 读取更新。
- `New file` / `New folder` 只建在 workspace 根，不做右键文件夹内的新建；文件夹创建成功后自动展开。

## 拖动

- 每个文件 / 文件夹行是 `fileDragValue{Workspace, Path, Dir}` 的拖动源。
- 文件夹行接受拖入，目标就是该文件夹；文件行的目标是它所在目录；树根空白区域的目标是 workspace 根。
  指针悬停在有效目标上时行/树根以 Accent 浅色高亮。
- 松开后异步 `os.Rename` 到目标目录，返回新相对路径；目标已存在、目标不是目录、或把文件夹移进自己 /
  后代时拒绝并显示错误。同目录拖动是 no-op，不做排序。
- 拖动值里带 workspace，切换 workspace 后遗留的拖动不会误移动新目录里的同名路径。

## 安全与异步

- `internal/files.Resolve` 用 `filepath.Rel` 拒绝 `..` 和绝对路径；所有操作只接受树生成的相对路径，
  不经过 shell，workspace 根不能被重命名、移动或删除。
- 不跟随符号链接目录；删除链接只删链接本身。
- 文件操作超时 30 秒；根读取和 Git status 超时 10 秒。过期结果由 generation + workspace 丢弃。
- 不使用 `git add` / `commit` / `checkout`；文件面板只改工作区，Git 面板仍负责暂存和提交。

## 测试

```bash
go test ./internal/files -v
go test ./internal/git -run 'TestLoadStatus' -v
go test ./internal/ui -run 'TestFilePanel|TestRightRail' -v -count=1
```

`internal/files` 覆盖排序、跳过 `.git`、新建/重命名/移动/删除、重名、非法名字、越界路径、根目录保护、
符号链接在树中按文件显示，但查看/编辑会拒绝（避免跟随链接走出 workspace）。`internal/ui` 覆盖面板 tab、树与 Git 状态、文件夹展开、新建文件/文件夹、右键重命名/删除、
Reveal 注入、拖进文件夹、跟随 workspace 切换，以及点击文件打开查看/编辑卡片、Markdown 渲染、保存和二进制拒绝。
