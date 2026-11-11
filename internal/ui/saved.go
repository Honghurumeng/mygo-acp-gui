package ui

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"time"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/session"
)

const savedSessionsTimeout = 15 * time.Second

// savedSession is one sidebar row. The archive record supplies AgentID,
// ShowName and Show; the embedded summary keeps the agent metadata.
type savedSession struct {
	acp.SessionSummary
	AgentID  string
	ShowName string
	Show     bool
	// archived distinguishes rows loaded from the local archive from rows
	// constructed directly in tests or future callers. Non-archive rows are
	// treated as visible.
	archived  bool
	workspace string
}

// refreshSavedSessions merges the selected agent's session/list into the local
// archive. It runs once per agent after the first successful connect (see
// syncSavedSessionsOnce); startup never waits for an agent just to draw the
// saved list. Existing Show/ShowName values win, so a deleted session stays
// deleted and a rename stays renamed.
func (a *App) refreshSavedSessions() {
	if a.closed.Load() || a.Resolving {
		return
	}
	spec, ok := a.preferredAgent()
	if !ok {
		return
	}
	chat := a.activeChat()
	var s *session.Session
	if chat != nil && chat.Agent.ID == spec.ID {
		s = chat.Session
	}

	a.savedVersion++
	version := a.savedVersion
	a.SavedLoading = true
	a.SavedError = ""
	dir := a.defaultWorkspace()

	a.cleanup.Add(1)
	go func() {
		defer a.cleanup.Done()
		base := a.lifeCtx
		if base == nil {
			base = context.Background()
		}
		ctx, cancel := context.WithTimeout(base, savedSessionsTimeout)
		defer cancel()
		var (
			sessions []acp.SessionSummary
			err      error
		)
		if s != nil {
			sessions, err = s.ListSessions(ctx, "")
		} else {
			sessions, err = session.ListSessionsIn(ctx, spec, dir, "")
		}
		a.Post(func() {
			if a.closed.Load() || version != a.savedVersion {
				return
			}
			a.SavedLoading = false
			if err != nil {
				a.SavedError = err.Error()
				return
			}
			a.SavedError = ""
			a.mergeArchivedSessions(spec.ID, sessions)
		})
	}()
}

// syncSavedSessionsOnce imports an agent's session/list the first time a live
// process for it exists. The sidebar has no manual refresh button, so the
// archive is filled from an agent that is already running instead of spawning
// a short-lived process at startup.
func (a *App) syncSavedSessionsOnce(agentID string) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" || a.Resolving {
		return
	}
	if a.savedSynced == nil {
		a.savedSynced = make(map[string]bool)
	}
	if a.savedSynced[agentID] {
		return
	}
	a.savedSynced[agentID] = true
	a.refreshSavedSessions()
}

func sortSavedSessions(sessions []savedSession) []savedSession {
	out := append([]savedSession(nil), sessions...)
	sort.SliceStable(out, func(i, j int) bool {
		return savedUpdatedAt(out[i].SessionSummary).After(savedUpdatedAt(out[j].SessionSummary))
	})
	return out
}

func savedUpdatedAt(item acp.SessionSummary) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, item.UpdatedAt); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

// canonicalWorkspace resolves symlinks for comparisons. Grouping sessions by
// cwd must not depend on whether the user picked /var or /private/var.
func canonicalWorkspace(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return path
}

// workspacePath returns the sidebar spelling for a saved session's cwd, so a
// resolved symlink does not create a duplicate workspace group.
func (a *App) workspacePath(cwd string) string {
	if !filepath.IsAbs(cwd) {
		return cwd
	}
	target := canonicalWorkspace(cwd)
	for _, workspace := range a.Workspaces {
		if sameWorkspace(workspace, cwd) || sameWorkspace(canonicalWorkspace(workspace), target) {
			return workspace
		}
	}
	return filepath.Clean(cwd)
}

// savedSessionsFor returns the saved sessions shown under one workspace.
// Sessions already open in a tab are omitted: the tab itself represents them.
func (a *App) savedSessionsFor(workspace string) []savedSession {
	seen := make(map[string]bool)
	workspaceCanonical := canonicalWorkspace(workspace)
	var out []savedSession
	for _, item := range a.SavedSessions {
		if strings.TrimSpace(item.SessionID) == "" {
			continue
		}
		key := item.AgentID + "\x00" + item.SessionID
		if seen[key] {
			continue
		}
		if a.savedSessionOpen(item) {
			continue
		}
		if item.archived && !item.Show && !a.ShowDeleted {
			continue
		}
		cwd := item.workspace
		if cwd == "" {
			cwd = canonicalWorkspace(a.savedSessionFallback())
		}
		if !sameWorkspace(cwd, workspace) && !sameWorkspace(cwd, workspaceCanonical) {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

// savedSessionFallback handles agents that omit cwd from session/list. Those
// sessions belong to the current conversation's workspace.
func (a *App) savedSessionFallback() string {
	if chat := a.activeChat(); chat != nil && strings.TrimSpace(chat.Workspace) != "" {
		return chat.Workspace
	}
	return a.Workspace
}

func (a *App) savedSessionOpen(item savedSession) bool {
	for _, chat := range a.Chats {
		if item.AgentID != "" && chat.Agent.ID != "" && item.AgentID != chat.Agent.ID {
			continue
		}
		if chat.Info.SessionID == item.SessionID || chat.pendingSessionID == item.SessionID {
			return true
		}
	}
	return false
}

func (a *App) savedSessionRow(c *mui.Context, item savedSession) {
	t := c.Theme()
	title := savedSessionTitle(item)
	hidden := item.archived && !item.Show
	rowKey := item.AgentID + "\x00" + item.SessionID
	display := title
	if hidden {
		display = "[deleted] " + title
	}
	mui.Row(c).Gap(2).AlignItems(mui.Center).Children(func() {
		button := mui.ButtonBase(c).Grow(1).Gap(5).Padding(4, 10).Radius(8).
			Background(t.Background).
			Label("Open saved session: " + title)
		button.Children(func() {
			icon := "◷"
			if hidden {
				icon = "⌫"
			}
			mui.Text(c, icon).FontSize(9).TextColor(t.TextMuted)
			agentBadge(c, item.AgentID, 10)
			mui.Text(c, truncate(display, 60)).FontSize(11).Grow(1).SingleLine()
			if stamp := savedTimeLabel(item.UpdatedAt); stamp != "" {
				mui.Text(c, stamp).FontSize(8).TextColor(t.TextMuted)
			}
		})
		tooltip := "Saved session · " + title
		if item.AgentID != "" {
			tooltip += "\nAgent: " + item.AgentID
		}
		if item.UpdatedAt != "" {
			tooltip += "\nUpdated " + formatTimestamp(item.UpdatedAt)
		}
		if item.Cwd != "" {
			tooltip += "\n" + item.Cwd
		}
		if hidden {
			tooltip += "\nDeleted locally; restore from the ⋯ menu."
		} else {
			tooltip += "\nClick to open this session and replay its history."
		}
		button.Tooltip(tooltip)
		if button.Clicked() {
			a.SavedMenuOpen = ""
			a.openSavedSession(item)
		}

		menu := mui.ButtonBase(c).Padding(4, 6).Radius(8).Background(t.Background).
			Label("Saved session actions for " + title)
		menu.Children(func() {
			mui.Text(c, "⋯").FontSize(11).TextColor(t.TextMuted)
		})
		if menu.Clicked() {
			if a.SavedMenuOpen == rowKey {
				a.SavedMenuOpen = ""
			} else {
				a.SavedMenuOpen = rowKey
			}
		}
		open := a.SavedMenuOpen == rowKey
		mui.Popover(c, menu, &open, func() {
			mui.Column(c).Width(132).Gap(2).Children(func() {
				if mui.Button(c, "Rename").FillWidth().FontSize(11).Clicked() {
					open = false
					a.SavedMenuOpen = ""
					a.openSavedRename(item)
				}
				if hidden {
					if mui.Button(c, "Restore").FillWidth().FontSize(11).Clicked() {
						open = false
						a.SavedMenuOpen = ""
						a.setArchivedShow(item.AgentID, item.SessionID, true)
					}
				} else if mui.Button(c, "Delete").FillWidth().FontSize(11).TextColor(t.Danger).Clicked() {
					open = false
					a.SavedMenuOpen = ""
					a.hideArchivedRecord(item.AgentID, item.SessionID, item.Cwd, item.Title)
				}
			})
		})
		if !open && a.SavedMenuOpen == rowKey {
			a.SavedMenuOpen = ""
		}
	})
}

func savedSessionTitle(item savedSession) string {
	title := strings.TrimSpace(item.ShowName)
	if title == "" {
		title = strings.TrimSpace(item.Title)
	}
	if title == "" {
		title = truncate(strings.TrimSpace(item.SessionID), 24)
	}
	if title == "" {
		title = "(untitled)"
	}
	return singleLine(title)
}

func savedTimeLabel(value string) string {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			local := parsed.Local()
			now := time.Now()
			if local.Year() == now.Year() && local.YearDay() == now.YearDay() {
				return local.Format("15:04")
			}
			return local.Format("Jan 2")
		}
	}
	return ""
}

// openSavedSession opens a saved session by replaying its history. The app
// does not persist message bodies locally, so session/load is the only way to
// show the conversation again after a restart. The agent is taken from the
// archive record, not from the current selection, so a session created with
// another agent still opens with the right runtime.
func (a *App) openSavedSession(item savedSession) {
	sessionID := strings.TrimSpace(item.SessionID)
	if sessionID == "" {
		return
	}
	spec, ok := a.savedSessionAgent(item)
	if !ok {
		agentName := strings.TrimSpace(item.AgentID)
		if agentName == "" {
			agentName = "the selected agent"
		}
		if a.Resolving {
			a.SavedError = "Agent " + agentName + " is still being resolved; try again in a moment."
		} else {
			a.SavedError = "Agent " + agentName + " is not available for this saved session."
		}
		return
	}
	a.SavedError = ""
	if item.archived && !item.Show {
		a.setArchivedShow(item.AgentID, item.SessionID, true)
	}
	title := savedSessionTitle(item)
	cwd := strings.TrimSpace(item.Cwd)
	if !filepath.IsAbs(cwd) {
		cwd = ""
	}
	workspace := ""
	if cwd != "" {
		workspace = a.workspacePath(cwd)
	}

	chat := a.activeChat()
	reuse := chat != nil && !chat.Connecting && chat.Agent.ID == spec.ID
	if reuse && chat.Session != nil && (len(chat.Messages) > 0 || chat.Streaming || chat.Busy != "") {
		reuse = false
	}
	// An agent process is bound to the workspace it was started in;
	// command-code rejects session/load with another cwd. Reuse only a
	// process that already serves the saved session's workspace, otherwise
	// open a tab with its own process.
	if reuse && chat.Session != nil && cwd != "" && canonicalWorkspace(chat.Workspace) != canonicalWorkspace(cwd) {
		reuse = false
	}
	switch {
	case chat == nil:
		if workspace != "" {
			chat = a.newChatTabIn(workspace)
		} else {
			chat = a.ensureChat()
		}
	case !reuse:
		// Keep a chat that is already doing something and open the saved
		// session in another tab, so the running conversation is not lost.
		if workspace != "" {
			chat = a.newChatTabIn(workspace)
		} else {
			chat = a.newChatTab()
		}
	default:
		a.selectChat(chat)
	}
	chat.Agent = spec
	a.AgentIndex = a.agentIndex(spec.ID)
	if workspace != "" {
		chat.Workspace = workspace
		a.Workspace = workspace
		a.showWorkspace(workspace)
		a.WorkspaceCollapsed[workspace] = false
	}
	if chat.Session != nil {
		a.loadHistory(sessionID, title, cwd)
		return
	}
	chat.pendingSessionID = sessionID
	chat.pendingSessionAction = "load"
	chat.pendingSessionTitle = title
	chat.Title = title
	a.Connect()
}

func (a *App) savedSessionAgent(item savedSession) (agent.LaunchSpec, bool) {
	agentID := strings.TrimSpace(item.AgentID)
	if agentID == "" {
		return a.preferredAgent()
	}
	for _, spec := range a.Agents {
		if spec.ID == agentID {
			return spec, true
		}
	}
	return agent.LaunchSpec{}, false
}

func (a *App) openSavedRename(item savedSession) {
	if strings.TrimSpace(item.SessionID) == "" {
		return
	}
	a.RenameChat = nil
	a.RenameSaved = true
	a.RenameAgentID = item.AgentID
	a.RenameSessionID = item.SessionID
	a.RenameText = savedSessionTitle(item)
	a.RenameOpen = true
}

func formatTimestamp(value string) string {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Local().Format("2006-01-02 15:04")
		}
	}
	return value
}
