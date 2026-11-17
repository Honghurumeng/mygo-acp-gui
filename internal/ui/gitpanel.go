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
	"github.com/reed/acp-gui/internal/files"
	"github.com/reed/acp-gui/internal/git"
)

const (
	gitPanelWidth = 300
	// gitListGutter is the room both lists keep at the panel's right edge
	// for their overlay scroll bars: their scroll boxes run to that edge, so
	// the bars line up in one column, and their rows stop short of it.
	gitListGutter           = 12
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

	// Stat counts the changed paths and their lines for the header.
	Stat git.DiffStat

	// CommitOpen is the commit whose changed files show under its graph
	// row. CommitFilesGen guards a late read against another commit, a
	// workspace change, or a collapsed row.
	CommitOpen      string
	CommitFiles     []git.CommitFile
	CommitFilesBusy bool
	CommitFilesErr  string
	CommitFilesGen  int

	// CommitHover is the graph row whose details card shows. Open turns on
	// after a short rest, Leave is when an open card closes after the
	// pointer left it, and Skip hides a card the user dismissed until the
	// pointer leaves its row.
	CommitHover      string
	CommitHoverSince time.Time
	CommitHoverOpen  bool
	CommitHoverLeave time.Time
	CommitHoverSkip  string

	// CommitStat is the hovered commit's changed paths and lines, read when
	// its details card opens. CommitStatGen guards a late read against
	// another card, a workspace change, or a closed card.
	CommitStatHash string
	CommitStat     git.DiffStat
	CommitStatBusy bool
	CommitStatErr  string
	CommitStatGen  int

	Message       string
	ChangesHeight float32
	Gen           int

	// BranchPicker is the local branch list behind the header's branch line.
	BranchPicker branchPickerState

	// BranchCreate is the dialog opened by right-clicking a commit: it makes
	// a branch at BranchCreateBase without checking it out.
	BranchCreateOpen bool
	BranchCreateBase string
	BranchCreateName string
	BranchCreateErr  string

	// DiscardOpen asks before throwing away every change in the repository.
	DiscardOpen bool

	// Preview is the file card floating left of one changed file;
	// PreviewGen guards a late read against a newer file or workspace.
	PreviewOpen   bool
	PreviewPath   string
	PreviewCommit string
	PreviewStaged bool
	PreviewMode   viewerMode
	// PreviewCanEdit is true when the card shows the working-tree text, so
	// Edit saves back to the file. Staged and historical sides stay
	// read-only.
	PreviewCanEdit bool
	PreviewBusy    bool
	PreviewErr     string
	PreviewOld     string
	PreviewNew     string
	PreviewSaving  bool
	PreviewSaveErr string
	PreviewNotice  string
	PreviewGen     int
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
	if a.closed.Load() || !a.GitPanelOpen || a.RightTab != rightTabGit {
		return
	}
	workspace := a.activeGitWorkspace()
	if workspace == "" || !filepath.IsAbs(workspace) {
		return
	}
	// A checkout made outside the app changes .git/HEAD without touching
	// the files the composer reads, so its cached branch name must go.
	for _, chat := range a.Chats {
		if sameWorkspace(chat.Workspace, workspace) {
			chat.gitWorkspace = ""
		}
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
	if !a.GitPanelOpen || a.RightTab != rightTabGit {
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
				a.Git.Stat = git.DiffStat{}
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
				a.Git.Stat = git.DiffStat{}
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
			a.Git.Stat = state.Stat
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
				a.Git.Error = T(label) + ": " + gitErrorText(err)
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
		return T("Repository initialized.")
	case "Commit":
		return T("Committed.")
	case "Fetch":
		return T("Fetched.")
	case "Pull":
		return T("Pulled.")
	case "Push":
		return T("Pushed.")
	case "Undo last commit":
		return T("Last commit undone; changes stay staged.")
	case "Discard all":
		return T("Discarded all changes.")
	default:
		return ""
	}
}

func gitErrorText(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return T("git timed out")
	}
	if errors.Is(err, context.Canceled) {
		return T("git cancelled")
	}
	return strings.TrimSpace(err.Error())
}

// gitPanel builds the right column and returns it as the diff card's anchor.
func (a *App) gitPanel(c *mui.Context) mui.Element {
	a.ensureGitPanel()
	t := c.Theme()
	panel := mui.Column(c).Width(gitPanelWidth).FillHeight().
		Background(t.Surface).
		BorderWidth(1, 0, 0, 0).BorderColor(t.Border)
	panel.Children(func() {
		a.gitPanelHeader(c)
		a.gitPanelBody(c)
	})
	return panel
}

func (a *App) gitPanelHeader(c *mui.Context) {
	t := c.Theme()
	mui.Row(c).Height(54).Gap(6).Padding(8, 12).AlignItems(mui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		mui.Column(c).Gap(1).Grow(1).Children(func() {
			mui.Row(c).Gap(5).AlignItems(mui.Center).Children(func() {
				a.rightPanelName(c, "Git")
				if a.Git.Loading {
					mui.Spinner(c)
				}
				if a.Git.Busy != "" {
					a.gitCaption(c, T(a.Git.Busy)+"…")
				}
				mui.Spacer(c)
				a.gitBehindBadge(c)
				a.gitHeaderStat(c)
			})
			line, tip := a.gitHeaderLine()
			if line != "" {
				if a.Git.RepoRoot != "" && a.Git.Branch != "" {
					a.gitHeaderBranch(c, line, tip)
				} else {
					text := a.gitCaption(c, line)
					if tip != "" {
						text.Tooltip(tip)
					}
				}
			}
		})
	})
}

// gitBehindBadge is the header's reminder that the upstream has commits the
// branch has not pulled: after a fetch it appears with the count, and a pull
// removes it. Without it the graph of a fetched branch and a pulled one can
// look the same.
func (a *App) gitBehindBadge(c *mui.Context) {
	if a.Git.Behind <= 0 {
		return
	}
	t := c.Theme()
	commits := "commits"
	if a.Git.Behind == 1 {
		commits = "commit"
	}
	mui.Text(c, "↓"+strconv.Itoa(a.Git.Behind)).FontSize(a.gitFont(8)).
		Padding(1, 5).Radius(4).Shrink(0).
		Background(t.Warning.Alpha(0.14)).TextColor(t.Warning).
		Tooltip(strconv.Itoa(a.Git.Behind) + " " + commits + " fetched; pull to merge")
}

// gitHeaderStat shows the changed file count and line changes beside the
// panel title. It stays empty while there is nothing uncommitted.
func (a *App) gitHeaderStat(c *mui.Context) {
	t := c.Theme()
	stat := a.Git.Stat
	if stat.Files == 0 {
		return
	}
	files := strconv.Itoa(stat.Files) + " files"
	if stat.Files == 1 {
		files = "1 file"
	}
	a.gitCaption(c, files)
	if stat.Added > 0 {
		mui.Text(c, "+"+strconv.Itoa(stat.Added)).FontSize(a.gitFont(9)).TextColor(t.Success)
	}
	if stat.Removed > 0 {
		mui.Text(c, "−"+strconv.Itoa(stat.Removed)).FontSize(a.gitFont(9)).TextColor(t.Danger)
	}
}

func (a *App) gitHeaderLine() (string, string) {
	if a.Git.Busy != "" {
		return "", ""
	}
	switch {
	case !a.Git.Checked:
		return T("Reading…"), ""
	case !a.Git.Available:
		return T("git not found"), ""
	case a.Git.NoRepo:
		return T("No repository"), ""
	case a.Git.Error != "" && a.Git.RepoRoot == "":
		return T("Error"), a.Git.Error
	case a.Git.RepoRoot != "":
		label := filepath.Base(a.Git.RepoRoot)
		if label == "" || label == "." || label == string(filepath.Separator) {
			label = a.Git.RepoRoot
		}
		line := truncate(label, 18)
		if a.Git.Branch != "" {
			line += " · ⑂ " + truncate(a.Git.Branch, 18)
		}
		if a.Git.Ahead > 0 {
			line += " ↑" + strconv.Itoa(a.Git.Ahead)
		}
		return line, a.Git.RepoRoot
	default:
		return T("Reading…"), ""
	}
}

// gitHeaderBranch makes the header's "repo · branch" line the branch
// picker: the panel is 300 DIP wide, so a separate branch button would cost a
// row. The line keeps its muted look until the pointer rests on it.
func (a *App) gitHeaderBranch(c *mui.Context, line, tip string) {
	t := c.Theme()
	state := &a.Git.BranchPicker
	button := buttonBase(c, T("Switch Git branch")).Padding(0, 0).Radius(4)
	if tip != "" {
		button.Tooltip(tip + " · " + T("Click to switch branch"))
	} else {
		button.Tooltip(T("Click to switch branch"))
	}
	if button.Hovered() || state.Open {
		button.Background(t.SurfaceHover)
	}
	button.Children(func() {
		text := a.gitCaption(c, line)
		if button.Hovered() || state.Open {
			text.TextColor(t.Text)
		}
	})
	if button.Clicked() {
		a.toggleBranchPicker(state, a.Git.RepoRoot)
	}
	disabled := a.Git.Busy != "" || a.Git.Loading
	a.branchPopover(c, button, state, a.Git.Branch, a.gitFont, disabled, a.switchGitPanelBranch)
}

// switchGitPanelBranch checks out one branch from the header picker and
// refreshes what shows it: the graph, the Files tab and every draft's branch
// chip in the same workspace.
func (a *App) switchGitPanelBranch(name string, carry bool) {
	workspace, root := a.Git.Workspace, a.Git.RepoRoot
	state := &a.Git.BranchPicker
	onSwitched := func(switched string) {
		if state.Warning == "" {
			state.Open = false
		}
		a.branchSwitched(workspace, switched, state)
	}
	if carry {
		a.carrySwitchBranchFromPicker(state, root, name, onSwitched)
		return
	}
	a.switchBranchFromPicker(state, root, name, onSwitched)
}

// openGitBranchCreate opens the commit context menu's branch dialog. It
// only makes the branch: checking it out is the composer's or the header's
// explicit choice.
func (a *App) openGitBranchCreate(hash string) {
	if a.Git.RepoRoot == "" || a.Git.Busy != "" || a.Git.Loading || !isFullHash(hash) {
		return
	}
	a.closeCommitHover()
	a.Git.BranchPicker.close()
	a.Git.BranchCreateOpen = true
	a.Git.BranchCreateBase = hash
	a.Git.BranchCreateName = ""
	a.Git.BranchCreateErr = ""
}

// closeGitBranchCreate drops the branch dialog and its error.
func (a *App) closeGitBranchCreate() {
	a.Git.BranchCreateOpen = false
	a.Git.BranchCreateBase = ""
	a.Git.BranchCreateName = ""
	a.Git.BranchCreateErr = ""
}

func isFullHash(hash string) bool {
	if len(hash) != 40 && len(hash) != 64 {
		return false
	}
	for _, r := range hash {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
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
		mutedText(c, T("Reading repository…"), a.gitFont(10))
	})
}

func (a *App) gitUnavailable(c *mui.Context) {
	t := c.Theme()
	workspace := a.Git.Workspace
	mui.Column(c).Grow(1).Padding(14, 14).Gap(8).Children(func() {
		mui.Text(c, T("Git is not installed.")).FontSize(a.gitFont(13)).Bold().TextColor(t.Warning)
		mutedText(c, T("Install Git, make sure it is on PATH, then refresh."), a.gitFont(10))
		if quietButton(c, T("Refresh")).FontSize(a.gitFont(10)).Clicked() {
			a.startGitLoad(workspace, true)
		}
	})
}

func (a *App) gitNoRepository(c *mui.Context) {
	workspace := a.Git.Workspace
	busy := a.Git.Busy != "" || a.Git.Loading
	mui.Column(c).Grow(1).Padding(14, 14).Gap(8).Children(func() {
		mui.Text(c, T("Not a Git repository")).FontSize(a.gitFont(13)).Bold()
		mutedText(c, T("This workspace is not inside a Git repository."), a.gitFont(10))
		mutedText(c, workspace, a.gitFont(9)).Selectable()
		button := primaryButton(c, T("Initialize Git repository")).FontSize(a.gitFont(10))
		button.Disabled(busy)
		button.Tooltip(T("Run git init in this workspace"))
		if button.Clicked() && !busy {
			a.runGitAction("Initialize repository", false, func(ctx context.Context, dir string) error {
				return git.Init(ctx, dir)
			})
		}
		if a.Git.Busy != "" {
			mui.Row(c).Gap(6).AlignItems(mui.Center).Children(func() {
				mui.Spinner(c)
				a.gitCaption(c, T(a.Git.Busy)+"…")
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
		mui.Text(c, T("Git unavailable")).FontSize(a.gitFont(13)).Bold().TextColor(t.Danger)
		message := truncate(singleLine(a.Git.Error), 240)
		mui.Text(c, message).FontSize(a.gitFont(10)).TextColor(t.Danger).Selectable().Tooltip(a.Git.Error)
		if quietButton(c, T("Refresh")).FontSize(a.gitFont(10)).Clicked() {
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
			Label(T("Commit message")).
			Placeholder(T("Commit message")).
			Lines(2, 4).
			Padding(5, 7).
			// The same size as the Staged/Changes rows: the Git panel's one
			// font setting governs its whole body, not just the list.
			FontSize(a.gitFont(10)).
			Radius(t.Radius).
			Border(1, t.Border).
			Background(t.Background).
			Disabled(busy)
		button := primaryButton(c, T("Commit")).FontSize(a.gitFont(10)).FillWidth()
		disabled := busy || staged == 0 || strings.TrimSpace(a.Git.Message) == ""
		button.Disabled(disabled)
		button.Tooltip(T("Commit the staged changes"))
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
			mui.Text(c, "⚠ "+message).FontSize(a.gitFont(9)).TextColor(t.Danger).Selectable().Tooltip(a.Git.Error)
		}
		if a.Git.Notice != "" {
			mui.Text(c, a.Git.Notice).FontSize(a.gitFont(9)).TextColor(t.Success)
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
	// The scroll box runs to the panel's right edge, so this list's overlay
	// scroll bar sits in the same column as the history list's below.
	mui.Column(c).FillHeight().Padding(6, 0, 4, gitListGutter).Children(func() {
		mui.Scroll(c).Grow(1).Children(func() {
			// Leave room for the overlay scrollbar so it never covers the
			// stage/unstage button of a row.
			mui.Column(c).Gap(3).Padding(0, gitListGutter, 0, 0).Children(func() {
				a.gitSectionHeader(c, T("Staged"), len(staged), gitSectionAction{
					label: T("Unstage all"),
					tip:   T("Unstage all files in this section"),
					show:  len(staged) > 0,
					fn: func() {
						a.runGitAction("Unstage all", true, func(ctx context.Context, root string) error {
							return git.UnstageAll(ctx, root)
						})
					},
				})
				if len(staged) == 0 {
					a.gitCaption(c, T("No staged changes"))
				} else {
					for _, file := range staged {
						a.gitFileRow(c, file, true)
					}
				}

				mui.Box(c).Height(6)
				a.gitSectionHeader(c, T("Changes"), len(changes),
					gitSectionAction{
						label:  T("Discard all"),
						tip:    T("Discard every staged and unstaged change; asks first"),
						danger: true,
						show:   len(staged) > 0 || len(changes) > 0,
						fn:     func() { a.Git.DiscardOpen = true },
					},
					gitSectionAction{
						label: T("Stage all"),
						tip:   T("Stage all files in this section"),
						show:  len(changes) > 0,
						fn: func() {
							a.runGitAction("Stage all", true, func(ctx context.Context, root string) error {
								return git.StageAll(ctx, root)
							})
						},
					},
				)
				if len(changes) == 0 {
					a.gitCaption(c, T("No changes"))
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
		a.gitSectionLabel(c, title)
		mutedText(c, strconv.Itoa(count), a.gitFont(8))
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
			button.FontSize(a.gitFont(8))
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
	selected := a.gitPreviewSelected(path, "", staged)

	mui.Row(c).Gap(4).AlignItems(mui.Center).Children(func() {
		mui.Text(c, code).Font("monospace").FontSize(a.gitFont(8)).TextColor(color)
		preview := mui.ButtonBase(c).Gap(5).Grow(1).Padding(1, 2).Radius(4).
			Justify(mui.Start).Label(Tf("Diff of %s", path))
		switch {
		case preview.Pressed():
			preview.Background(t.SurfacePressed)
		case selected:
			preview.Background(t.Selection)
		default:
			hover := float32(0)
			if preview.Hovered() {
				hover = 1
			}
			preview.Background(preview.AnimateColor("bg", t.SurfaceHover.Alpha(hover), mui.Snappy))
		}
		preview.Children(func() {
			mui.Text(c, truncate(name, 24)).FontSize(a.gitFont(10)).SingleLine()
			if dir != "" {
				mutedText(c, truncate(dir, 20), a.gitFont(8)).SingleLine()
			}
		})
		if preview.Clicked() {
			a.openGitPreview(file, staged)
		}
		label, action := "＋", T("Stage ")
		if staged {
			label, action = "－", T("Unstage ")
		}
		button := quietButton(c, label).Label(action + path).FontSize(a.gitFont(9))
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

// gitPreviewSelected reports whether the floating diff card shows this side
// of the file. Working-tree rows pass commit "" and their staged flag; a file
// of an expanded commit passes that commit.
func (a *App) gitPreviewSelected(path, commit string, staged bool) bool {
	return a.Git.PreviewOpen && a.Git.PreviewPath == path &&
		a.Git.PreviewCommit == commit && a.Git.PreviewStaged == staged &&
		a.Git.Workspace == a.activeGitWorkspace()
}

// openGitPreview shows a staged or unstaged file against the index or HEAD.
func (a *App) openGitPreview(file git.File, staged bool) {
	canEdit := !staged && file.Worktree != 'D' && !file.Conflict()
	a.startGitPreview(file.Path, "", staged, canEdit, func(ctx context.Context, root string) (string, string, error) {
		return git.Diff(ctx, root, file, staged)
	})
}

// openGitCommitPreview shows one file as a commit changed it, against the
// commit's first parent, as the expanded commit list lists it.
func (a *App) openGitCommitPreview(hash string, file git.CommitFile) {
	a.startGitPreview(file.Path, hash, false, false, func(ctx context.Context, root string) (string, string, error) {
		return git.CommitDiff(ctx, root, hash, file)
	})
}

// startGitPreview reads both sides off the UI thread and opens the overlay;
// a newer click, another commit or another workspace wins over a late read.
func (a *App) startGitPreview(path, commit string, staged, canEdit bool, load func(context.Context, string) (string, string, error)) {
	workspace, root := a.activeGitWorkspace(), a.Git.RepoRoot
	if a.closed.Load() || workspace == "" || root == "" || path == "" {
		return
	}
	a.Git.PreviewOpen = true
	a.Git.PreviewPath = path
	a.Git.PreviewCommit = commit
	a.Git.PreviewStaged = staged
	a.Git.PreviewMode = viewerDiff
	a.Git.PreviewCanEdit = canEdit
	a.Git.PreviewBusy = true
	a.Git.PreviewErr = ""
	a.Git.PreviewOld, a.Git.PreviewNew = "", ""
	a.Git.PreviewSaving = false
	a.Git.PreviewSaveErr = ""
	a.Git.PreviewNotice = ""
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
	a.Git.PreviewMode = viewerDiff
	a.Git.PreviewCanEdit = false
	a.Git.PreviewErr = ""
	a.Git.PreviewOld, a.Git.PreviewNew = "", ""
	a.Git.PreviewSaving = false
	a.Git.PreviewSaveErr = ""
	a.Git.PreviewNotice = ""
}

// saveGitPreview writes an edited working-tree file back through the same
// path checks as the Files panel. Staged and historical sides never save.
func (a *App) saveGitPreview(text string) {
	workspace, root := a.Git.Workspace, a.Git.RepoRoot
	path := a.Git.PreviewPath
	if a.closed.Load() || workspace == "" || root == "" || path == "" ||
		a.Git.PreviewStaged || a.Git.PreviewCommit != "" || !a.Git.PreviewCanEdit ||
		a.Git.PreviewBusy || a.Git.PreviewErr != "" {
		return
	}
	gen := a.Git.PreviewGen
	a.Git.PreviewSaving = true
	a.Git.PreviewSaveErr = ""
	a.Git.PreviewNotice = ""
	go func() {
		err := files.SaveText(root, path, text)
		a.Post(func() {
			if a.closed.Load() || a.Git.PreviewGen != gen || !a.Git.PreviewOpen ||
				a.Git.Workspace != workspace || a.Git.PreviewPath != path ||
				a.Git.PreviewStaged || a.Git.PreviewCommit != "" {
				return
			}
			a.Git.PreviewSaving = false
			if err != nil {
				a.Git.PreviewSaveErr = fileErrorText(err)
				return
			}
			a.Git.PreviewNew = text
			a.Git.PreviewNotice = T("Saved.")
			a.refreshAfterFileSave(workspace)
		})
	}()
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

// ensureCommitStat reads the hovered commit's changed paths and lines once
// its card opens, off the UI thread; another card or workspace wins over a
// late read.
func (a *App) ensureCommitStat(hash string) {
	if a.Git.CommitStatHash == hash {
		return
	}
	workspace, root := a.Git.Workspace, a.Git.RepoRoot
	if a.closed.Load() || workspace == "" || root == "" || hash == "" {
		return
	}
	a.Git.CommitStatGen++
	gen := a.Git.CommitStatGen
	a.Git.CommitStatHash = hash
	a.Git.CommitStat = git.DiffStat{}
	a.Git.CommitStatBusy = true
	a.Git.CommitStatErr = ""
	go func() {
		ctx, cancel := context.WithTimeout(a.lifeCtx, gitStatusTimeout)
		defer cancel()
		stat, err := git.CommitStat(ctx, root, hash)
		a.Post(func() {
			if a.closed.Load() || a.Git.CommitStatGen != gen ||
				a.Git.Workspace != workspace || a.Git.CommitHover != hash {
				return
			}
			a.Git.CommitStatBusy = false
			if err != nil {
				a.Git.CommitStatErr = gitErrorText(err)
				return
			}
			a.Git.CommitStat = stat
		})
	}()
}

// resetCommitStat drops the card's stat and invalidates a read still running.
func (a *App) resetCommitStat() {
	a.Git.CommitStatGen++
	a.Git.CommitStatHash = ""
	a.Git.CommitStat = git.DiffStat{}
	a.Git.CommitStatBusy = false
	a.Git.CommitStatErr = ""
}

// pruneCommitFiles closes an expanded row whose commit left the bounded
// history, as after undo or a fetch that rewrote refs, and the details card
// of a commit that is no longer shown.
func (a *App) pruneCommitFiles() {
	foundOpen, foundHover := false, false
	for _, commit := range a.Git.Commits {
		if commit.Hash == a.Git.CommitOpen {
			foundOpen = true
		}
		if commit.Hash == a.Git.CommitHover {
			foundHover = true
		}
	}
	if a.Git.CommitOpen != "" && !foundOpen {
		a.closeCommitFiles()
	}
	if a.Git.CommitHover != "" && !foundHover {
		a.closeCommitHover()
	}
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
			a.gitToolButton(c, T("Fetch"), T("Fetch from the remote"), busy, func() {
				a.runGitAction("Fetch", true, func(ctx context.Context, root string) error {
					return git.Fetch(ctx, root)
				})
			})
			a.gitToolButton(c, T("Pull"), T("Pull the current branch; uses a same-named remote branch when it has no upstream"), busy, func() {
				a.runGitAction("Pull", true, func(ctx context.Context, root string) error {
					return git.PullBranch(ctx, root)
				})
			})
			a.gitToolButton(c, T("Push"), T("Push the current branch; sets its upstream when it has none"), busy, func() {
				a.runGitAction("Push", true, func(ctx context.Context, root string) error {
					return git.PushBranch(ctx, root)
				})
			})
			a.gitToolButton(c, T("Refresh"), T("Reload status and graph"), busy, func() {
				a.startGitLoad(a.Git.Workspace, true)
			})
			a.gitToolButton(c, T("Undo"), T("Undo the last commit (soft reset)"), busy || !a.Git.HasCommits, func() {
				a.runGitAction("Undo last commit", true, func(ctx context.Context, root string) error {
					return git.UndoLastCommit(ctx, root)
				})
			})
		})
		mui.Scroll(c).Grow(1).Padding(5, 0, 8, 6).Children(func() {
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
			mui.Text(c, T("Discard all changes?")).FontSize(a.gitFont(16)).Bold()
			mui.Text(c, T("Tracked files go back to the last commit, and untracked files and folders are deleted. Ignored files stay. This cannot be undone.")).
				FontSize(a.gitFont(11)).TextColor(t.TextMuted)
			mutedText(c, Tf("%d staged · %d unstaged", len(a.Git.Staged), len(a.Git.Changes)), a.gitFont(10))
			mui.Row(c).Gap(8).Justify(mui.End).Children(func() {
				if quietButton(c, T("Cancel")).Clicked() {
					a.Git.DiscardOpen = false
				}
				if dangerButton(c, T("Discard")).Disabled(busy).Clicked() {
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

// gitBranchCreateDialog asks for the name of the branch made at the commit a
// context menu opened. The dialog stays open with git's error when the name
// or the start point is unusable.
func (a *App) gitBranchCreateDialog(c *mui.Context) {
	if !a.Git.BranchCreateOpen {
		return
	}
	if a.Git.Workspace == "" || a.Git.Workspace != a.activeGitWorkspace() ||
		a.Git.RepoRoot == "" || a.Git.BranchCreateBase == "" ||
		(!a.GitPanelOpen || a.RightTab != rightTabGit) {
		a.closeGitBranchCreate()
		return
	}
	t := c.Theme()
	busy := a.Git.Busy != "" || a.Git.Loading
	subject := ""
	for _, commit := range a.Git.Commits {
		if commit.Hash == a.Git.BranchCreateBase {
			subject = commit.Message
			break
		}
	}
	base := shortCommitHash(a.Git.BranchCreateBase)
	if subject != "" {
		base += " · " + truncate(subject, 60)
	}
	mui.Modal(c, &a.Git.BranchCreateOpen, func() {
		mui.Column(c).Gap(9).Width(400).Children(func() {
			mui.Text(c, T("Create branch")).FontSize(a.gitFont(16)).Bold()
			mui.Text(c, Tf("At %s", base)).FontSize(a.gitFont(10)).
				TextColor(t.TextMuted).SingleLine().Selectable().
				Tooltip(a.Git.BranchCreateBase)
			mui.TextInput(c, &a.Git.BranchCreateName).Label(T("Branch name")).
				Placeholder("feature/name")
			mutedText(c, T("The branch starts at this commit; the work tree and the current branch stay as they are."), a.gitFont(9))
			if a.Git.BranchCreateErr != "" {
				mui.Text(c, "⚠ "+truncate(singleLine(a.Git.BranchCreateErr), 240)).
					FontSize(a.gitFont(10)).TextColor(t.Danger).MaxLines(3)
			}
			mui.Row(c).Gap(8).Justify(mui.End).Children(func() {
				if quietButton(c, T("Cancel")).Clicked() {
					a.closeGitBranchCreate()
				}
				create := primaryButton(c, T("Create"))
				create.Disabled(busy || strings.TrimSpace(a.Git.BranchCreateName) == "")
				if create.Clicked() && !busy {
					a.submitGitBranchCreate()
				}
			})
		})
	})
	if !a.Git.BranchCreateOpen {
		a.closeGitBranchCreate()
	}
}

// submitGitBranchCreate runs git branch in the background. On success the
// graph rereads so the new ref shows on the commit; a failure stays in the
// dialog next to the name that caused it.
func (a *App) submitGitBranchCreate() {
	if a.closed.Load() || a.Git.Busy != "" || a.Git.Loading {
		return
	}
	workspace, root := a.Git.Workspace, a.Git.RepoRoot
	name, base := strings.TrimSpace(a.Git.BranchCreateName), a.Git.BranchCreateBase
	if workspace == "" || root == "" || base == "" {
		a.Git.BranchCreateErr = T("The repository is no longer available.")
		return
	}
	if name == "" {
		a.Git.BranchCreateErr = T("Enter a branch name.")
		return
	}
	a.Git.Gen++
	gen := a.Git.Gen
	a.Git.Busy = "Create branch"
	a.Git.Error = ""
	a.Git.Notice = ""
	a.Git.BranchCreateErr = ""
	go func() {
		ctx, cancel := context.WithTimeout(a.lifeCtx, gitActionTimeout)
		defer cancel()
		err := git.CreateBranch(ctx, root, name, base)
		a.Post(func() {
			if a.closed.Load() || a.Git.Gen != gen || a.Git.Workspace != workspace {
				return
			}
			a.Git.Busy = ""
			if err != nil {
				a.Git.BranchCreateErr = gitErrorText(err)
				return
			}
			a.closeGitBranchCreate()
			a.Git.BranchPicker.close()
			a.Git.Notice = Tf("Branch %s created.", name)
			a.startGitLoad(workspace, false)
		})
	}()
}

// gitPreviewTitle names the file and the mode the card shows. Diff keeps
// the old wording so the title tells which side is compared.
func (a *App) gitPreviewTitle(mode viewerMode) string {
	switch {
	case a.Git.PreviewCommit != "":
		return Tf("Commit %s · %s", shortCommitHash(a.Git.PreviewCommit), a.Git.PreviewPath)
	case a.Git.PreviewStaged:
		if mode == viewerView || mode == viewerEdit {
			return Tf("Staged file · %s", a.Git.PreviewPath)
		}
		return Tf("Staged diff · %s", a.Git.PreviewPath)
	}
	switch mode {
	case viewerView:
		return Tf("File · %s", a.Git.PreviewPath)
	case viewerEdit:
		return Tf("Edit · %s", a.Git.PreviewPath)
	default:
		return Tf("Diff · %s", a.Git.PreviewPath)
	}
}

// gitDiffPopover floats the shared viewer beside the Git panel: the card's
// right edge sits on the panel's left edge, as the commit details card does
// beside a graph row, but larger. Pressing outside or Escape closes it.
func (a *App) gitDiffPopover(c *mui.Context, anchor mui.Element) {
	if !a.Git.PreviewOpen {
		return
	}
	if a.Git.Workspace != a.activeGitWorkspace() {
		// Layout reads the state before the panel does; keep this frame valid.
		a.closeGitPreview()
		return
	}
	mui.PopoverBase(c, anchor, &a.Git.PreviewOpen, func(panel mui.Element) {
		a.gitDiffCard(c, panel, anchor)
	})
	if !a.Git.PreviewOpen {
		a.closeGitPreview()
	}
}

// gitDiffCard maps the Git preview onto the shared file viewer: Diff and
// View always, Edit only for the working-tree side.
func (a *App) gitDiffCard(c *mui.Context, panel, anchor mui.Element) {
	canEdit := a.Git.PreviewCanEdit && !a.Git.PreviewBusy && a.Git.PreviewErr == ""
	modes := []viewerMode{viewerDiff, viewerView}
	if canEdit {
		modes = append(modes, viewerEdit)
	}
	var save func(string)
	if canEdit {
		save = a.saveGitPreview
	}
	a.viewerCard(c, panel, anchor, viewerSpec{
		Key:      "git:" + a.Git.PreviewCommit + ":" + a.Git.PreviewPath,
		Title:    a.gitPreviewTitle(a.Git.PreviewMode),
		Tooltip:  a.Git.PreviewPath,
		Filename: a.Git.PreviewPath,
		Body:     a.Git.PreviewNew,
		Old:      a.Git.PreviewOld,
		New:      a.Git.PreviewNew,
		Markdown: isMarkdownPath(a.Git.PreviewPath),
		Modes:    modes,
		Mode:     a.Git.PreviewMode,
		OnMode: func(mode viewerMode) {
			a.Git.PreviewMode = mode
			a.Git.PreviewSaveErr = ""
			a.Git.PreviewNotice = ""
		},
		OnClose:    a.closeGitPreview,
		Save:       save,
		Saving:     a.Git.PreviewSaving,
		SaveErr:    a.Git.PreviewSaveErr,
		Notice:     a.Git.PreviewNotice,
		Busy:       a.Git.PreviewBusy,
		Err:        a.Git.PreviewErr,
		BusyText:   T("Loading diff…"),
		ErrorTitle: T("Cannot show this diff"),
	})
}

func (a *App) gitToolButton(c *mui.Context, label, tip string, disabled bool, fn func()) {
	button := quietButton(c, label).FontSize(a.gitFont(8))
	button.Disabled(disabled)
	button.Tooltip(tip)
	if button.Clicked() && !disabled {
		fn()
	}
}
