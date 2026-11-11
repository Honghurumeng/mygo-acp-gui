package ui

import (
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
