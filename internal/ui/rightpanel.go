package ui

import (
	mui "github.com/egoist/mygo/ui"
)

// The right column has two tabs that share its width and open state.
const (
	rightTabGit   = "git"
	rightTabTasks = "tasks"
)

// The rail is the narrow strip at the window's right edge that switches the
// right column: one icon button per tab, stacked. It is always visible, so a
// closed column can still be opened from either tab.
const (
	rightRailWidth      = 38
	rightRailButtonSize = 28
	rightRailIconSize   = 16
)

// rightRail draws the tab buttons to the right of the column. They replaced
// the Git / Tasks text buttons that used to sit in the conversation header.
func (a *App) rightRail(c *mui.Context) {
	t := c.Theme()
	mui.Column(c).Width(rightRailWidth).FillHeight().Gap(2).Padding(8, 0).
		AlignItems(mui.Center).Background(sunkenSurface(t)).
		BorderWidth(1, 0, 0, 0).BorderColor(t.Border).
		Role(mui.RoleTabList).Label("Right panel tabs").Children(func() {
		a.rightRailButton(c, gitBranchIcon, "Git", rightTabGit)
		a.rightRailButton(c, listChecksIcon, "Tasks", rightTabTasks)
	})
}

// rightRailButton is one tab's icon button: accented while its tab shows and
// muted otherwise. The label is also the tooltip and the name tests and
// assistive technology use, so it stays a sentence rather than a glyph.
func (a *App) rightRailButton(c *mui.Context, icon *mui.SVG, name, tab string) {
	t := c.Theme()
	active := a.GitPanelOpen && a.RightTab == tab
	label := "Show " + name + " panel"
	if active {
		label = "Hide " + name + " panel"
	}
	b := buttonBase(c, label).Size(rightRailButtonSize, rightRailButtonSize).
		Center().Tooltip(label).Role(mui.RoleTab).AccessSelected(active)
	// The rail sits below the surface its buttons normally use, so the state
	// fills are tinted with the text color instead of SurfaceHover and
	// SurfacePressed: text darkens in light themes and lightens in dark ones,
	// which stays visible on every palette where those two colors may not.
	switch {
	case active:
		b.Background(t.Accent.Alpha(0.14))
	case b.Pressed():
		b.Background(t.Text.Alpha(0.10))
	case b.Hovered():
		b.Background(t.Text.Alpha(0.04))
	}
	fg := t.TextMuted
	if active {
		fg = t.Accent
	}
	b.Children(func() {
		mui.Icon(c, icon).Size(rightRailIconSize, rightRailIconSize).TextColor(fg)
	})
	if b.Clicked() {
		a.toggleRightPanel(tab)
	}
}

// rightPanelName is the title inside the visible panel. The Git / Tasks
// switch lives in the rail at the window's right edge; the panel itself only
// names the tab that is already showing.
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

// toggleRightPanel is the rail's behavior: show the requested tab, switch to
// it when the other one is visible, or close the column when it is already
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
