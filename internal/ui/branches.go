package ui

import (
	"context"
	"strings"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/git"
)

// branchPickerState is the UI-thread copy of one branch list: the Git
// panel's header and each draft's composer own one. Reads and switches run
// in goroutines; Gen and Workspace keep a late result out of a newer list.
type branchPickerState struct {
	Open      bool
	Workspace string
	Branches  []git.Branch
	Busy      string
	// Switching keeps the picker from starting a branch read over a
	// checkout that is still running.
	Switching bool
	Err       string
	// SwitchTarget is the branch the last checkout tried; it names the branch
	// in a failure message even when git's own output does not.
	SwitchTarget string
	// SwitchFailed is the branch a plain checkout just refused because local
	// changes would be overwritten; it enables the carry fallback.
	SwitchFailed string
	// Warning reports a switch that happened but could not reapply the
	// stashed changes; StashRef names where they stayed.
	Warning string
	Gen     int
}

// reset drops the list and invalidates a read still running, without losing
// the generation count that guards it.
func (s *branchPickerState) reset() {
	*s = branchPickerState{Gen: s.Gen + 1}
}

// close hides the popover and its progress, but keeps a loaded list for the
// next open. The owner reloads on open anyway, so the rows are never stale.
func (s *branchPickerState) close() {
	s.Open = false
	if !s.Switching {
		s.Busy = ""
		s.Err = ""
		s.SwitchTarget = ""
		s.SwitchFailed = ""
		s.Warning = ""
	}
}

// toggleBranchPicker opens the list at dir and starts a read, or closes an
// open one.
func (a *App) toggleBranchPicker(state *branchPickerState, dir string) {
	if state.Open {
		state.close()
		return
	}
	state.Open = true
	a.loadBranches(state, dir)
}

// loadBranches reads the local branches off the UI thread. A newer read, a
// workspace change or a reset drops the result.
func (a *App) loadBranches(state *branchPickerState, dir string) {
	dir = strings.TrimSpace(dir)
	if a.closed.Load() || dir == "" || state.Switching {
		return
	}
	state.Gen++
	gen := state.Gen
	state.Workspace = dir
	state.Busy = T("Reading branches…")
	state.Err = ""
	state.SwitchTarget = ""
	state.SwitchFailed = ""
	state.Warning = ""
	go func() {
		ctx, cancel := context.WithTimeout(a.lifeCtx, gitStatusTimeout)
		defer cancel()
		branches, err := git.Branches(ctx, dir)
		a.Post(func() {
			if a.closed.Load() || state.Gen != gen || state.Workspace != dir {
				return
			}
			state.Busy = ""
			if err != nil {
				state.Branches = nil
				state.Err = gitErrorText(err)
				return
			}
			state.Branches = branches
		})
	}()
}

// switchBranchFromPicker checks out one branch and reports success on the UI
// thread. A failure stays in the popover so the name that caused it is still
// visible.
func (a *App) switchBranchFromPicker(state *branchPickerState, dir, name string, onSwitched func(string)) {
	a.runBranchSwitch(state, dir, name, Tf("Switching to %s…", name), true, func(ctx context.Context, root, branch string) (string, error) {
		return "", git.SwitchBranch(ctx, root, branch)
	}, onSwitched)
}

// carrySwitchBranchFromPicker is the fallback for a checkout git refused
// because local changes would be overwritten: stash, switch, then restore
// with the index. A restore conflict leaves the popover open with a warning
// naming the stash, so the changes are not silently lost.
func (a *App) carrySwitchBranchFromPicker(state *branchPickerState, dir, name string, onSwitched func(string)) {
	a.runBranchSwitch(state, dir, name, Tf("Stashing changes, switching to %s, then restoring them…", name), false, func(ctx context.Context, root, branch string) (string, error) {
		result, err := git.SwitchBranchCarryingChanges(ctx, root, branch)
		if err != nil {
			return "", err
		}
		if result.RestoreConflict {
			return Tf("Switched to %s, but local changes could not be restored cleanly. Resolve the conflicts in your working tree, then drop the stash entry with git stash drop %s.", name, result.StashRef), nil
		}
		return "", nil
	}, onSwitched)
}

// runBranchSwitch runs either the plain checkout or its stash fallback and
// posts the result back to one picker. Gen, Workspace and Switching guard a
// late checkout against a newer list or a closed picker.
func (a *App) runBranchSwitch(state *branchPickerState, dir, name, busy string, offerCarry bool, run func(context.Context, string, string) (string, error), onSwitched func(string)) {
	dir = strings.TrimSpace(dir)
	name = strings.TrimSpace(name)
	if a.closed.Load() || dir == "" || name == "" || state.Switching {
		return
	}
	state.Gen++
	gen := state.Gen
	state.Workspace = dir
	state.Switching = true
	state.Busy = busy
	state.Err = ""
	state.SwitchTarget = name
	state.SwitchFailed = ""
	state.Warning = ""
	go func() {
		ctx, cancel := context.WithTimeout(a.lifeCtx, gitActionTimeout)
		defer cancel()
		warning, err := run(ctx, dir, name)
		a.Post(func() {
			if a.closed.Load() || state.Gen != gen || state.Workspace != dir {
				return
			}
			state.Switching = false
			state.Busy = ""
			if err != nil {
				state.Err = gitErrorText(err)
				// Only a plain checkout offers the fallback: a carry that fails
				// again would loop on the same button.
				if offerCarry && git.LocalChangesBlockSwitch(err) {
					state.SwitchFailed = name
				}
				// Keep the list and the Stash & switch action visible.
				state.Open = true
				return
			}
			for i := range state.Branches {
				state.Branches[i].Current = state.Branches[i].Name == name
			}
			state.Warning = warning
			if onSwitched != nil {
				onSwitched(name)
			}
		})
	}()
}

// branchPopover shows a picker beside anchor. font is the owner's text scale
// (the composer's or the Git panel's) so branch rows follow their area's
// setting. disabled keeps the list readable while the owner runs another
// Git command.
func (a *App) branchPopover(c *mui.Context, anchor mui.Element, state *branchPickerState, current string, font func(float32) float32, disabled bool, onSwitch func(name string, carry bool)) {
	if !state.Open {
		return
	}
	mui.PopoverBase(c, anchor, &state.Open, func(panel mui.Element) {
		t := c.Theme()
		panel.Width(236).Padding(t.Space(1)).Radius(t.Radius+2).
			Background(t.Background).Border(1, t.Border).
			Shadow(0, 6, 20, 0, mui.RGBA(0, 0, 0, 0.18))
		mui.Column(c).Gap(2).Children(func() {
			mui.Row(c).FillWidth().Gap(6).AlignItems(mui.Center).Padding(2, 6).Children(func() {
				mui.Text(c, T("Branches")).FontSize(font(9)).Bold().TextColor(t.TextMuted)
				mui.Spacer(c)
				if state.Busy != "" {
					mui.Spinner(c)
				}
			})
			if state.Busy != "" && len(state.Branches) == 0 {
				a.branchPickerLine(c, font, state.Busy)
			}
			if state.Err != "" {
				a.branchPickerError(c, state, font)
				if state.SwitchFailed != "" {
					a.branchCarrySteps(c, font, state.SwitchFailed)
				}
				mui.Row(c).FillWidth().Gap(4).AlignItems(mui.Center).Wrap().Children(func() {
					if state.SwitchFailed != "" {
						name := state.SwitchFailed
						carry := quietButton(c, T("Stash & switch")).Label("Stash local changes and switch to " + name).
							FontSize(font(8)).TextColor(t.Warning).
							Tooltip(T("Stash staged, unstaged and untracked changes, switch, then restore them. A conflicting restore keeps them in stash@{0}."))
						carry.Disabled(disabled || state.Busy != "")
						if carry.Clicked() && !disabled && state.Busy == "" {
							onSwitch(name, true)
						}
					}
					retry := quietButton(c, T("Retry")).Label(T("Retry loading branches")).FontSize(font(8))
					retry.Disabled(state.Busy != "")
					if retry.Clicked() && state.Busy == "" {
						a.loadBranches(state, state.Workspace)
					}
				})
			}
			if state.Warning != "" {
				mui.Text(c, "⚠ "+state.Warning).FontSize(font(9)).TextColor(t.Warning)
			}
			if state.Busy != "" && len(state.Branches) > 0 {
				a.branchPickerLine(c, font, state.Busy)
			}
			switch {
			case len(state.Branches) == 0:
				if state.Busy == "" && state.Err == "" {
					a.branchPickerLine(c, font, T("No local branches yet"))
				}
			default:
				mui.Scroll(c).MaxHeight(240).Gap(1).Children(func() {
					for _, branch := range state.Branches {
						branch := branch
						selected := branch.Current || branch.Name == current
						item := mui.ButtonBase(c).FillWidth().Gap(6).Padding(4, 8).
							Radius(6).Justify(mui.Start)
						if selected {
							item.Background(t.SurfaceHover)
						}
						label := Tf("Switch to branch %s", branch.Name)
						if selected {
							label = Tf("Current branch %s", branch.Name)
						}
						item.Label(label)
						item.Children(func() {
							mui.Text(c, truncate(branch.Name, 30)).FontSize(font(10)).
								Grow(1).MinWidth(0).SingleLine()
							if branch.Upstream != "" {
								mutedText(c, "→ "+truncate(branch.Upstream, 18), font(8)).Shrink(0)
							}
							if selected {
								mui.Text(c, "✓").FontSize(font(9)).TextColor(t.Accent).Shrink(0)
							}
						})
						item.Disabled(disabled || state.Busy != "")
						if item.Clicked() && !disabled && state.Busy == "" {
							if selected {
								state.Open = false
							} else {
								onSwitch(branch.Name, false)
							}
						}
					}
				})
			}
		})
	})
}

// gitBranchChip is the composer's branch chip. In a draft it opens the
// branch picker so the branch is chosen before session/new; once a session
// exists it only reports the branch the agent started on.
func (a *App) gitBranchChip(c *mui.Context, chat *Chat) {
	branch := chat.gitBranch()
	if branch == "" {
		return
	}
	draft := chat.Session == nil && !chat.Connecting && chat.Busy == "" && !chat.taskActive && !chat.sleeping()
	chip := a.composerControl(chipBase(c)).Label(Tf("Git branch: %s", branch))
	chip.Children(func() {
		mui.Text(c, "⑂ "+truncate(branch, 24)).FontSize(a.composerFont(9)).SingleLine()
	})
	if !draft {
		chip.Tooltip(Tf("Git branch in %s; start a new chat to switch", chat.Workspace))
		return
	}
	chip.Tooltip(T("Switch Git branch before the session starts"))
	if chip.Clicked() {
		a.toggleBranchPicker(&chat.BranchPicker, chat.Workspace)
	}
	a.branchPopover(c, chip, &chat.BranchPicker, branch, a.composerFont,
		chat.Busy != "" || chat.Connecting, func(name string, carry bool) {
			workspace := chat.Workspace
			onSwitched := func(switched string) {
				if chat.BranchPicker.Warning == "" {
					chat.BranchPicker.Open = false
				}
				a.branchSwitched(workspace, switched, &chat.BranchPicker)
			}
			if carry {
				a.carrySwitchBranchFromPicker(&chat.BranchPicker, workspace, name, onSwitched)
				return
			}
			a.switchBranchFromPicker(&chat.BranchPicker, workspace, name, onSwitched)
		})
}

// branchSwitched refreshes everything that names the branch: every draft in
// the workspace, the Git panel when it shows that repository, and the Files
// tab. A hidden panel only marks itself stale.
func (a *App) branchSwitched(workspace, name string, except *branchPickerState) {
	for _, chat := range a.Chats {
		if sameWorkspace(chat.Workspace, workspace) {
			// Keep the new name without clearing the cache: clearing it would
			// make gitBranch reset the picker and drop a carry warning.
			chat.gitWorkspace = strings.TrimSpace(chat.Workspace)
			chat.gitBranchName = name
			if &chat.BranchPicker != except {
				chat.BranchPicker.close()
			}
		}
	}
	if sameWorkspace(a.Git.Workspace, workspace) {
		// The diff card and the expanded commit show files from the branch
		// that was checked out; the checkout may have replaced them.
		a.closeGitPreview()
		a.closeCommitFiles()
		a.closeCommitHover()
		a.Git.Branch = name
		if &a.Git.BranchPicker != except {
			a.Git.BranchPicker.close()
		}
		if a.GitPanelOpen && a.RightTab == rightTabGit {
			a.startGitLoad(a.Git.Workspace, false)
		} else {
			a.Git.Checked = false
		}
	}
	if sameWorkspace(a.Files.Workspace, workspace) {
		a.closeFileViewer()
		a.closeFileDialogs()
		if a.GitPanelOpen && a.RightTab == rightTabFiles {
			a.refreshFilesOnFocus()
		} else {
			a.Files.Checked = false
		}
	}
}

// branchPickerError explains a failed checkout: which branch was tried,
// whether anything changed, and git's own output in full. Tabs become
// indents and line breaks stay, because the file list and the closing hint
// are what the user acts on; the popover wraps long lines itself.
func (a *App) branchPickerError(c *mui.Context, state *branchPickerState, font func(float32) float32) {
	t := c.Theme()
	if state.SwitchTarget != "" {
		mui.Text(c, Tf("Could not switch to %s.", state.SwitchTarget)).
			FontSize(font(9)).Bold().TextColor(t.Danger)
	}
	if state.SwitchFailed != "" {
		mutedText(c, T("Local changes or untracked files would be overwritten, so nothing was switched."), font(8))
	}
	mui.Text(c, gitErrorLines(state.Err)).Font("monospace").
		FontSize(font(8)).TextColor(t.Danger)
}

// branchCarrySteps writes out what Stash & switch runs, because the button's
// tooltip alone is invisible and the stash is the only copy of the changes
// when the restore conflicts.
func (a *App) branchCarrySteps(c *mui.Context, font func(float32) float32, name string) {
	mui.Text(c, T("Stash & switch does this:")).FontSize(font(8)).Bold()
	mutedText(c, T("1. Stash staged, unstaged and untracked changes."), font(8))
	mutedText(c, Tf("2. Switch to %s.", name), font(8))
	mutedText(c, T("3. Restore them with git stash pop --index."), font(8))
	mutedText(c, T("If the restore conflicts, the switch still happens and the changes stay in stash@{0}."), font(8))
}

// gitErrorLines keeps a git error's lines and shows tabs as indents so the
// file list under the first line stays readable in the popover.
func gitErrorLines(text string) string {
	return strings.ReplaceAll(strings.TrimSpace(text), "\t", "  ")
}

// branchPickerLine is a muted status inside the branch popover. Its text
// wraps instead of being cut when a branch name makes it long.
func (a *App) branchPickerLine(c *mui.Context, font func(float32) float32, text string) {
	mutedText(c, singleLine(text), font(9)).Padding(3, 6)
}
