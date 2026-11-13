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

const savedSessionsTimeout = 30 * time.Second

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
	if a.closed.Load() {
		return
	}
	spec, ok := a.preferredAgent()
	if !ok {
		return
	}
	a.startSavedSync(spec.ID, "")
}

// startSavedSync starts one session/list call for agentID and reports whether
// a call was started. An empty cwdFilter asks for every workspace; a non-empty
// one asks the agent to return just that workspace. It prefers a live process
// for that agent, so a session connecting in a background tab imports its own
// agent instead of whichever tab happens to be active.
func (a *App) startSavedSync(agentID, cwdFilter string) bool {
	if a.closed.Load() {
		return false
	}
	spec, ok := a.agentByID(agentID)
	if !ok {
		spec, ok = a.preferredAgent()
	}
	if !ok {
		return false
	}
	var s *session.Session
	dir := a.defaultWorkspace()
	if filepath.IsAbs(cwdFilter) {
		dir = cwdFilter
	}
	for _, chat := range a.Chats {
		if chat.Session == nil || chat.Agent.ID != spec.ID {
			continue
		}
		s = chat.Session
		if trim := strings.TrimSpace(chat.Workspace); trim != "" && cwdFilter == "" {
			dir = trim
		}
		if chat == a.activeChat() {
			break
		}
	}
	if s == nil && a.Resolving {
		// No process to ask yet. Leave savedSynced unset so the next connect
		// retries instead of losing this agent's history for the whole run.
		return false
	}

	key := spec.ID
	if cwdFilter != "" {
		key = spec.ID + "\x00" + canonicalWorkspace(cwdFilter)
	}
	if a.savedVersions == nil {
		a.savedVersions = make(map[string]int)
	}
	a.savedVersions[key]++
	version := a.savedVersions[key]
	a.SavedLoading = true
	a.SavedError = ""

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
			sessions, err = s.ListSessions(ctx, cwdFilter)
		} else {
			sessions, err = session.ListSessionsIn(ctx, spec, dir, cwdFilter)
		}
		a.Post(func() {
			if a.closed.Load() || version != a.savedVersions[key] {
				return
			}
			a.SavedLoading = false
			if err != nil {
				a.SavedError = err.Error()
				if cwdFilter == "" {
					// Retry the full sync after the next connect rather than
					// losing this agent for the rest of the run.
					delete(a.savedSynced, spec.ID)
				}
				return
			}
			a.SavedError = ""
			a.mergeArchivedSessions(spec.ID, sessions)
		})
	}()
	return true
}

// syncWorkspaceSessions imports the saved sessions of one workspace from every
// ready agent. It runs when the user adds a workspace so its existing history
// appears without waiting for a restart. Sessions from other workspaces are
// never added to the sidebar or the archive.
func (a *App) syncWorkspaceSessions(workspace string) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" || !filepath.IsAbs(workspace) {
		return
	}
	for _, spec := range a.Agents {
		a.startSavedSync(spec.ID, workspace)
	}
}

// syncSavedSessionsOnce imports an agent's session/list once per run. It runs
// for every ready agent right after discovery, and again when an agent
// connects in case the startup sync failed. The sidebar has no manual refresh
// button; a short-lived process is spawned when no live process exists.
func (a *App) syncSavedSessionsOnce(agentID string) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return
	}
	if a.savedSynced == nil {
		a.savedSynced = make(map[string]bool)
	}
	if a.savedSynced[agentID] {
		return
	}
	if !a.startSavedSync(agentID, "") {
		return
	}
	a.savedSynced[agentID] = true
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

// workspaceKnown reports whether cwd belongs to a workspace the user already
// added. session/list import uses it to ignore every other project.
func (a *App) workspaceKnown(cwd string) bool {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return false
	}
	target := canonicalWorkspace(cwd)
	for _, workspace := range a.Workspaces {
		if sameWorkspace(workspace, cwd) || sameWorkspace(canonicalWorkspace(workspace), target) {
			return true
		}
	}
	return false
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
			mutedText(c, icon, a.workspaceFont(9))
			agentBadge(c, item.AgentID, 10)
			mui.Text(c, truncate(display, 60)).FontSize(a.workspaceFont(11)).Grow(1).SingleLine()
			if stamp := savedTimeLabel(item.UpdatedAt); stamp != "" {
				mutedText(c, stamp, a.workspaceFont(8))
			}
		})
		if button.Clicked() {
			a.SavedMenuOpen = ""
			a.openSavedSession(item)
		}

		menu := menuTrigger(c, "Saved session actions for "+title, t.Background, a.workspaceFont(11))
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
				if quietButton(c, "Rename").FillWidth().FontSize(a.workspaceFont(11)).Clicked() {
					open = false
					a.SavedMenuOpen = ""
					a.openSavedRename(item)
				}
				if hidden {
					if quietButton(c, "Restore").FillWidth().FontSize(a.workspaceFont(11)).Clicked() {
						open = false
						a.SavedMenuOpen = ""
						a.setArchivedShow(item.AgentID, item.SessionID, true)
					}
				} else if quietButton(c, "Delete").FillWidth().FontSize(a.workspaceFont(11)).TextColor(t.Danger).Clicked() {
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
		title = cleanSessionTitle(item.Title)
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

func (a *App) agentByID(agentID string) (agent.LaunchSpec, bool) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return agent.LaunchSpec{}, false
	}
	for _, spec := range a.Agents {
		if spec.ID == agentID {
			return spec, true
		}
	}
	return agent.LaunchSpec{}, false
}

func (a *App) savedSessionAgent(item savedSession) (agent.LaunchSpec, bool) {
	agentID := strings.TrimSpace(item.AgentID)
	if agentID == "" {
		return a.preferredAgent()
	}
	return a.agentByID(agentID)
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
