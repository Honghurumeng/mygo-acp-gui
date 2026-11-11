package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/logs"
)

// stderrTailLimit bounds the in-memory stderr shown per chat. Older bytes
// stay in the log file.
const stderrTailLimit = 64 * 1024

// openDiagnostics shows the status and stderr of the active chat, or the
// discovery statuses when no chat is open.
func (a *App) openDiagnostics() {
	a.DiagChat = a.activeChat()
	a.DiagOpen = true
}

func (a *App) diagnosticsDialog(c *mui.Context) {
	if !a.DiagOpen {
		return
	}
	t := c.Theme()
	chat := a.DiagChat
	if chat != nil && a.chatIndex(chat) < 0 {
		chat = nil
	}
	mui.Modal(c, &a.DiagOpen, func() {
		mui.Column(c).Gap(10).Width(760).Children(func() {
			title := "Diagnostics"
			if chat != nil {
				title = "Diagnostics — " + chat.agentName()
			}
			mui.Text(c, title).FontSize(18).Bold()
			a.diagnosticStatuses(c)
			if chat == nil {
				mui.Text(c, "Open a chat to see its stderr buffer.").FontSize(11).TextColor(t.TextMuted)
				if a.logDir != "" {
					mui.Text(c, "Log folder: "+a.logDir).FontSize(10).TextColor(t.TextMuted).Selectable()
				}
			} else {
				a.chatDiagnostics(c, chat)
			}
			mui.Row(c).Gap(8).Justify(mui.End).Children(func() {
				if mui.Button(c, "Close").Clicked() {
					a.DiagOpen = false
				}
			})
		})
	})
}

func (a *App) diagnosticStatuses(c *mui.Context) {
	if len(a.AgentStatuses) == 0 {
		return
	}
	t := c.Theme()
	for _, status := range a.AgentStatuses {
		color := t.TextMuted
		switch status.Status {
		case AgentReady:
			color = t.Success
		case AgentProbing, AgentNeedsAuth:
			color = t.Warning
		case AgentMissing, AgentNotACP, AgentError:
			color = t.Danger
		}
		mui.Text(c, status.Name+": "+status.Status).FontSize(11).TextColor(color)
		if status.Status != AgentReady && strings.TrimSpace(status.Detail) != "" {
			mui.Text(c, truncate(status.Detail, 2000)).
				FontSize(10).
				TextColor(t.TextMuted).
				Selectable()
		}
		if a.logDir != "" && status.Status != AgentReady {
			mui.Text(c, "log: "+a.agentLogPath(status.ID)).
				FontSize(10).
				TextColor(t.TextMuted).
				Selectable()
		}
	}
}

func (a *App) chatDiagnostics(c *mui.Context, chat *Chat) {
	t := c.Theme()
	mui.Text(c, "Agent: "+chat.agentName()).FontSize(11).TextColor(t.TextMuted)
	mui.Text(c, "Workspace: "+chat.Workspace).FontSize(11).TextColor(t.TextMuted).Selectable()
	if chat.Agent.Command != "" {
		mui.Text(c, "Command: "+chat.Agent.CommandLine()).
			FontSize(10).
			TextColor(t.TextMuted).
			Selectable()
	}
	if chat.ConnectError != "" {
		mui.Text(c, chat.ConnectError).FontSize(11).TextColor(t.Danger).Selectable()
	}
	if chat.WorkspaceError != "" {
		mui.Text(c, chat.WorkspaceError).FontSize(11).TextColor(t.Danger).Selectable()
	}
	if chat.Agent.LoginHint != "" {
		mui.Text(c, "Login: "+chat.Agent.LoginHint).FontSize(10).TextColor(t.TextMuted).Selectable()
	}
	if path := a.agentLogPath(chat.Agent.ID); path != "" {
		mui.Row(c).Gap(8).AlignItems(mui.Center).Children(func() {
			mui.Text(c, "Log: "+path).
				FontSize(10).
				TextColor(t.TextMuted).
				Selectable().
				Grow(1)
			if mui.Button(c, "Reveal log").FontSize(10).Clicked() {
				mygo.Shell.ShowItemInFolder(path)
			}
		})
	}
	mui.Text(c, stderrLabel(chat)).FontSize(12)
	if chat.Stderr == "" {
		mui.Text(c, "No stderr output.").FontSize(11).TextColor(t.TextMuted)
		return
	}
	mui.TextArea(c, &chat.Stderr).
		Label("stderr").
		ReadOnly(true).
		Font("monospace").
		FontSize(11).
		Height(300)
}

func stderrLabel(chat *Chat) string {
	if chat == nil || chat.StderrBytes == 0 {
		return "stderr"
	}
	shown := int64(len(chat.Stderr))
	if shown >= chat.StderrBytes {
		return "stderr — " + byteSize(chat.StderrBytes)
	}
	return "stderr — last " + byteSize(shown) + " of " + byteSize(chat.StderrBytes)
}

func byteSize(n int64) string {
	const unit = 1024
	switch {
	case n < unit:
		return fmt.Sprintf("%d B", n)
	case n < unit*unit:
		return fmt.Sprintf("%.1f KiB", float64(n)/unit)
	default:
		return fmt.Sprintf("%.1f MiB", float64(n)/(unit*unit))
	}
}

// appendStderr keeps the tail of agent stderr in the chat for Diagnostics.
// It runs on the UI thread.
func (a *App) appendStderr(chat *Chat, chunk string) {
	if chat == nil || chunk == "" {
		return
	}
	chat.StderrBytes += int64(len(chunk))
	chat.Stderr += chunk
	if len(chat.Stderr) > stderrTailLimit {
		chat.Stderr = chat.Stderr[len(chat.Stderr)-stderrTailLimit:]
	}
}

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
