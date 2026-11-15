package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/logs"
)

// logSessionStart marks a new connection attempt in the agent's log file.
func (a *App) logSessionStart(spec agent.LaunchSpec, workspace, action, sessionID string) {
	if action == "" {
		action = "new"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "=== %s | %s | %s | %s", time.Now().Format(time.RFC3339), spec.Name, action, workspace)
	if sessionID != "" {
		fmt.Fprintf(&b, " | %s", sessionID)
	}
	b.WriteString(" ===\n")
	fmt.Fprintf(&b, "command: %s\n", spec.CommandLine())
	a.appendAgentLog(spec.ID, []byte(b.String()))
}

// appendAgentLog writes raw agent output to its file. The session callbacks
// call it from their own goroutines, so it takes the logs mutex and tolerates
// a closed app or an unavailable file.
func (a *App) appendAgentLog(agentID string, data []byte) {
	if a.logDir == "" || len(data) == 0 {
		return
	}
	a.logsMu.Lock()
	defer a.logsMu.Unlock()
	if a.logsClosed {
		return
	}
	entry := a.logs[agentID]
	if entry == nil {
		opened, err := logs.Open(a.agentLogPath(agentID), logs.DefaultMaxBytes)
		if err != nil {
			return
		}
		if a.logs == nil {
			a.logs = make(map[string]*logs.Log)
		}
		a.logs[agentID] = opened
		entry = opened
	}
	_, _ = entry.Write(data)
}

func (a *App) closeLogs() {
	a.logsMu.Lock()
	defer a.logsMu.Unlock()
	a.logsClosed = true
	for id, entry := range a.logs {
		_ = entry.Close()
		delete(a.logs, id)
	}
}

func (a *App) agentLogPath(agentID string) string {
	if a.logDir == "" {
		return ""
	}
	return filepath.Join(a.logDir, safeLogName(agentID)+".log")
}

func safeLogName(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "agent"
	}
	return b.String()
}
