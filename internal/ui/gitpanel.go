package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	crycode "github.com/ELY-Open-Source/CrychicUI/code"
	cryfeedback "github.com/ELY-Open-Source/CrychicUI/feedback"
	crykit "github.com/ELY-Open-Source/CrychicUI/kit"
	crylayout "github.com/ELY-Open-Source/CrychicUI/layout"
	cryoverlay "github.com/ELY-Open-Source/CrychicUI/overlay"
	mui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/reed/acp-gui/internal/git"
)

const (
	gitPanelWidth           = 300
	defaultGitChangesHeight = 260
	gitStatusTimeout        = 10 * time.Second
	gitCommitTimeout        = 30 * time.Second
	gitNetworkTimeout       = 60 * time.Second
	gitActionTimeout        = 15 * time.Second
)

// gitPanelState is the UI-thread copy of the active workspace's repository.
// Background reads and actions only post results back; a generation and the
// workspace guard them so a late result cannot replace a newer workspace.
type gitPanelState struct {
	Workspace string

	Checked   bool
	Available bool
	NoRepo    bool
	Loading   bool
	Busy      string
	Error     string
	Notice    string

	RepoRoot   string
	Branch     string
	Ahead      int
	Behind     int
	HasCommits bool
	Staged     []git.File
	Changes    []git.File
	Commits    []git.LogCommit

	// CommitOpen is the commit whose changed files show under its graph
	// row. CommitFilesGen guards a late read against another commit, a
	// workspace change, or a collapsed row.
	CommitOpen      string
	CommitFiles     []git.CommitFile
	CommitFilesBusy bool
	CommitFilesErr  string
	CommitFilesGen  int

	Message       string
	ChangesHeight float32
	Gen           int

	// DiscardOpen asks before throwing away every change in the repository.
	DiscardOpen bool

	// Preview is the diff overlay for one changed file; PreviewGen guards
	// a late read against a newer file or workspace.
	PreviewOpen   bool
	PreviewPath   string
	PreviewCommit string
	PreviewStaged bool
	PreviewBusy   bool
	PreviewErr    string
	PreviewOld    string
	PreviewNew    string
	PreviewGen    int
}

func (a *App) activeGitWorkspace() string {
	if chat := a.activeChat(); chat != nil {
		return strings.TrimSpace(chat.Workspace)
	}
	return strings.TrimSpace(a.Workspace)
}

// ensureGitPanel starts one read when the panel opens or follows a chat to a
// new workspace. Checked keeps the view from spawning git every frame.
func (a *App) ensureGitPanel() {
	workspace := a.activeGitWorkspace()
	if a.Git.Workspace != workspace {
		height := a.Git.ChangesHeight
		if height <= 0 {
			height = defaultGitChangesHeight
		}
		a.Git = gitPanelState{Workspace: workspace, ChangesHeight: height}
	}
	if workspace == "" || !filepath.IsAbs(workspace) {
		a.Git.Checked = true
		a.Git.Loading = false
		return
	}
	if a.Git.Checked || a.Git.Loading {
		return
	}
	a.startGitLoad(workspace, false)
}

// refreshGitOnFocus rereads the repository when the window gains focus, so
// changes made in another app show up without pressing Refresh. It leaves a
// load or an action already running alone, and only reads while the panel is
// open.
func (a *App) refreshGitOnFocus() {
	if a.closed.Load() || !a.GitPanelOpen {
		return
	}
	workspace := a.activeGitWorkspace()
	if workspace == "" || !filepath.IsAbs(workspace) {
		return
	}
	if a.Git.Workspace != workspace || !a.Git.Checked {
		a.ensureGitPanel()
		return
	}
	if a.Git.Loading || a.Git.Busy != "" {
		return
	}
	a.startGitLoad(workspace, false)
}

// refreshGitAfterTurn rereads the repository when an agent turn ends, so the
// files the agent just wrote show up without pressing Refresh. Only the chat
// on screen matters because the panel follows the active workspace; a hidden
// panel is just marked stale and reloads when it is shown again.
func (a *App) refreshGitAfterTurn(chat *Chat) {
	if a.closed.Load() || chat == nil || chat != a.activeChat() {
		return
	}
	workspace := strings.TrimSpace(chat.Workspace)
	if workspace == "" || !filepath.IsAbs(workspace) {
		return
	}
	if !a.GitPanelOpen {
		if a.Git.Workspace == workspace {
			a.Git.Checked = false
		}
		return
	}
	a.refreshGitOnFocus()
}

func (a *App) startGitLoad(workspace string, clearFeedback bool) {
	if a.closed.Load() {
		return
	}
	a.Git.Gen++
	gen := a.Git.Gen
	a.Git.Workspace = workspace
	a.Git.Loading = true
	a.Git.Busy = ""
	if clearFeedback {
		a.Git.Error = ""
		a.Git.Notice = ""
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifeCtx, gitStatusTimeout)
		defer cancel()

		available := git.Available()
		noRepo := false
		var state git.State
		var err error
		if available {
			info, statErr := os.Stat(workspace)
			switch {
			case statErr != nil:
				err = fmt.Errorf("workspace: %w", statErr)
			case !info.IsDir():
				err = errors.New("workspace is not a directory")
			default:
				state, err = git.Load(ctx, workspace)
				switch {
				case errors.Is(err, git.ErrNotRepository):
					noRepo, err = true, nil
				case errors.Is(err, git.ErrGitNotFound):
					available, err = false, nil
				}
			}
		}

		a.Post(func() {
			if a.closed.Load() || a.Git.Gen != gen || a.Git.Workspace != workspace {
				return
			}
			a.Git.Checked = true
			a.Git.Loading = false
			a.Git.Available = available
			if !available {
				a.Git.NoRepo = false
				a.Git.RepoRoot = ""
				a.Git.Branch = ""
				a.Git.Ahead, a.Git.Behind = 0, 0
				a.Git.HasCommits = false
				a.Git.Staged, a.Git.Changes = nil, nil
				a.Git.Commits = nil
				return
			}
			if noRepo {
				a.Git.NoRepo = true
				a.Git.RepoRoot = ""
				a.Git.Branch = ""
				a.Git.Ahead, a.Git.Behind = 0, 0
				a.Git.HasCommits = false
				a.Git.Staged, a.Git.Changes = nil, nil
				a.Git.Commits = nil
				if clearFeedback {
					a.Git.Error = ""
				}
				return
			}
			if err != nil {
				a.Git.NoRepo = false
				a.Git.Error = gitErrorText(err)
				return
			}
			a.Git.NoRepo = false
			a.Git.RepoRoot = state.Root
			a.Git.Branch = state.Branch
			a.Git.Ahead = state.Ahead
			a.Git.Behind = state.Behind
			a.Git.HasCommits = state.HasCommits
			a.Git.Staged = state.Staged()
			a.Git.Changes = state.Changes()
			a.Git.Commits = state.Commits
			a.pruneCommitFiles()
		})
	}()
}

func (a *App) runGitAction(label string, requireRepo bool, fn func(context.Context, string) error) {
	workspace := a.activeGitWorkspace()
	if workspace == "" || !filepath.IsAbs(workspace) || a.Git.Workspace != workspace {
		return
	}
	if !a.Git.Available || a.Git.Loading || a.Git.Busy != "" {
		return
	}
	target := workspace
	if requireRepo {
		if a.Git.RepoRoot == "" {
			return
		}
		target = a.Git.RepoRoot
	}
	a.Git.Gen++
	gen := a.Git.Gen
	a.Git.Busy = label
	a.Git.Error = ""
	a.Git.Notice = ""
	go func() {
		ctx, cancel := context.WithTimeout(a.lifeCtx, gitTimeoutFor(label))
		defer cancel()
		err := fn(ctx, target)
		a.Post(func() {
			if a.closed.Load() || a.Git.Gen != gen || a.Git.Workspace != workspace {
				return
			}
			a.Git.Busy = ""
			if err != nil {
				a.Git.Error = label + ": " + gitErrorText(err)
			} else {
				a.Git.Notice = gitNoticeFor(label)
				if label == "Commit" {
					a.Git.Message = ""
				}
			}
			a.startGitLoad(workspace, false)
		})
	}()
}

func gitTimeoutFor(label string) time.Duration {
	switch label {
	case "Fetch", "Pull", "Push":
		return gitNetworkTimeout
	case "Commit", "Discard all":
		return gitCommitTimeout
	default:
		return gitActionTimeout
	}
}

func gitNoticeFor(label string) string {
	switch label {
	case "Initialize repository":
		return "Repository initialized."
	case "Commit":
		return "Committed."
	case "Fetch":
		return "Fetched."
	case "Pull":
		return "Pulled."
	case "Push":
		return "Pushed."
	case "Undo last commit":
		return "Last commit undone; changes stay staged."
	case "Discard all":
		return "Discarded all changes."
	default:
		return ""
	}
}

func gitErrorText(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "git timed out"
	}
	if errors.Is(err, context.Canceled) {
		return "git cancelled"
	}
	return strings.TrimSpace(err.Error())
}

func (a *App) gitPanel(c *mui.Context) {
	a.ensureGitPanel()
	t := c.Theme()
	mui.Column(c).Width(gitPanelWidth).FillHeight().
		Background(t.Surface).
		BorderWidth(1, 0, 0, 0).BorderColor(t.Border).
		Children(func() {
			a.gitPanelHeader(c)
			a.gitPanelBody(c)
		})
}

func (a *App) gitPanelHeader(c *mui.Context) {
	t := c.Theme()
	mui.Row(c).Height(54).Gap(6).Padding(8, 12).AlignItems(mui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		mui.Column(c).Gap(1).Grow(1).Children(func() {
			mui.Row(c).Gap(5).AlignItems(mui.Center).Children(func() {
				mui.Text(c, "Git").FontSize(14).Bold()
				if a.Git.Loading {
					mui.Spinner(c)
				}
				if a.Git.Busy != "" {
					caption(c, a.Git.Busy+"…")
				}
			})
			line, tip := a.gitHeaderLine()
			if line != "" {
				text := caption(c, line)
				if tip != "" {
					text.Tooltip(tip)
				}
			}
		})
	})
}

func (a *App) gitHeaderLine() (string, string) {
	if a.Git.Busy != "" {
		return "", ""
	}
	switch {
	case !a.Git.Checked:
		return "Reading…", ""
	case !a.Git.Available:
		return "git not found", ""
	case a.Git.NoRepo:
		return "No repository", ""
	case a.Git.Error != "" && a.Git.RepoRoot == "":
		return "Error", a.Git.Error
	case a.Git.RepoRoot != "":
		label := filepath.Base(a.Git.RepoRoot)
		if label == "" || label == "." || label == string(filepath.Separator) {
			label = a.Git.RepoRoot
		}
		line := truncate(label, 18)
		if a.Git.Branch != "" {
			line += " · ⑂ " + truncate(a.Git.Branch, 18)
		}
		if a.Git.Ahead > 0 || a.Git.Behind > 0 {
			line += " ↑" + strconv.Itoa(a.Git.Ahead) + " ↓" + strconv.Itoa(a.Git.Behind)
		}
		return line, a.Git.RepoRoot
	default:
		return "Reading…", ""
	}
}

func (a *App) gitPanelBody(c *mui.Context) {
	if !a.Git.Checked {
		a.gitLoading(c)
		return
	}
	if !a.Git.Available {
		a.gitUnavailable(c)
		return
	}
	if a.Git.RepoRoot == "" {
		switch {
		case a.Git.NoRepo:
			a.gitNoRepository(c)
		case a.Git.Error != "":
			a.gitInitialError(c)
		default:
			a.gitLoading(c)
		}
		return
	}
	a.gitCommitBox(c)
	a.gitFeedback(c)
	a.gitHistorySplit(c)
}

func (a *App) gitLoading(c *mui.Context) {
	mui.Column(c).Grow(1).Center().Gap(8).Children(func() {
		mui.Spinner(c)
		mutedText(c, "Reading repository…", 10)
	})
}

func (a *App) gitUnavailable(c *mui.Context) {
	t := c.Theme()
	workspace := a.Git.Workspace
	mui.Column(c).Grow(1).Padding(14, 14).Gap(8).Children(func() {
		mui.Text(c, "Git is not installed.").FontSize(13).Bold().TextColor(t.Warning)
		mutedText(c, "Install Git, make sure it is on PATH, then refresh.", 10)
		if quietButton(c, "Refresh").FontSize(10).Clicked() {
			a.startGitLoad(workspace, true)
		}
	})
}

func (a *App) gitNoRepository(c *mui.Context) {
	workspace := a.Git.Workspace
	busy := a.Git.Busy != "" || a.Git.Loading
	mui.Column(c).Grow(1).Padding(14, 14).Gap(8).Children(func() {
		mui.Text(c, "Not a Git repository").FontSize(13).Bold()
		mutedText(c, "This workspace is not inside a Git repository.", 10)
		mutedText(c, workspace, 9).Selectable()
		button := primaryButton(c, "Initialize Git repository").FontSize(10)
		button.Disabled(busy)
		button.Tooltip("Run git init in this workspace")
		if button.Clicked() && !busy {
			a.runGitAction("Initialize repository", false, func(ctx context.Context, dir string) error {
				return git.Init(ctx, dir)
			})
		}
		if a.Git.Busy != "" {
			mui.Row(c).Gap(6).AlignItems(mui.Center).Children(func() {
				mui.Spinner(c)
				caption(c, a.Git.Busy+"…")
			})
		}
		if a.Git.Error != "" {
			a.gitFeedback(c)
		}
	})
}

func (a *App) gitInitialError(c *mui.Context) {
	t := c.Theme()
	workspace := a.Git.Workspace
	mui.Column(c).Grow(1).Padding(14, 14).Gap(8).Children(func() {
		mui.Text(c, "Git unavailable").FontSize(13).Bold().TextColor(t.Danger)
		message := truncate(singleLine(a.Git.Error), 240)
		mui.Text(c, message).FontSize(10).TextColor(t.Danger).Selectable().Tooltip(a.Git.Error)
		if quietButton(c, "Refresh").FontSize(10).Clicked() {
			a.startGitLoad(workspace, true)
		}
	})
}

func (a *App) gitCommitBox(c *mui.Context) {
	t := c.Theme()
	busy := a.Git.Busy != "" || a.Git.Loading
	staged := len(a.Git.Staged)
	mui.Column(c).Gap(6).Padding(6, 12, 6, 12).Children(func() {
		mui.TextAreaBase(c, &a.Git.Message).
			Label("Commit message").
			Placeholder("Commit message").
			Lines(2, 4).
			Padding(5, 7).
			Radius(t.Radius).
			Border(1, t.Border).
			Background(t.Background).
			Disabled(busy)
		button := primaryButton(c, "Commit").FontSize(10).FillWidth()
		disabled := busy || staged == 0 || strings.TrimSpace(a.Git.Message) == ""
		button.Disabled(disabled)
		button.Tooltip("Commit the staged changes")
		if button.Clicked() && !disabled {
			message := strings.TrimSpace(a.Git.Message)
			a.runGitAction("Commit", true, func(ctx context.Context, root string) error {
				return git.Commit(ctx, root, message)
			})
		}
	})
}

func (a *App) gitFeedback(c *mui.Context) {
	t := c.Theme()
	if a.Git.Error == "" && a.Git.Notice == "" {
		return
	}
	mui.Column(c).Gap(2).Padding(3, 12, 3, 12).Children(func() {
		if a.Git.Error != "" {
			message := truncate(singleLine(a.Git.Error), 240)
			mui.Text(c, "⚠ "+message).FontSize(9).TextColor(t.Danger).Selectable().Tooltip(a.Git.Error)
		}
		if a.Git.Notice != "" {
			mui.Text(c, a.Git.Notice).FontSize(9).TextColor(t.Success)
		}
	})
}

func (a *App) gitHistorySplit(c *mui.Context) {
	mui.SplitVertical(c, &a.Git.ChangesHeight, func() {
		a.gitChanges(c)
	}, func() {
		a.gitGraph(c)
	}).Grow(1)
}

func (a *App) gitChanges(c *mui.Context) {
	staged := a.Git.Staged
	changes := a.Git.Changes
	mui.Column(c).FillHeight().Padding(6, 12, 4, 12).Children(func() {
		mui.Scroll(c).Grow(1).Children(func() {
			// Leave room for the overlay scrollbar so it never covers the
			// stage/unstage button of a row.
			mui.Column(c).Gap(3).Padding(0, 10, 0, 0).Children(func() {
				a.gitSectionHeader(c, "Staged", len(staged), gitSectionAction{
					label: "Unstage all",
					tip:   "Unstage all files in this section",
					show:  len(staged) > 0,
					fn: func() {
						a.runGitAction("Unstage all", true, func(ctx context.Context, root string) error {
							return git.UnstageAll(ctx, root)
						})
					},
				})
				if len(staged) == 0 {
					caption(c, "No staged changes")
				} else {
					for _, file := range staged {
						a.gitFileRow(c, file, true)
					}
				}

				mui.Box(c).Height(6)
				a.gitSectionHeader(c, "Changes", len(changes),
					gitSectionAction{
						label:  "Discard all",
						tip:    "Discard every staged and unstaged change; asks first",
						danger: true,
						show:   len(staged) > 0 || len(changes) > 0,
						fn:     func() { a.Git.DiscardOpen = true },
					},
					gitSectionAction{
						label: "Stage all",
						tip:   "Stage all files in this section",
						show:  len(changes) > 0,
						fn: func() {
							a.runGitAction("Stage all", true, func(ctx context.Context, root string) error {
								return git.StageAll(ctx, root)
							})
						},
					},
				)
				if len(changes) == 0 {
					caption(c, "No changes")
				} else {
					for _, file := range changes {
						a.gitFileRow(c, file, false)
					}
				}
			})
		})
	})
}

// gitSectionAction is one button in a section header. A danger action uses the
// destructive text color; show hides the button while it has nothing to act on.
type gitSectionAction struct {
	label  string
	tip    string
	danger bool
	show   bool
	fn     func()
}

func (a *App) gitSectionHeader(c *mui.Context, title string, count int, actions ...gitSectionAction) {
	busy := a.Git.Busy != "" || a.Git.Loading
	mui.Row(c).Gap(4).AlignItems(mui.Center).Children(func() {
		sectionLabel(c, title)
		mutedText(c, strconv.Itoa(count), 8)
		mui.Spacer(c)
		for _, action := range actions {
			if !action.show {
				continue
			}
			var button mui.Element
			if action.danger {
				button = dangerButton(c, action.label)
			} else {
				button = quietButton(c, action.label)
			}
			button.FontSize(8)
			button.Disabled(busy)
			button.Tooltip(action.tip)
			if button.Clicked() && !busy {
				action.fn()
			}
		}
	})
}

func (a *App) gitFileRow(c *mui.Context, file git.File, staged bool) {
	t := c.Theme()
	code := file.WorktreeCode()
	if staged {
		code = file.IndexCode()
	}
	color := t.TextMuted
	switch {
	case code == "!" || code == "D":
		color = t.Danger
	case code == "A" || code == "??":
		color = t.Success
	case code == "M" || code == "R":
		color = t.Warning
	}
	path := file.Path
	name := filepath.Base(path)
	dir := filepath.Dir(path)
	if dir == "." {
		dir = ""
	}
	busy := a.Git.Busy != "" || a.Git.Loading

	mui.Row(c).Gap(4).AlignItems(mui.Center).Children(func() {
		mui.Text(c, code).Font("monospace").FontSize(8).TextColor(color)
		preview := mui.ButtonBase(c).Gap(5).Grow(1).Padding(1, 2).Radius(4).
			Justify(mui.Start).Label("Diff of " + path)
		hover := float32(0)
		if preview.Hovered() {
			hover = 1
		}
		preview.Background(preview.AnimateColor("bg", t.SurfaceHover.Alpha(hover), mui.Snappy))
		preview.Children(func() {
			mui.Text(c, truncate(name, 24)).FontSize(10).SingleLine()
			if dir != "" {
				mutedText(c, truncate(dir, 20), 8).SingleLine()
			}
		})
		if preview.Clicked() {
			a.openGitPreview(file, staged)
		}
		label, action := "＋", "Stage "
		if staged {
			label, action = "－", "Unstage "
		}
		button := quietButton(c, label).Label(action + path).FontSize(9)
		button.Disabled(busy)
		if button.Clicked() && !busy {
			if staged {
				a.runGitAction("Unstage", true, func(ctx context.Context, root string) error {
					return git.Unstage(ctx, root, path)
				})
			} else {
				a.runGitAction("Stage", true, func(ctx context.Context, root string) error {
					return git.Stage(ctx, root, path)
				})
			}
		}
	})
}

// openGitPreview shows a staged or unstaged file against the index or HEAD.
func (a *App) openGitPreview(file git.File, staged bool) {
	a.startGitPreview(file.Path, "", staged, func(ctx context.Context, root string) (string, string, error) {
		return git.Diff(ctx, root, file, staged)
	})
}

// openGitCommitPreview shows one file as a commit changed it, against the
// commit's first parent, as the expanded commit list lists it.
func (a *App) openGitCommitPreview(hash string, file git.CommitFile) {
	a.startGitPreview(file.Path, hash, false, func(ctx context.Context, root string) (string, string, error) {
		return git.CommitDiff(ctx, root, hash, file)
	})
}

// startGitPreview reads both sides off the UI thread and opens the overlay;
// a newer click, another commit or another workspace wins over a late read.
func (a *App) startGitPreview(path, commit string, staged bool, load func(context.Context, string) (string, string, error)) {
	workspace, root := a.activeGitWorkspace(), a.Git.RepoRoot
	if a.closed.Load() || workspace == "" || root == "" || path == "" {
		return
	}
	a.Git.PreviewOpen = true
	a.Git.PreviewPath = path
	a.Git.PreviewCommit = commit
	a.Git.PreviewStaged = staged
	a.Git.PreviewBusy = true
	a.Git.PreviewErr = ""
	a.Git.PreviewOld, a.Git.PreviewNew = "", ""
	a.Git.PreviewGen++
	gen := a.Git.PreviewGen
	go func() {
		ctx, cancel := context.WithTimeout(a.lifeCtx, gitStatusTimeout)
		defer cancel()
		old, new, err := load(ctx, root)
		a.Post(func() {
			if a.closed.Load() || a.Git.PreviewGen != gen || a.Git.Workspace != workspace ||
				a.Git.PreviewPath != path || a.Git.PreviewCommit != commit {
				return
			}
			a.Git.PreviewBusy = false
			if err != nil {
				a.Git.PreviewErr = gitErrorText(err)
				return
			}
			a.Git.PreviewOld, a.Git.PreviewNew = old, new
		})
	}()
}

// closeGitPreview drops the overlay, its texts and any read still running.
func (a *App) closeGitPreview() {
	a.Git.PreviewOpen = false
	a.Git.PreviewGen++
	a.Git.PreviewPath = ""
	a.Git.PreviewCommit = ""
	a.Git.PreviewErr = ""
	a.Git.PreviewOld, a.Git.PreviewNew = "", ""
}

// toggleCommit expands or collapses the changed files of one commit. One
// commit is open at a time; a second click on the same row closes it.
func (a *App) toggleCommit(hash string) {
	if hash == "" {
		return
	}
	if a.Git.CommitOpen == hash {
		a.closeCommitFiles()
		return
	}
	workspace, root := a.Git.Workspace, a.Git.RepoRoot
	if workspace == "" || root == "" {
		return
	}
	a.Git.CommitFilesGen++
	gen := a.Git.CommitFilesGen
	a.Git.CommitOpen = hash
	a.Git.CommitFiles = nil
	a.Git.CommitFilesBusy = true
	a.Git.CommitFilesErr = ""
	go func() {
		ctx, cancel := context.WithTimeout(a.lifeCtx, gitStatusTimeout)
		defer cancel()
		files, err := git.CommitFiles(ctx, root, hash)
		a.Post(func() {
			if a.closed.Load() || a.Git.CommitFilesGen != gen ||
				a.Git.Workspace != workspace || a.Git.CommitOpen != hash {
				return
			}
			a.Git.CommitFilesBusy = false
			if err != nil {
				a.Git.CommitFilesErr = gitErrorText(err)
				return
			}
			a.Git.CommitFiles = files
		})
	}()
}

// closeCommitFiles collapses the row and drops any read still running.
func (a *App) closeCommitFiles() {
	a.Git.CommitOpen = ""
	a.Git.CommitFiles = nil
	a.Git.CommitFilesBusy = false
	a.Git.CommitFilesErr = ""
	a.Git.CommitFilesGen++
}

// pruneCommitFiles closes an expanded row whose commit left the bounded
// history, as after undo or a fetch that rewrote refs.
func (a *App) pruneCommitFiles() {
	if a.Git.CommitOpen == "" {
		return
	}
	for _, commit := range a.Git.Commits {
		if commit.Hash == a.Git.CommitOpen {
			return
		}
	}
	a.closeCommitFiles()
}

// shortGraphText cuts by runes so names in any script stay whole.
func shortGraphText(text string, max int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "…"
}

func (a *App) gitGraph(c *mui.Context) {
	t := c.Theme()
	busy := a.Git.Busy != "" || a.Git.Loading
	mui.Column(c).FillHeight().BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		mui.Row(c).Gap(2).AlignItems(mui.Center).Padding(4, 8, 4, 8).Children(func() {
			a.gitToolButton(c, "Fetch", "Fetch from the remote", busy, func() {
				a.runGitAction("Fetch", true, func(ctx context.Context, root string) error {
					return git.Fetch(ctx, root)
				})
			})
			a.gitToolButton(c, "Pull", "Pull the upstream branch", busy, func() {
				a.runGitAction("Pull", true, func(ctx context.Context, root string) error {
					return git.Pull(ctx, root)
				})
			})
			a.gitToolButton(c, "Push", "Push the current branch", busy, func() {
				a.runGitAction("Push", true, func(ctx context.Context, root string) error {
					return git.Push(ctx, root)
				})
			})
			a.gitToolButton(c, "Refresh", "Reload status and graph", busy, func() {
				a.startGitLoad(a.Git.Workspace, true)
			})
			a.gitToolButton(c, "Undo", "Undo the last commit (soft reset)", busy || !a.Git.HasCommits, func() {
				a.runGitAction("Undo last commit", true, func(ctx context.Context, root string) error {
					return git.UndoLastCommit(ctx, root)
				})
			})
		})
		mui.Scroll(c).Grow(1).Padding(5, 4, 8, 6).Children(func() {
			a.gitGraphBody(c)
		})
	})
}

// gitDiscardDialog confirms discarding every change in the repository before
// the destructive git command runs.
func (a *App) gitDiscardDialog(c *mui.Context) {
	if !a.Git.DiscardOpen {
		return
	}
	if a.Git.Workspace == "" || a.Git.Workspace != a.activeGitWorkspace() {
		a.Git.DiscardOpen = false
		return
	}
	t := c.Theme()
	busy := a.Git.Busy != "" || a.Git.Loading
	confirm := false
	mui.Modal(c, &a.Git.DiscardOpen, func() {
		mui.Column(c).Gap(10).Width(380).Children(func() {
			mui.Text(c, "Discard all changes?").FontSize(16).Bold()
			mui.Text(c, "Tracked files go back to the last commit, and untracked files and folders are deleted. Ignored files stay. This cannot be undone.").
				FontSize(11).TextColor(t.TextMuted)
			mutedText(c, fmt.Sprintf("%d staged · %d unstaged", len(a.Git.Staged), len(a.Git.Changes)), 10)
			mui.Row(c).Gap(8).Justify(mui.End).Children(func() {
				if quietButton(c, "Cancel").Clicked() {
					a.Git.DiscardOpen = false
				}
				if dangerButton(c, "Discard").Disabled(busy).Clicked() {
					confirm = true
					a.Git.DiscardOpen = false
				}
			})
		})
	})
	if confirm {
		a.runGitAction("Discard all", true, git.DiscardAll)
	}
}

// gitDiffView holds one preview's buffers; they are rebuilt when the sides
// change, and MyGo diffs them again as they do.
type gitDiffView struct {
	old, new string
	state    *editor.DiffState
}

// gitDiffDialog shows the previewed file in a window-wide overlay.
func (a *App) gitDiffDialog(c *mui.Context) {
	if !a.Git.PreviewOpen {
		return
	}
	if a.Git.Workspace != a.activeGitWorkspace() {
		a.closeGitPreview()
		return
	}
	title := "Diff · " + a.Git.PreviewPath
	switch {
	case a.Git.PreviewCommit != "":
		title = "Commit " + shortCommitHash(a.Git.PreviewCommit) + " · " + a.Git.PreviewPath
	case a.Git.PreviewStaged:
		title = "Staged diff · " + a.Git.PreviewPath
	}
	cryoverlay.FullscreenModal(c, title, &a.Git.PreviewOpen, cryoverlay.FullscreenModalOptions{}, func() {
		switch {
		case a.Git.PreviewBusy:
			crylayout.Centered(c, func() {
				cryfeedback.Busy(c, true, "Loading diff…", "Loaded")
			})
		case a.Git.PreviewErr != "":
			mui.Column(c).Grow(1).Center().Padding(0, 40).Children(func() {
				cryfeedback.Alert(c, cryfeedback.ToneDanger, "Cannot show this diff", a.Git.PreviewErr)
			})
		case a.Git.PreviewOld == a.Git.PreviewNew:
			crylayout.Centered(c, func() {
				cryfeedback.EmptyState(c, "git-compare", "No changes to show", "Both sides of this file are identical.", "")
			})
		default:
			a.gitDiffBody(c)
		}
	})
	if !a.Git.PreviewOpen {
		a.closeGitPreview()
	}
}

// gitDiffBody sizes the viewer to the room the overlay gives it.
func (a *App) gitDiffBody(c *mui.Context) {
	box := mui.Column(c).Grow(1).FillWidth().Clip()
	height := mui.Local(box, "height", func() float32 { return 0 })
	split := mui.Local(box, "split", func() bool { return true })
	view := mui.Local(box, "view", func() gitDiffView { return gitDiffView{} })
	if view.old != a.Git.PreviewOld || view.new != a.Git.PreviewNew {
		view.old, view.new = a.Git.PreviewOld, a.Git.PreviewNew
		view.state = &editor.DiffState{Old: editor.NewBuffer(view.old), New: editor.NewBuffer(view.new)}
	}
	used := *height
	box.Children(func() {
		crycode.DiffView(c, view.state, split, crycode.DiffViewOptions{Name: a.Git.PreviewPath, Height: used})
	})
	crykit.Settle(c, box, []float32{used}, func() []float32 { return []float32{box.Bounds().H} })
	*height = box.Bounds().H
}

func (a *App) gitToolButton(c *mui.Context, label, tip string, disabled bool, fn func()) {
	button := quietButton(c, label).FontSize(8)
	button.Disabled(disabled)
	button.Tooltip(tip)
	if button.Clicked() && !disabled {
		fn()
	}
}
