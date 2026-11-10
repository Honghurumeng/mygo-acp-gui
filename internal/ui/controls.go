package ui

import (
	"fmt"
	"strconv"
	"strings"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/acp"
)

func (a *App) agentStatusRow(c *mui.Context) {
	if len(a.AgentStatuses) == 0 {
		return
	}
	t := c.Theme()
	mui.Row(c).Gap(14).Wrap().AlignItems(mui.Center).Padding(0, 20, 8, 20).Children(func() {
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
			text := status.Name + ": " + status.Status
			if status.Status == AgentMissing && strings.TrimSpace(status.InstallHint) != "" {
				text += " — " + status.InstallHint
			} else if status.Status != AgentReady && strings.TrimSpace(status.Detail) != "" {
				detail := strings.Join(strings.Fields(status.Detail), " ")
				text += " — " + truncate(detail, 180)
			}
			mui.Text(c, text).FontSize(11).TextColor(color).Selectable()
		}
		if !a.Resolving && !a.anyLiveChat() && !a.injectedAgents {
			if mui.Button(c, "Refresh").FontSize(11).Clicked() {
				a.retryDiscovery()
			}
		}
	})
}

func (a *App) sessionControls(c *mui.Context, chat *Chat) {
	if chat.Session == nil {
		return
	}
	t := c.Theme()
	mui.Row(c).Gap(8).Wrap().AlignItems(mui.Center).Padding(0, 20, 8, 20).Children(func() {
		busy := chat.Connecting || chat.Streaming || chat.Busy != "" || chat.ConfigBusy != ""
		if chat.Busy != "" {
			mui.Text(c, sessionBusyLabel(chat.Busy)+"…").FontSize(11).TextColor(t.TextMuted)
		}
		if chat.Info.SessionID == "" {
			mui.Text(c, "No active session").FontSize(11).TextColor(t.TextMuted)
		}
		if mui.Button(c, "History").Disabled(busy).Clicked() {
			a.openHistory()
		}
		if mui.Button(c, "New chat").Disabled(busy).Clicked() {
			a.newSession()
		}
		if chat.Info.SessionID != "" {
			if mui.Button(c, "Close chat").Disabled(busy).Clicked() {
				a.closeActiveSession()
			}
		}
		if mui.Button(c, "MCP…").Disabled(busy).Clicked() {
			a.openMCPSettings()
		}
		mui.Spacer(c)
		if chat.Title != "" {
			mui.Text(c, truncate(chat.Title, 80)).FontSize(11).TextColor(t.TextMuted).Selectable()
		}
		if chat.Usage != nil {
			text := "context " + formatCount(chat.Usage.Used)
			if chat.Usage.Size > 0 {
				text += " / " + formatCount(chat.Usage.Size)
			}
			if chat.Usage.Cost != nil && chat.Usage.Cost.Amount > 0 {
				text += " · " + formatMoney(chat.Usage.Cost)
			}
			mui.Text(c, text).FontSize(11).TextColor(t.TextMuted)
		}
	})
}

func (a *App) configControls(c *mui.Context, chat *Chat) {
	if chat.Session == nil {
		return
	}
	if len(chat.Info.Modes) == 0 && len(chat.Info.ConfigOptions) == 0 {
		return
	}
	t := c.Theme()
	disabled := chat.Connecting || chat.Streaming || chat.Busy != "" || chat.ConfigBusy != "" || chat.Info.SessionID == ""
	mui.Row(c).Gap(10).Wrap().AlignItems(mui.Center).Padding(0, 20, 8, 20).Children(func() {
		if len(chat.Info.Modes) > 0 {
			labels := make([]string, 0, len(chat.Info.Modes))
			current := ""
			for _, mode := range chat.Info.Modes {
				label := mode.Name
				if label == "" {
					label = mode.ID
				}
				labels = append(labels, label)
				if mode.ID == chat.Info.CurrentModeID {
					current = label
				}
			}
			if current == "" && len(labels) > 0 {
				current = labels[0]
			}
			if mui.Select(c, &current, labels).
				Label("Mode").
				Disabled(disabled).
				Changed() {
				for _, mode := range chat.Info.Modes {
					label := mode.Name
					if label == "" {
						label = mode.ID
					}
					if label == current {
						a.setMode(mode.ID)
						break
					}
				}
			}
			for _, mode := range chat.Info.Modes {
				if mode.ID == chat.Info.CurrentModeID && mode.Description != "" {
					mui.Text(c, truncate(mode.Description, 120)).FontSize(10).TextColor(t.TextMuted)
					break
				}
			}
		}
		for _, option := range chat.Info.ConfigOptions {
			if len(option.Options) == 0 || (option.Type != "" && option.Type != "select") {
				continue
			}
			labels, current := configChoices(option)
			if len(labels) == 0 {
				continue
			}
			label := option.Name
			if label == "" {
				label = option.ID
			}
			if mui.Select(c, &current, labels).
				Label(label).
				Disabled(disabled).
				Changed() {
				for _, choice := range option.Options {
					choiceLabel := choice.Name
					if choiceLabel == "" {
						choiceLabel = choice.Value
					}
					if choiceLabel == current {
						a.setConfigOption(option.ID, choice.Value)
						break
					}
				}
			}
		}
		if chat.ConfigBusy != "" {
			mui.Text(c, "Updating "+chat.ConfigBusy+"…").FontSize(11).TextColor(t.TextMuted)
		}
	})
}

func configChoices(option acp.ConfigOption) ([]string, string) {
	labels := make([]string, 0, len(option.Options))
	current := ""
	for _, choice := range option.Options {
		label := choice.Name
		if label == "" {
			label = choice.Value
		}
		labels = append(labels, label)
		if choice.Value == option.CurrentValue {
			current = label
		}
	}
	if current == "" && len(labels) > 0 {
		current = labels[0]
	}
	return labels, current
}

func sessionBusyLabel(action string) string {
	switch action {
	case "connect":
		return "Connecting"
	case "new":
		return "Creating chat"
	case "load":
		return "Opening history"
	case "resume":
		return "Resuming history"
	case "close":
		return "Closing chat"
	default:
		return action
	}
}

func formatCount(n int) string {
	switch {
	case n >= 1_000_000:
		return trimFloat(float64(n)/1_000_000) + "M"
	case n >= 1_000:
		return trimFloat(float64(n)/1_000) + "k"
	default:
		return strconv.Itoa(n)
	}
}

func trimFloat(v float64) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(v, 'f', 1, 64), "0"), ".")
}

func formatMoney(cost *acp.Cost) string {
	if cost == nil {
		return ""
	}
	return fmt.Sprintf("%.4f %s", cost.Amount, cost.Currency)
}
