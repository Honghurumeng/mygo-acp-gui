package ui

import (
	"path/filepath"
	"strings"

	"github.com/reed/acp-gui/internal/settings"
)

// persistSettings writes the last agent, workspace and MCP defaults. It is
// called from UI events and again on shutdown, and does nothing when the app
// was started without a data directory (tests, portable runs).
func (a *App) persistSettings() {
	if a.settings == nil {
		return
	}
	file := settings.File{Version: settings.CurrentVersion, Paths: a.settingsPaths}
	file.Workspaces = a.savedWorkspaces()
	file.RemovedWorkspaces = a.removedWorkspaceList()
	file.CollapsedWorkspaces = a.collapsedWorkspaceList()
	if chat := a.activeChat(); chat != nil {
		file.LastAgent = chat.Agent.ID
		file.LastWorkspace = strings.TrimSpace(chat.Workspace)
		file.MCPServers = append([]any(nil), chat.MCPServers...)
	} else {
		if a.AgentIndex >= 0 && a.AgentIndex < len(a.Agents) {
			file.LastAgent = a.Agents[a.AgentIndex].ID
		}
		file.LastWorkspace = strings.TrimSpace(a.Workspace)
		file.MCPServers = append([]any(nil), a.MCPServers...)
	}
	if err := a.settings.Save(file); err != nil {
		a.SettingsWarning = err.Error()
	}
}

// savedWorkspaces returns the sidebar list plus any workspace currently in
// use, so a chat moved by opening a saved session is not lost on restart.
func (a *App) savedWorkspaces() []string {
	seen := make(map[string]bool, len(a.Workspaces)+len(a.Chats)+1)
	out := make([]string, 0, len(a.Workspaces)+len(a.Chats)+1)
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || !filepath.IsAbs(path) {
			return
		}
		path = filepath.Clean(path)
		if seen[path] {
			return
		}
		seen[path] = true
		out = append(out, path)
	}
	for _, workspace := range a.Workspaces {
		add(workspace)
	}
	add(a.Workspace)
	for _, chat := range a.Chats {
		add(chat.Workspace)
	}
	return out
}
