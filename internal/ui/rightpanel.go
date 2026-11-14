package ui

import (
	mui "github.com/egoist/mygo/ui"
)

// The right column has two tabs that share its width and open state.
const (
	rightTabGit   = "git"
	rightTabTasks = "tasks"
)

// rightPanelName is the title inside the visible panel. The Git / Tasks
// switch lives in the conversation header; the panel itself only names the
// tab that is already showing.
func (a *App) rightPanelName(c *mui.Context, name string) {
	mui.Text(c, name).FontSize(a.gitFont(11)).Bold()
}

// setRightTab switches the visible right panel and closes floating cards
// that belong to the other tab. It does not open a closed panel.
func (a *App) setRightTab(tab string) {
	if tab != rightTabGit && tab != rightTabTasks {
		return
	}
	if a.RightTab == tab {
		return
	}
	a.RightTab = tab
	a.Git.DiscardOpen = false
	a.closeGitOverlays()
	a.closeTaskSummary()
	if tab == rightTabGit && a.Git.Workspace != "" {
		// The repository may have changed while the Tasks tab was visible.
		a.Git.Checked = false
	}
}

// toggleRightPanel is the header's behavior: show the requested tab, switch
// to it when the other one is visible, or close the column when it is already
// showing that tab.
func (a *App) toggleRightPanel(tab string) {
	if a.GitPanelOpen && a.RightTab == tab {
		a.GitPanelOpen = false
		a.closeGitOverlays()
		a.closeTaskSummary()
		a.Git.DiscardOpen = false
		return
	}
	a.GitPanelOpen = true
	a.setRightTab(tab)
}

// closeGitOverlays dismisses the Git diff card, the commit details card and
// the expanded commit files. Used when the right column leaves the Git tab.
func (a *App) closeGitOverlays() {
	a.closeCommitHover()
	a.closeGitPreview()
}
