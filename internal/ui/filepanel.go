package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/files"
	"github.com/reed/acp-gui/internal/git"
)

const (
	// filePanelTimeout bounds the first tree read and the status scan. Folder
	// reads have no context; a late result is discarded by the generation
	// guard.
	filePanelTimeout  = 10 * time.Second
	fileActionTimeout = 30 * time.Second
	fileRowGutter     = gitListGutter
)

// fileDragValue is one entry dragged inside the Files panel. The workspace is
// part of the value so a drag left over while the panel switches folders does
// not move a file in the new workspace.
type fileDragValue struct {
	Workspace string
	Path      string
	Dir       bool
}

// filePanelState is the UI-thread copy of the workspace's tree and its Git
// statuses. Folder contents load lazily; dialogs and transient notices live
// beside the tree so switching tabs does not lose the explorer.
type filePanelState struct {
	Workspace string
	Gen       int
	Checked   bool
	Loading   bool
	Busy      string
	Error     string
	Notice    string
	// StatusError is a non-fatal Git read failure; the tree still works.
	StatusError string

	Entries     map[string][]files.Entry
	Loaded      map[string]bool
	LoadingDirs map[string]bool
	DirErrors   map[string]string
	Expanded    map[string]bool

	Status    map[string]git.File
	DirtyDirs map[string]bool
	// Ignored holds workspace-relative paths Git ignores; a folder in it
	// covers its whole subtree. IgnoredAll marks a workspace that sits inside
	// an ignored folder, where every row is ignored.
	Ignored    map[string]bool
	IgnoredAll bool
	RepoRoot   string
	Branch     string

	CreateOpen bool
	CreateDir  bool
	CreateName string
	CreateErr  string

	RenameOpen bool
	RenamePath string
	RenameDir  bool
	RenameName string
	RenameErr  string

	DeleteOpen bool
	DeletePath string
	DeleteDir  bool
}

func newFilePanelState(workspace string) filePanelState {
	return filePanelState{
		Workspace:   workspace,
		Checked:     workspace == "",
		Entries:     make(map[string][]files.Entry),
		Loaded:      make(map[string]bool),
		LoadingDirs: make(map[string]bool),
		DirErrors:   make(map[string]string),
		Expanded:    make(map[string]bool),
		Status:      make(map[string]git.File),
		DirtyDirs:   make(map[string]bool),
		Ignored:     make(map[string]bool),
	}
}

func (a *App) activeFileWorkspace() string {
	return a.activeGitWorkspace()
}

// ensureFilePanel follows the active conversation's workspace and starts one
// root read when it opens or follows a chat to a new folder.
func (a *App) ensureFilePanel() {
	workspace := a.activeFileWorkspace()
	if a.Files.Workspace != workspace {
		a.Files = newFilePanelState(workspace)
	}
	if workspace == "" || !filepath.IsAbs(workspace) {
		a.Files.Checked = true
		a.Files.Loading = false
		return
	}
	if a.Files.Checked || a.Files.Loading || a.Files.Busy != "" {
		return
	}
	a.startFilesLoad(workspace, false)
}

// startFilesLoad rereads the root entries and the Git statuses. Folder
// expansion state survives, but loaded children are dropped so a changed
// folder is read again when it is drawn.
func (a *App) startFilesLoad(workspace string, clearFeedback bool) {
	if a.closed.Load() {
		return
	}
	a.Files.Gen++
	gen := a.Files.Gen
	a.Files.Workspace = workspace
	a.Files.Loading = true
	a.Files.Entries = make(map[string][]files.Entry)
	a.Files.Loaded = make(map[string]bool)
	a.Files.LoadingDirs = make(map[string]bool)
	a.Files.DirErrors = make(map[string]string)
	a.Files.Status = make(map[string]git.File)
	a.Files.RepoRoot = ""
	a.Files.Branch = ""
	a.Files.StatusError = ""
	if clearFeedback {
		a.Files.Error = ""
		a.Files.Notice = ""
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifeCtx, filePanelTimeout)
		defer cancel()

		entries, treeErr := files.ReadDir(workspace, "")
		var state git.State
		var statusErr error
		if treeErr == nil {
			state, statusErr = git.LoadStatus(ctx, workspace)
			if ctx.Err() != nil {
				statusErr = ctx.Err()
			}
		}

		a.Post(func() {
			if a.closed.Load() || a.Files.Gen != gen || a.Files.Workspace != workspace {
				return
			}
			a.Files.Checked = true
			a.Files.Loading = false
			if treeErr != nil {
				a.Files.Error = "Files: " + fileErrorText(treeErr)
				return
			}
			a.Files.Error = ""
			a.Files.Entries[""] = entries
			a.Files.Loaded[""] = true
			a.applyFileGitStatus(workspace, state, statusErr)
		})
	}()
}

func (a *App) applyFileGitStatus(workspace string, state git.State, err error) {
	a.Files.Status = make(map[string]git.File)
	a.Files.DirtyDirs = make(map[string]bool)
	a.Files.Ignored = make(map[string]bool)
	a.Files.IgnoredAll = false
	a.Files.RepoRoot = state.Root
	a.Files.Branch = state.Branch
	if err != nil {
		switch {
		case errors.Is(err, git.ErrNotRepository), errors.Is(err, git.ErrGitNotFound):
			// The tree does not need a repository; there is just no status.
		default:
			a.Files.StatusError = fileErrorText(err)
		}
		return
	}
	if state.Root == "" {
		return
	}
	workspace = canonicalWorkspace(workspace)
	root := canonicalWorkspace(state.Root)
	for _, file := range state.Files {
		rel, ok := fileWorkspaceRelative(workspace, root, file.Path)
		if !ok {
			continue
		}
		file.Path = rel
		if file.OrigPath != "" {
			if orig, ok := fileWorkspaceRelative(workspace, root, file.OrigPath); ok {
				file.OrigPath = orig
			} else {
				file.OrigPath = ""
			}
		}
		a.Files.Status[rel] = file
		for dir := fileParent(rel); dir != ""; dir = fileParent(dir) {
			a.Files.DirtyDirs[dir] = true
		}
	}
	workspaceRel := ""
	if rel, err := filepath.Rel(root, workspace); err == nil {
		workspaceRel = filepath.ToSlash(rel)
	}
	for _, ignored := range state.Ignored {
		if rel, ok := fileWorkspaceRelative(workspace, root, ignored); ok && rel != "" && rel != "." {
			a.Files.Ignored[rel] = true
			continue
		}
		// The workspace itself can sit inside an ignored folder (a checkout
		// under the repository's build/), in which case Git lists only that
		// folder and nothing inside it maps to the tree.
		if workspaceRel == ignored || strings.HasPrefix(workspaceRel, ignored+"/") {
			a.Files.IgnoredAll = true
		}
	}
}

// fileWorkspaceRelative maps a repository-relative git path to the active
// workspace's slash-relative path. Paths outside the workspace are dropped:
// the panel only ever shows this folder.
func fileWorkspaceRelative(workspace, root, repoRel string) (string, bool) {
	repoRel = filepath.FromSlash(repoRel)
	if filepath.IsAbs(repoRel) {
		return "", false
	}
	rel, err := filepath.Rel(workspace, filepath.Join(root, repoRel))
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// ensureFileDir starts one lazy read for an expanded folder. The generation
// and workspace guards keep a late read from a closed or replaced tree out.
func (a *App) ensureFileDir(rel string) {
	if rel == "" || a.Files.Loaded[rel] || a.Files.LoadingDirs[rel] || a.Files.Loading {
		return
	}
	workspace := a.Files.Workspace
	if workspace == "" || !filepath.IsAbs(workspace) {
		return
	}
	a.Files.LoadingDirs[rel] = true
	gen := a.Files.Gen
	go func() {
		entries, err := files.ReadDir(workspace, rel)
		a.Post(func() {
			if a.closed.Load() || a.Files.Gen != gen || a.Files.Workspace != workspace {
				return
			}
			delete(a.Files.LoadingDirs, rel)
			if err != nil {
				a.Files.DirErrors[rel] = fileErrorText(err)
				return
			}
			a.Files.Entries[rel] = entries
			a.Files.Loaded[rel] = true
		})
	}()
}

func (a *App) toggleFileFolder(rel string) {
	if rel == "" {
		return
	}
	if a.Files.Expanded[rel] {
		delete(a.Files.Expanded, rel)
		return
	}
	a.Files.Expanded[rel] = true
	a.ensureFileDir(rel)
}

// refreshFilesOnFocus rereads the tree when the window gains focus, so files
// changed in another app show up. It leaves a load or action already running
// alone and only reads while the Files tab is open.
func (a *App) refreshFilesOnFocus() {
	if a.closed.Load() || !a.GitPanelOpen || a.RightTab != rightTabFiles {
		return
	}
	workspace := a.activeFileWorkspace()
	if workspace == "" || !filepath.IsAbs(workspace) {
		return
	}
	if a.Files.Workspace != workspace || !a.Files.Checked {
		a.ensureFilePanel()
		return
	}
	if a.Files.Loading || a.Files.Busy != "" {
		return
	}
	a.startFilesLoad(workspace, false)
}

// refreshFilesAfterTurn rereads the tree when an agent turn ends. A hidden
// panel is only marked stale and reloads when its tab is shown again.
func (a *App) refreshFilesAfterTurn(chat *Chat) {
	if a.closed.Load() || chat == nil || chat != a.activeChat() {
		return
	}
	workspace := strings.TrimSpace(chat.Workspace)
	if workspace == "" || !filepath.IsAbs(workspace) {
		return
	}
	if !a.GitPanelOpen || a.RightTab != rightTabFiles {
		if sameWorkspace(a.Files.Workspace, workspace) {
			a.Files.Checked = false
		}
		return
	}
	a.refreshFilesOnFocus()
}

// refreshFilesAfterTask makes a task's files and commit visible to the Files
// panel, with the same stale-while-hidden rule as a turn.
func (a *App) refreshFilesAfterTask(workspace string) {
	if a.closed.Load() || workspace == "" {
		return
	}
	if !sameWorkspace(a.activeFileWorkspace(), workspace) {
		if sameWorkspace(a.Files.Workspace, workspace) {
			a.Files.Checked = false
		}
		return
	}
	if a.GitPanelOpen && a.RightTab == rightTabFiles {
		a.refreshFilesOnFocus()
		return
	}
	if sameWorkspace(a.Files.Workspace, workspace) {
		a.Files.Checked = false
	}
}

func (a *App) filePanel(c *mui.Context) mui.Element {
	a.ensureFilePanel()
	t := c.Theme()
	panel := mui.Column(c).Width(gitPanelWidth).FillHeight().
		Background(t.Surface).
		BorderWidth(1, 0, 0, 0).BorderColor(t.Border)
	panel.Children(func() {
		a.filePanelHeader(c)
		a.filePanelBody(c)
	})
	return panel
}

func (a *App) filePanelHeader(c *mui.Context) {
	t := c.Theme()
	busy := a.Files.Busy != ""
	workspace := strings.TrimSpace(a.Files.Workspace)
	disabled := busy || workspace == "" || !filepath.IsAbs(workspace)
	mui.Row(c).Height(54).Gap(6).Padding(8, 12).AlignItems(mui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		mui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
			mui.Row(c).Gap(4).AlignItems(mui.Center).Children(func() {
				a.rightPanelName(c, "Files")
				if a.Files.Loading {
					mui.Spinner(c)
				}
				mui.Spacer(c)
				if a.fileIconButton(c, filePlusIcon, "New file", disabled) {
					a.openNewFile(false)
				}
				if a.fileIconButton(c, folderPlusIcon, "New folder", disabled) {
					a.openNewFile(true)
				}
				if a.fileIconButton(c, refreshIcon, "Refresh files", disabled || a.Files.Loading) {
					a.startFilesLoad(workspace, true)
				}
			})
			line, tip := a.fileHeaderLine()
			if line != "" {
				text := a.gitCaption(c, line).MinWidth(0)
				if tip != "" {
					text.Tooltip(tip)
				}
			}
		})
	})
}

func (a *App) fileHeaderLine() (string, string) {
	workspace := strings.TrimSpace(a.Files.Workspace)
	if workspace == "" || !filepath.IsAbs(workspace) {
		return "No workspace", ""
	}
	if a.Files.Busy != "" {
		return a.Files.Busy + "…", ""
	}
	switch {
	case !a.Files.Checked:
		return "Reading…", workspace
	case a.Files.Error != "" && !a.Files.Loaded[""]:
		return "Cannot read workspace", a.Files.Error
	}
	line := filepath.Base(workspace)
	if line == "" || line == "." || line == string(filepath.Separator) {
		line = workspace
	}
	tip := workspace
	if a.Files.RepoRoot != "" {
		if a.Files.Branch != "" {
			line += " · ⑂ " + truncate(a.Files.Branch, 18)
		}
		if count := len(a.Files.Status); count > 0 {
			line += " · " + formatCount(count) + " changed"
		}
	} else if !a.Files.Loading && a.Files.Checked {
		line += " · no Git"
		if a.Files.StatusError != "" {
			line += " (status unavailable)"
			tip += "\nGit status: " + a.Files.StatusError
		}
	}
	return truncate(line, 36), tip
}

// fileIconButton is the header's icon-only action. It uses buttonBase like
// the rail buttons and keeps a full-sentence label for tests and assistive
// technology.
func (a *App) fileIconButton(c *mui.Context, icon *mui.SVG, label string, disabled bool) bool {
	t := c.Theme()
	b := buttonBase(c, label).Size(22, 22).Center().Tooltip(label).Disabled(disabled)
	fill := t.Surface
	switch {
	case b.Pressed():
		fill = t.SurfacePressed
	case b.Hovered():
		fill = t.SurfaceHover
	}
	b.Background(fill)
	fg := t.TextMuted
	if disabled {
		fg = fg.Alpha(0.5)
	}
	b.Children(func() {
		mui.Icon(c, icon).Size(14, 14).TextColor(fg)
	})
	return b.Clicked()
}

func (a *App) filePanelBody(c *mui.Context) {
	t := c.Theme()
	workspace := strings.TrimSpace(a.Files.Workspace)
	if workspace == "" || !filepath.IsAbs(workspace) {
		mui.Column(c).Grow(1).Center().Gap(8).Padding(16, 16).Children(func() {
			mui.Icon(c, folderTreeIcon).Size(22, 22).TextColor(t.TextMuted)
			mui.Text(c, "No workspace").FontSize(a.gitFont(12)).Bold()
			mutedText(c, "Pick a workspace to browse its files.", a.gitFont(10))
		})
		return
	}
	if !a.Files.Loaded[""] {
		if a.Files.Error != "" {
			mui.Column(c).Grow(1).Padding(14, 14).Gap(8).Children(func() {
				mui.Text(c, "Cannot read this workspace").FontSize(a.gitFont(13)).Bold().TextColor(t.Danger)
				mui.Text(c, a.Files.Error).FontSize(a.gitFont(10)).TextColor(t.Danger).
					Selectable().Tooltip(a.Files.Error)
				if quietButton(c, "Refresh files").Label("Refresh files").FontSize(a.gitFont(10)).Clicked() {
					a.startFilesLoad(workspace, true)
				}
			})
			return
		}
		mui.Column(c).Grow(1).Center().Gap(8).Children(func() {
			mui.Spinner(c)
			mutedText(c, "Reading files…", a.gitFont(10))
		})
		return
	}
	a.fileFeedback(c)
	a.fileTree(c)
}

func (a *App) fileFeedback(c *mui.Context) {
	t := c.Theme()
	if a.Files.Error != "" {
		mui.Text(c, "⚠ "+truncate(a.Files.Error, 100)).
			FontSize(a.gitFont(9)).TextColor(t.Danger).SingleLine().
			Tooltip(a.Files.Error).Padding(3, 12, 1, 12)
	}
	if a.Files.StatusError != "" {
		message := "Git status: " + a.Files.StatusError
		mui.Text(c, "⚠ "+truncate(message, 100)).
			FontSize(a.gitFont(9)).TextColor(t.Warning).SingleLine().
			Tooltip(message).Padding(3, 12, 1, 12)
	}
	if a.Files.Notice != "" {
		mui.Text(c, a.Files.Notice).FontSize(a.gitFont(9)).TextColor(t.Success).
			SingleLine().Padding(3, 12, 1, 12)
	}
}

func (a *App) fileTree(c *mui.Context) {
	t := c.Theme()
	scroll := mui.Scroll(c).Grow(1)
	// Dropping on the empty area moves the entry to the workspace root.
	if value, ok := mui.Drop[fileDragValue](scroll); ok {
		a.moveFileEntry(value, "")
	}
	if value, over := mui.DragOver[fileDragValue](scroll); over && a.canMoveFileTo(value, "") {
		scroll.Background(t.Accent.Alpha(0.04))
	}
	scroll.Children(func() {
		mui.Column(c).Gap(1).FillWidth().Padding(4, 0, 8, 0).
			Role(mui.RoleTree).Label("Workspace files").Children(func() {
			a.fileEntries(c, "", 0)
			if entries, ok := a.Files.Entries[""]; ok && len(entries) == 0 {
				mui.Column(c).Gap(2).Padding(8, 12).Children(func() {
					mutedText(c, "No files yet.", a.gitFont(10))
					mutedText(c, "Use New file or New folder above.", a.gitFont(9))
				})
			}
		})
	})
}

func (a *App) fileEntries(c *mui.Context, dir string, depth int) {
	if dir != "" && !a.Files.Loaded[dir] {
		a.ensureFileDir(dir)
		if message := a.Files.DirErrors[dir]; message != "" {
			mui.Text(c, "⚠ "+truncate(message, 80)).
				FontSize(a.gitFont(9)).TextColor(c.Theme().Danger).SingleLine().
				Tooltip(message).Padding(2, fileRowGutter, 2, fileIndent(depth))
			return
		}
		if a.Files.LoadingDirs[dir] {
			mui.Row(c).Gap(4).AlignItems(mui.Center).Padding(2, fileRowGutter, 2, fileIndent(depth)).Children(func() {
				mui.Spinner(c)
				mutedText(c, "Loading…", a.gitFont(9))
			})
		}
		return
	}
	entries := a.Files.Entries[dir]
	if len(entries) == 0 {
		if dir != "" {
			mutedText(c, "Empty", a.gitFont(9)).Padding(2, fileRowGutter, 2, fileIndent(depth))
		}
		return
	}
	for _, entry := range entries {
		a.fileRow(c, entry, depth)
		if entry.Dir && a.Files.Expanded[entry.Path] {
			a.fileEntries(c, entry.Path, depth+1)
		}
	}
}

func (a *App) fileRow(c *mui.Context, entry files.Entry, depth int) {
	t := c.Theme()
	expanded := entry.Dir && a.Files.Expanded[entry.Path]
	ignored := a.fileIgnored(entry.Path)
	tip := entry.Path
	if ignored {
		tip += "\nIgnored by Git"
	}
	// Ignored rows stay in the tree, dimmed, so a .gitignore entry is
	// recognisable where it applies instead of just vanishing from view.
	nameColor, iconColor := t.Text, t.TextMuted
	if ignored {
		nameColor = t.TextMuted.Alpha(0.75)
		iconColor = t.TextMuted.Alpha(0.55)
	}
	row := buttonBase(c, fileRowLabel(entry)).FillWidth().Gap(4).
		Padding(2, fileRowGutter, 2, fileIndent(depth)).Radius(6).
		Background(t.Surface).Tooltip(tip).Key(entry.Path).
		Role(mui.RoleTreeItem).Level(depth + 1)
	if entry.Dir {
		row.Expanded(expanded)
	}
	row.Children(func() {
		if entry.Dir {
			arrow := "▸"
			if expanded {
				arrow = "▾"
			}
			mutedText(c, arrow, a.gitFont(9)).Width(9).Shrink(0)
			icon := folderIcon
			if expanded {
				icon = folderOpenIcon
			}
			mui.Icon(c, icon).Size(a.gitFont(13), a.gitFont(13)).TextColor(iconColor).Shrink(0)
		} else {
			mui.Box(c).Width(9).Shrink(0)
			mui.Icon(c, fileIconFor(entry.Name)).Size(a.gitFont(13), a.gitFont(13)).TextColor(iconColor).Shrink(0)
		}
		mui.Text(c, entry.Name).FontSize(a.gitFont(10)).TextColor(nameColor).Grow(1).MinWidth(0).SingleLine()
		switch {
		case ignored:
			mui.Text(c, "ignored").FontSize(a.gitFont(8)).TextColor(t.TextMuted.Alpha(0.7)).
				Shrink(0).Tooltip("Ignored by Git")
		case entry.Dir:
			if a.fileDirDirty(entry.Path) {
				mui.Box(c).Size(5, 5).Radius(3).Background(t.Accent).Shrink(0).Tooltip("Contains changes")
			}
		default:
			if file, ok := a.Files.Status[entry.Path]; ok {
				if code := fileStatusCode(file); code != "" {
					mui.Text(c, code).Font("monospace").FontSize(a.gitFont(9)).
						TextColor(fileStatusColor(t, code)).Shrink(0).
						Tooltip(fileStatusTooltip(file))
				}
			}
		}
	})
	switch {
	case row.Pressed():
		row.Background(t.SurfacePressed)
	case row.Hovered():
		row.Background(t.SurfaceHover)
	}
	if entry.Dir && row.Clicked() {
		a.toggleFileFolder(entry.Path)
	}
	row.ContextMenu(func(m *mui.Menu) {
		if m.Item("Reveal in folder").Chosen() {
			a.revealFileEntry(entry)
		}
		busy := a.Files.Busy != ""
		if m.Item("Rename").Disabled(busy).Chosen() {
			a.openFileRename(entry)
		}
		if m.Item("Delete").Danger().Disabled(busy).Chosen() {
			a.requestFileDelete(entry)
		}
	})
	row.Drag(fileDragValue{Workspace: a.Files.Workspace, Path: entry.Path, Dir: entry.Dir})
	target := fileDropDir(entry)
	if value, ok := mui.Drop[fileDragValue](row); ok {
		a.moveFileEntry(value, target)
	}
	if value, over := mui.DragOver[fileDragValue](row); over && a.canMoveFileTo(value, target) {
		row.Background(t.Accent.Alpha(0.12))
	}
}

// fileIndent is a row's left inset: the panel gutter at the root, one gutter
// wider per nesting level so children read as belonging to their folder.
func fileIndent(depth int) float32 {
	if depth <= 0 {
		return fileRowGutter
	}
	return fileRowGutter + float32(depth)*12
}

func fileRowLabel(entry files.Entry) string {
	kind := "File"
	if entry.Dir {
		kind = "Folder"
	}
	return kind + " " + entry.Path
}

func fileDropDir(entry files.Entry) string {
	if entry.Dir {
		return entry.Path
	}
	return fileParent(entry.Path)
}

func fileParent(rel string) string {
	parent := path.Dir(rel)
	if parent == "." {
		return ""
	}
	return parent
}

func (a *App) fileDirDirty(dir string) bool {
	return a.Files.DirtyDirs[dir]
}

// fileIgnored reports whether Git ignores a workspace path, either directly or
// through an ignored parent folder: the tree can expand an ignored folder,
// and everything inside it is ignored too.
func (a *App) fileIgnored(rel string) bool {
	if a.Files.IgnoredAll {
		return true
	}
	for dir := rel; dir != ""; dir = fileParent(dir) {
		if a.Files.Ignored[dir] {
			return true
		}
	}
	return false
}

func (a *App) canMoveFileTo(value fileDragValue, destDir string) bool {
	if value.Path == "" || value.Workspace != a.Files.Workspace || a.Files.Busy != "" {
		return false
	}
	if fileParent(value.Path) == destDir {
		return false
	}
	if value.Dir && (destDir == value.Path || strings.HasPrefix(destDir, value.Path+"/")) {
		return false
	}
	return true
}

func (a *App) moveFileEntry(value fileDragValue, destDir string) {
	if !a.canMoveFileTo(value, destDir) {
		return
	}
	workspace := a.Files.Workspace
	source, isDir := value.Path, value.Dir
	a.runFileAction("Move", func(context.Context) (string, error) {
		return files.Move(workspace, source, destDir)
	}, func(newPath string) {
		if destDir != "" {
			a.Files.Expanded[destDir] = true
		}
		if isDir {
			a.rebaseFileExpanded(source, newPath)
		}
	})
}

// runFileAction runs one filesystem change off the UI thread. A generation
// and workspace check drop a result that belongs to a tree the user already
// left; success reloads the root and marks the Git tab stale.
func (a *App) runFileAction(label string, fn func(context.Context) (string, error), after func(string)) {
	workspace := a.Files.Workspace
	if a.closed.Load() || workspace == "" || !filepath.IsAbs(workspace) || a.Files.Busy != "" {
		return
	}
	gen := a.Files.Gen
	a.Files.Busy = label
	a.Files.Error = ""
	a.Files.Notice = ""
	go func() {
		ctx, cancel := context.WithTimeout(a.lifeCtx, fileActionTimeout)
		defer cancel()
		result, err := fn(ctx)
		a.Post(func() {
			if a.closed.Load() || a.Files.Gen != gen || a.Files.Workspace != workspace {
				return
			}
			a.Files.Busy = ""
			if err != nil {
				a.Files.Error = label + ": " + fileErrorText(err)
				return
			}
			if after != nil {
				after(result)
			}
			a.Files.Notice = fileNoticeFor(label)
			if sameWorkspace(a.Git.Workspace, workspace) {
				a.Git.Checked = false
			}
			a.startFilesLoad(workspace, false)
		})
	}()
}

func fileNoticeFor(label string) string {
	switch label {
	case "New file":
		return "File created."
	case "New folder":
		return "Folder created."
	case "Rename":
		return "Renamed."
	case "Move":
		return "Moved."
	case "Delete":
		return "Deleted."
	default:
		return "Done."
	}
}

func fileErrorText(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	}
	return strings.TrimSpace(err.Error())
}

func (a *App) revealFileEntry(entry files.Entry) {
	target, err := files.Resolve(a.Files.Workspace, entry.Path)
	if err != nil {
		a.Files.Error = "Reveal: " + fileErrorText(err)
		return
	}
	if a.revealItem != nil {
		a.revealItem(target)
	}
}

func (a *App) openFileRename(entry files.Entry) {
	if a.Files.Busy != "" {
		return
	}
	a.closeFileDialogs()
	a.Files.RenameOpen = true
	a.Files.RenamePath = entry.Path
	a.Files.RenameDir = entry.Dir
	a.Files.RenameName = entry.Name
	a.Files.RenameErr = ""
}

func (a *App) requestFileDelete(entry files.Entry) {
	if a.Files.Busy != "" {
		return
	}
	a.closeFileDialogs()
	a.Files.DeleteOpen = true
	a.Files.DeletePath = entry.Path
	a.Files.DeleteDir = entry.Dir
}

func (a *App) openNewFile(directory bool) {
	if a.Files.Busy != "" {
		return
	}
	a.closeFileDialogs()
	a.Files.CreateOpen = true
	a.Files.CreateDir = directory
	a.Files.CreateName = ""
	a.Files.CreateErr = ""
}

func (a *App) closeFileDialogs() {
	a.Files.CreateOpen = false
	a.Files.CreateDir = false
	a.Files.CreateName = ""
	a.Files.CreateErr = ""

	a.Files.RenameOpen = false
	a.Files.RenamePath = ""
	a.Files.RenameDir = false
	a.Files.RenameName = ""
	a.Files.RenameErr = ""

	a.Files.DeleteOpen = false
	a.Files.DeletePath = ""
	a.Files.DeleteDir = false
}

func (a *App) fileCreateDialog(c *mui.Context) {
	if !a.Files.CreateOpen {
		return
	}
	t := c.Theme()
	heading, label := "New file", "New file name"
	if a.Files.CreateDir {
		heading, label = "New folder", "New folder name"
	}
	workspace := strings.TrimSpace(a.Files.Workspace)
	target := "Workspace root"
	if workspace != "" {
		target = workspace
	}
	mui.Modal(c, &a.Files.CreateOpen, func() {
		mui.Column(c).Gap(9).Width(380).Children(func() {
			mui.Text(c, heading).FontSize(a.workspaceFont(16)).Bold()
			mui.Text(c, "Create in "+target).FontSize(a.workspaceFont(10)).
				TextColor(t.TextMuted).Selectable()
			mui.TextInput(c, &a.Files.CreateName).Label(label).Placeholder("name")
			if a.Files.CreateErr != "" {
				mui.Text(c, "⚠ "+a.Files.CreateErr).FontSize(a.workspaceFont(10)).TextColor(t.Danger)
			}
			mui.Row(c).Gap(8).Justify(mui.End).Children(func() {
				if quietButton(c, "Cancel").Clicked() {
					a.closeFileDialogs()
				}
				if primaryButton(c, "Create").Clicked() {
					a.submitFileCreate()
				}
			})
		})
	})
	if !a.Files.CreateOpen {
		a.closeFileDialogs()
	}
}

func (a *App) submitFileCreate() {
	workspace := strings.TrimSpace(a.Files.Workspace)
	if workspace == "" || !filepath.IsAbs(workspace) {
		a.Files.CreateErr = "The workspace is no longer available."
		return
	}
	name, err := files.CleanName(a.Files.CreateName)
	if err != nil {
		a.Files.CreateErr = err.Error()
		return
	}
	target, err := files.Resolve(workspace, name)
	if err != nil {
		a.Files.CreateErr = fileErrorText(err)
		return
	}
	if _, err := os.Lstat(target); err == nil {
		a.Files.CreateErr = files.ErrExists.Error()
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		a.Files.CreateErr = fileErrorText(err)
		return
	}
	directory := a.Files.CreateDir
	label := "New file"
	if directory {
		label = "New folder"
	}
	a.Files.CreateOpen = false
	a.runFileAction(label, func(context.Context) (string, error) {
		return files.Create(workspace, "", name, directory)
	}, func(newPath string) {
		if directory {
			a.Files.Expanded[newPath] = true
		}
	})
}

func (a *App) fileRenameDialog(c *mui.Context) {
	if !a.Files.RenameOpen {
		return
	}
	t := c.Theme()
	heading, label := "Rename file", "File name"
	if a.Files.RenameDir {
		heading, label = "Rename folder", "Folder name"
	}
	mui.Modal(c, &a.Files.RenameOpen, func() {
		mui.Column(c).Gap(9).Width(380).Children(func() {
			mui.Text(c, heading).FontSize(a.workspaceFont(16)).Bold()
			mui.Text(c, a.Files.RenamePath).FontSize(a.workspaceFont(10)).
				TextColor(t.TextMuted).Selectable().Tooltip(a.Files.RenamePath)
			mui.TextInput(c, &a.Files.RenameName).Label(label).Placeholder("name")
			if a.Files.RenameErr != "" {
				mui.Text(c, "⚠ "+a.Files.RenameErr).FontSize(a.workspaceFont(10)).TextColor(t.Danger)
			}
			mui.Row(c).Gap(8).Justify(mui.End).Children(func() {
				if quietButton(c, "Cancel").Clicked() {
					a.closeFileDialogs()
				}
				if primaryButton(c, "Save").Clicked() {
					a.submitFileRename()
				}
			})
		})
	})
	if !a.Files.RenameOpen {
		a.closeFileDialogs()
	}
}

func (a *App) submitFileRename() {
	workspace := strings.TrimSpace(a.Files.Workspace)
	if workspace == "" || !filepath.IsAbs(workspace) {
		a.Files.RenameErr = "The workspace is no longer available."
		return
	}
	source := a.Files.RenamePath
	if source == "" {
		a.Files.RenameErr = "Choose a file or folder first."
		return
	}
	name, err := files.CleanName(a.Files.RenameName)
	if err != nil {
		a.Files.RenameErr = err.Error()
		return
	}
	if name == path.Base(source) {
		a.closeFileDialogs()
		return
	}
	target, err := files.Resolve(workspace, path.Join(fileParent(source), name))
	if err != nil {
		a.Files.RenameErr = fileErrorText(err)
		return
	}
	if _, err := os.Lstat(target); err == nil {
		a.Files.RenameErr = files.ErrExists.Error()
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		a.Files.RenameErr = fileErrorText(err)
		return
	}
	isDir := a.Files.RenameDir
	a.Files.RenameOpen = false
	a.runFileAction("Rename", func(context.Context) (string, error) {
		return files.Rename(workspace, source, name)
	}, func(newPath string) {
		if isDir {
			a.rebaseFileExpanded(source, newPath)
		}
	})
}

func (a *App) fileDeleteDialog(c *mui.Context) {
	if !a.Files.DeleteOpen {
		return
	}
	t := c.Theme()
	target := a.Files.DeletePath
	title, button := "Delete file?", "Delete file"
	hint := "The file is deleted from disk. This cannot be undone."
	if a.Files.DeleteDir {
		title, button = "Delete folder?", "Delete folder"
		hint = "The folder and everything inside it are deleted from disk. This cannot be undone."
	}
	mui.Modal(c, &a.Files.DeleteOpen, func() {
		mui.Column(c).Gap(10).Width(380).Children(func() {
			mui.Text(c, title).FontSize(a.workspaceFont(16)).Bold()
			mui.Text(c, target).FontSize(a.workspaceFont(12)).Bold().SingleLine().Tooltip(target)
			mui.Text(c, hint).FontSize(a.workspaceFont(10)).TextColor(t.TextMuted)
			mui.Row(c).Gap(8).Justify(mui.End).Children(func() {
				if quietButton(c, "Cancel").Clicked() {
					a.closeFileDialogs()
				}
				if dangerButton(c, button).Clicked() {
					a.submitFileDelete()
				}
			})
		})
	})
	if !a.Files.DeleteOpen {
		a.closeFileDialogs()
	}
}

func (a *App) submitFileDelete() {
	workspace := strings.TrimSpace(a.Files.Workspace)
	target := a.Files.DeletePath
	if workspace == "" || !filepath.IsAbs(workspace) || target == "" {
		a.closeFileDialogs()
		return
	}
	a.Files.DeleteOpen = false
	a.runFileAction("Delete", func(context.Context) (string, error) {
		return "", files.Remove(workspace, target)
	}, func(string) {
		a.clearFileExpanded(target)
	})
}

func (a *App) rebaseFileExpanded(oldRel, newRel string) {
	if oldRel == "" || newRel == "" {
		return
	}
	next := make(map[string]bool, len(a.Files.Expanded))
	for key, expanded := range a.Files.Expanded {
		switch {
		case key == oldRel:
			next[newRel] = expanded
		case strings.HasPrefix(key, oldRel+"/"):
			next[newRel+strings.TrimPrefix(key, oldRel)] = expanded
		default:
			next[key] = expanded
		}
	}
	a.Files.Expanded = next
}

func (a *App) clearFileExpanded(dir string) {
	if dir == "" {
		return
	}
	for key := range a.Files.Expanded {
		if key == dir || strings.HasPrefix(key, dir+"/") {
			delete(a.Files.Expanded, key)
		}
	}
}

// fileStatusCode is the one letter the tree shows: untracked is U, conflicts
// are !, and a working-tree change wins over the staged side because it is the
// file's current state. The tooltip still names both sides.
func fileStatusCode(file git.File) string {
	switch {
	case file.Conflict():
		return "!"
	case file.Index == '?':
		return "U"
	case file.Worktree != ' ' && file.Worktree != '?':
		return string(file.Worktree)
	case file.Index != ' ' && file.Index != '?':
		return string(file.Index)
	default:
		return ""
	}
}

func fileStatusColor(t *mui.Theme, code string) mui.Color {
	switch code {
	case "A", "U":
		return t.Success
	case "D", "!":
		return t.Danger
	case "M", "T":
		return t.Warning
	case "R", "C":
		return t.Accent
	default:
		return t.TextMuted
	}
}

func fileStatusTooltip(file git.File) string {
	if file.Conflict() {
		return "Merge conflict"
	}
	if file.Index == '?' {
		return "Untracked"
	}
	var parts []string
	if file.Index != ' ' && file.Index != '?' {
		parts = append(parts, "Staged: "+fileStatusWord(file.Index))
	}
	if file.Worktree != ' ' && file.Worktree != '?' {
		parts = append(parts, "Unstaged: "+fileStatusWord(file.Worktree))
	}
	if len(parts) == 0 {
		return "Unchanged"
	}
	if file.OrigPath != "" {
		parts = append(parts, "from "+file.OrigPath)
	}
	return strings.Join(parts, " · ")
}

func fileStatusWord(code byte) string {
	switch code {
	case 'A':
		return "Added"
	case 'M':
		return "Modified"
	case 'D':
		return "Deleted"
	case 'R':
		return "Renamed"
	case 'C':
		return "Copied"
	case 'T':
		return "Type changed"
	case 'U':
		return "Unmerged"
	default:
		return fmt.Sprintf("%c", code)
	}
}
