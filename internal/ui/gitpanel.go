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

	mui "github.com/egoist/mygo/ui"
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
	Graph      string

	Message       string
	ChangesHeight float32
	Gen           int
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
				a.Git.Graph = ""
				return
			}
			if noRepo {
				a.Git.NoRepo = true
				a.Git.RepoRoot = ""
				a.Git.Branch = ""
				a.Git.Ahead, a.Git.Behind = 0, 0
				a.Git.HasCommits = false
				a.Git.Staged, a.Git.Changes = nil, nil
				a.Git.Graph = ""
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
			a.Git.Graph = state.Graph
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
	case "Commit":
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
					mui.Text(c, a.Git.Busy+"…").FontSize(9).TextColor(t.TextMuted).SingleLine()
				}
			})
			line, tip := a.gitHeaderLine()
			if line != "" {
				text := mui.Text(c, line).FontSize(9).TextColor(t.TextMuted).SingleLine()
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
	t := c.Theme()
	mui.Column(c).Grow(1).Center().Gap(8).Children(func() {
		mui.Spinner(c)
		mui.Text(c, "Reading repository…").FontSize(10).TextColor(t.TextMuted)
	})
}

func (a *App) gitUnavailable(c *mui.Context) {
	t := c.Theme()
	workspace := a.Git.Workspace
	mui.Column(c).Grow(1).Padding(14, 14).Gap(8).Children(func() {
		mui.Text(c, "Git is not installed.").FontSize(13).Bold().TextColor(t.Warning)
		mui.Text(c, "Install Git, make sure it is on PATH, then refresh.").
			FontSize(10).TextColor(t.TextMuted)
		if mui.Button(c, "Refresh").FontSize(10).Clicked() {
			a.startGitLoad(workspace, true)
		}
	})
}

func (a *App) gitNoRepository(c *mui.Context) {
	t := c.Theme()
	workspace := a.Git.Workspace
	busy := a.Git.Busy != "" || a.Git.Loading
	mui.Column(c).Grow(1).Padding(14, 14).Gap(8).Children(func() {
		mui.Text(c, "Not a Git repository").FontSize(13).Bold()
		mui.Text(c, "This workspace is not inside a Git repository.").
			FontSize(10).TextColor(t.TextMuted)
		mui.Text(c, workspace).FontSize(9).TextColor(t.TextMuted).Selectable()
		button := mui.PrimaryButton(c, "Initialize Git repository").FontSize(10)
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
				mui.Text(c, a.Git.Busy+"…").FontSize(9).TextColor(t.TextMuted)
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
		if mui.Button(c, "Refresh").FontSize(10).Clicked() {
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
			Disabled(busy)
		status := ""
		switch {
		case busy && a.Git.Busy != "":
			status = a.Git.Busy + "…"
		case staged == 0:
			status = "Stage changes to commit"
		default:
			status = strconv.Itoa(staged) + " staged"
		}
		mui.Row(c).Gap(6).AlignItems(mui.Center).Children(func() {
			if busy && a.Git.Busy != "" {
				mui.Spinner(c)
			}
			if status != "" {
				mui.Text(c, status).FontSize(9).TextColor(t.TextMuted).SingleLine()
			}
		})
		button := mui.PrimaryButton(c, "Commit").FontSize(10).FillWidth()
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
	t := c.Theme()
	staged := a.Git.Staged
	changes := a.Git.Changes
	mui.Column(c).FillHeight().Padding(6, 12, 4, 12).Children(func() {
		mui.Scroll(c).Grow(1).Children(func() {
			mui.Column(c).Gap(3).Children(func() {
				a.gitSectionHeader(c, "Staged", len(staged), "Unstage all", len(staged) > 0, func() {
					a.runGitAction("Unstage all", true, func(ctx context.Context, root string) error {
						return git.UnstageAll(ctx, root)
					})
				})
				if len(staged) == 0 {
					mui.Text(c, "No staged changes").FontSize(9).TextColor(t.TextMuted)
				} else {
					for _, file := range staged {
						a.gitFileRow(c, file, true)
					}
				}

				mui.Box(c).Height(6)
				a.gitSectionHeader(c, "Changes", len(changes), "Stage all", len(changes) > 0, func() {
					a.runGitAction("Stage all", true, func(ctx context.Context, root string) error {
						return git.StageAll(ctx, root)
					})
				})
				if len(changes) == 0 {
					mui.Text(c, "No changes").FontSize(9).TextColor(t.TextMuted)
				} else {
					for _, file := range changes {
						a.gitFileRow(c, file, false)
					}
				}
			})
		})
	})
}

func (a *App) gitSectionHeader(c *mui.Context, title string, count int, action string, showAction bool, fn func()) {
	t := c.Theme()
	busy := a.Git.Busy != "" || a.Git.Loading
	mui.Row(c).Gap(4).AlignItems(mui.Center).Children(func() {
		mui.Text(c, title).FontSize(9).Bold().TextColor(t.TextMuted)
		mui.Text(c, strconv.Itoa(count)).FontSize(8).TextColor(t.TextMuted)
		mui.Spacer(c)
		if showAction {
			button := mui.Button(c, action).FontSize(8)
			button.Disabled(busy)
			button.Tooltip(action + " files in this section")
			if button.Clicked() && !busy {
				fn()
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
	tip := path
	if file.OrigPath != "" {
		tip = file.OrigPath + " → " + path
	}
	busy := a.Git.Busy != "" || a.Git.Loading

	mui.Row(c).Gap(4).AlignItems(mui.Center).Children(func() {
		mui.Text(c, code).Font("monospace").FontSize(8).TextColor(color)
		mui.Column(c).Gap(0).Grow(1).Children(func() {
			text := mui.Text(c, truncate(name, 30)).FontSize(10).SingleLine()
			text.Tooltip(tip)
			if dir != "" {
				mui.Text(c, truncate(dir, 34)).FontSize(8).TextColor(t.TextMuted).SingleLine()
			}
		})
		label, action := "＋", "Stage "
		if staged {
			label, action = "－", "Unstage "
		}
		button := mui.Button(c, label).Label(action + path).FontSize(9)
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
		mui.Scroll(c).Grow(1).Padding(5, 10, 8, 10).Children(func() {
			if a.Git.Graph == "" {
				mui.Text(c, "No commits yet.").FontSize(10).TextColor(t.TextMuted)
				return
			}
			mui.Text(c, a.Git.Graph).Font("monospace").FontSize(9).Selectable()
		})
	})
}

func (a *App) gitToolButton(c *mui.Context, label, tip string, disabled bool, fn func()) {
	button := mui.Button(c, label).FontSize(8)
	button.Disabled(disabled)
	button.Tooltip(tip)
	if button.Clicked() && !disabled {
		fn()
	}
}
