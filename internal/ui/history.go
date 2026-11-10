package ui

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/session"
)

func (a *App) openHistory() {
	if a.HistoryLoading || a.Resolving || a.closed.Load() {
		return
	}
	a.HistoryOpen = true
	a.HistoryError = ""
	a.HistorySelected = 0
	a.loadHistorySessions()
}

func (a *App) loadHistorySessions() {
	if a.HistoryLoading || a.closed.Load() {
		return
	}
	spec, ok := a.currentAgent()
	if !ok {
		a.HistoryError = "No agent selected."
		return
	}
	cwd := ""
	if workspace := strings.TrimSpace(a.Workspace); !a.HistoryAll && filepath.IsAbs(workspace) {
		cwd = workspace
		if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
			cwd = resolved
		}
	}
	a.HistoryLoading = true
	a.HistoryError = ""
	s := a.Session
	a.cleanup.Add(1)
	go func() {
		defer a.cleanup.Done()
		base := a.lifeCtx
		if base == nil {
			base = context.Background()
		}
		ctx, cancel := context.WithTimeout(base, 15*time.Second)
		defer cancel()
		var (
			sessions []acp.SessionSummary
			err      error
		)
		if s != nil {
			sessions, err = s.ListSessions(ctx, cwd)
		} else {
			sessions, err = session.ListSessions(ctx, spec, cwd)
		}
		a.Post(func() {
			a.HistoryLoading = false
			if a.closed.Load() {
				return
			}
			if err != nil {
				a.HistoryError = err.Error()
				a.HistorySessions = nil
				a.HistorySelected = -1
				return
			}
			a.HistorySessions = sessions
			if len(sessions) == 0 {
				a.HistorySelected = -1
			} else {
				a.HistorySelected = 0
			}
		})
	}()
}

func (a *App) historyDialog(c *mui.Context) {
	if !a.HistoryOpen {
		return
	}
	t := c.Theme()
	mui.Modal(c, &a.HistoryOpen, func() {
		mui.Column(c).Gap(10).Width(680).Children(func() {
			mui.Text(c, "History").FontSize(18).Bold()
			mui.Text(c, "Saved sessions from this agent. Open replays the conversation; Resume attaches without replay.").
				FontSize(11).TextColor(t.TextMuted)
			scope := 0
			if a.HistoryAll {
				scope = 1
			}
			if mui.Segmented(c, &scope, "This workspace", "All workspaces").Changed() {
				a.HistoryAll = scope == 1
				a.loadHistorySessions()
			}
			switch {
			case a.HistoryLoading:
				mui.Row(c).Gap(8).AlignItems(mui.Center).Children(func() {
					mui.Spinner(c)
					mui.Text(c, "Loading sessions…").FontSize(12).TextColor(t.TextMuted)
				})
			case a.HistoryError != "":
				mui.Text(c, a.HistoryError).FontSize(12).TextColor(t.Danger).Selectable()
			case len(a.HistorySessions) == 0:
				mui.Text(c, "No saved sessions.").FontSize(12).TextColor(t.TextMuted)
			default:
				if a.HistorySelected >= len(a.HistorySessions) {
					a.HistorySelected = 0
				}
				a.HistoryList.Key = func(i int) any { return a.HistorySessions[i].SessionID }
				a.HistoryList.Selected = &a.HistorySelected
				mui.List(c, &a.HistoryList, len(a.HistorySessions), func(i int) {
					a.historyRow(c, a.HistorySessions[i])
				}).Height(330)
			}

			mui.Row(c).Gap(8).Wrap().Justify(mui.End).Children(func() {
				selected := a.selectedHistory()
				hasSelection := selected != nil
				busy := a.SessionBusy != ""
				if mui.Button(c, "Close current chat").Disabled(a.Session == nil || a.Info.SessionID == "" || busy).Clicked() {
					a.closeActiveSession()
					a.HistoryOpen = false
				}
				if mui.Button(c, "Resume").Disabled(!hasSelection || busy).Clicked() {
					a.resumeSelected(selected)
				}
				if mui.PrimaryButton(c, "Open").Disabled(!hasSelection || busy).Clicked() {
					a.openSelected(selected)
				}
				if mui.Button(c, "Cancel").Clicked() {
					a.HistoryOpen = false
				}
			})
		})
	})
}

func (a *App) historyRow(c *mui.Context, item acp.SessionSummary) {
	t := c.Theme()
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = "(untitled)"
	}
	mui.Column(c).Gap(2).Padding(6, 10).Children(func() {
		mui.Row(c).Gap(10).AlignItems(mui.Center).Children(func() {
			mui.Text(c, truncate(title, 90)).FontSize(13).Bold()
			mui.Text(c, truncate(item.SessionID, 18)).FontSize(10).TextColor(t.TextMuted)
		})
		details := truncate(item.Cwd, 100)
		if item.UpdatedAt != "" {
			details = formatTimestamp(item.UpdatedAt) + " · " + details
		}
		if strings.TrimSpace(details) != "" {
			mui.Text(c, details).FontSize(10).TextColor(t.TextMuted)
		}
	})
}

func (a *App) selectedHistory() *acp.SessionSummary {
	if a.HistorySelected < 0 || a.HistorySelected >= len(a.HistorySessions) {
		return nil
	}
	return &a.HistorySessions[a.HistorySelected]
}

func (a *App) openSelected(item *acp.SessionSummary) {
	if item == nil {
		return
	}
	a.HistoryOpen = false
	if a.Session != nil {
		a.loadHistory(item.SessionID, item.Title, item.Cwd)
		return
	}
	if filepath.IsAbs(item.Cwd) {
		a.Workspace = item.Cwd
	}
	a.pendingSessionID = item.SessionID
	a.pendingSessionAction = "load"
	a.pendingSessionTitle = item.Title
	a.Connect()
}

func (a *App) resumeSelected(item *acp.SessionSummary) {
	if item == nil {
		return
	}
	a.HistoryOpen = false
	if a.Session != nil {
		a.resumeHistory(item.SessionID, item.Title, item.Cwd)
		return
	}
	if filepath.IsAbs(item.Cwd) {
		a.Workspace = item.Cwd
	}
	a.pendingSessionID = item.SessionID
	a.pendingSessionAction = "resume"
	a.pendingSessionTitle = item.Title
	a.Connect()
}

func formatTimestamp(value string) string {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Local().Format("2006-01-02 15:04")
		}
	}
	return value
}
