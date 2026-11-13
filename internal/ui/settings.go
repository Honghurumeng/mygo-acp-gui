package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	mui "github.com/egoist/mygo/ui"
)

const settingsSectionNotifications = "notifications"

const mcpExample = `[
  {
    "name": "filesystem",
    "command": "npx",
    "args": ["-y", "@modelcontextprotocol/server-filesystem", "."]
  },
  {
    "type": "http",
    "name": "remote",
    "url": "https://example.com/mcp",
    "headers": {"Authorization": "Bearer …"}
  }
]`

func (a *App) openMCPSettings() {
	a.MCPText = prettyJSON(a.MCPServers)
	a.MCPError = ""
	a.MCPOpen = true
}

func (a *App) mcpDialog(c *mui.Context) {
	if !a.MCPOpen {
		return
	}
	t := c.Theme()
	mui.Modal(c, &a.MCPOpen, func() {
		mui.Column(c).Gap(10).Width(620).Children(func() {
			mui.Text(c, "MCP servers").FontSize(18).Bold()
			mutedText(c, "Applies to the active chat and new tabs created from it. Each tab keeps its own list.", 11)
			mui.TextArea(c, &a.MCPText).
				Label("JSON array").
				Lines(8, 20).
				Placeholder(mcpExample)
			mutedText(c, "stdio: name + command + args + env; HTTP: type \"http\" + name + url + headers.", 10)
			if a.MCPError != "" {
				mui.Text(c, a.MCPError).FontSize(12).TextColor(t.Danger).Selectable()
			}
			mui.Row(c).Gap(8).Wrap().Justify(mui.End).Children(func() {
				if quietButton(c, "Insert example").Clicked() {
					a.MCPText = mcpExample
					a.MCPError = ""
				}
				if quietButton(c, "Cancel").Clicked() {
					a.MCPOpen = false
				}
				if primaryButton(c, "Apply").Clicked() {
					servers, err := parseMCPServers(a.MCPText)
					if err != nil {
						a.MCPError = err.Error()
					} else {
						a.MCPServers = servers
						if chat := a.activeChat(); chat != nil {
							chat.MCPServers = append([]any(nil), servers...)
							if chat.Session != nil {
								chat.Session.SetMCPServers(servers)
							}
						}
						a.MCPText = prettyJSON(servers)
						a.MCPError = ""
						a.MCPOpen = false
						a.persistSettings()
					}
				}
			})
		})
	})
}

func (a *App) openSettings() {
	if a.SettingsSection == "" {
		a.SettingsSection = settingsSectionNotifications
	}
	a.NotificationTestNotice = ""
	a.SettingsOpen = true
}

// settingsDialog is the general settings panel: a category list on the left
// and the selected category's settings on the right. MCP stays in its own
// modal because it edits the active chat's server list.
func (a *App) settingsDialog(c *mui.Context) {
	if !a.SettingsOpen {
		return
	}
	t := c.Theme()
	mui.Modal(c, &a.SettingsOpen, func() {
		mui.Column(c).Gap(12).Width(720).Children(func() {
			mui.Text(c, "Settings").FontSize(18).Bold()
			mui.Row(c).Height(390).Clip().Radius(10).Border(1, t.Border).Children(func() {
				mui.Column(c).Width(176).FillHeight().Gap(2).Padding(8).
					Background(t.Surface).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).
					Children(func() {
						a.settingsCategory(c, settingsSectionNotifications, "Notifications")
					})
				mui.Column(c).Grow(1).FillHeight().Padding(18).Children(func() {
					switch a.SettingsSection {
					case settingsSectionNotifications:
						a.notificationSettings(c)
					}
				})
			})
			mui.Row(c).Gap(8).Justify(mui.End).Children(func() {
				if primaryButton(c, "Close").Clicked() {
					a.SettingsOpen = false
				}
			})
		})
	})
}

func (a *App) settingsCategory(c *mui.Context, id, label string) {
	t := c.Theme()
	item := mui.ButtonBase(c).FillWidth().Padding(7, 10).Radius(8).Label(label)
	switch {
	case a.SettingsSection == id:
		item.Background(t.Accent).TextColor(t.AccentText)
	case item.Hovered():
		item.Background(t.SurfaceHover)
	}
	item.Children(func() {
		mui.Text(c, label).FontSize(11).SingleLine()
	})
	if item.Clicked() && a.SettingsSection != id {
		a.SettingsSection = id
		a.NotificationTestNotice = ""
	}
}

func (a *App) notificationSettings(c *mui.Context) {
	t := c.Theme()
	mui.Column(c).Gap(12).Children(func() {
		mui.Text(c, "Notifications").FontSize(15).Bold()
		mutedText(c, "Choose when ACP GUI asks for attention outside the window.", 11)
		mui.Row(c).Gap(16).AlignItems(mui.Center).Padding(10, 12).Radius(8).
			Background(t.Surface).Children(func() {
			mui.Column(c).Gap(3).Grow(1).Children(func() {
				mui.Text(c, "System notifications").FontSize(12)
				mutedText(c, "Notify on permission requests and finished turns while the window is closed, unfocused, or showing another conversation.", 10)
			})
			sw := mui.Switch(c, &a.NotificationsEnabled).Label("Toggle system notifications")
			if sw.Changed() {
				a.NotificationTestNotice = ""
				a.persistSettings()
			}
		})
		mui.Row(c).Gap(16).AlignItems(mui.Center).Padding(10, 12).Radius(8).
			Background(t.Surface).Children(func() {
			mui.Column(c).Gap(3).Grow(1).Children(func() {
				mui.Text(c, "Dock badge").FontSize(12)
				mutedText(c, "Show the number of conversations waiting for a permission answer or that finished while you were elsewhere. On the Dock (macOS) or launcher (Linux).", 10)
			})
			sw := mui.Switch(c, &a.DockBadgeEnabled).Label("Toggle Dock badge")
			if sw.Changed() {
				a.NotificationTestNotice = ""
				a.persistSettings()
				a.updateDockBadge()
			}
		})
		mui.Row(c).Gap(10).AlignItems(mui.Center).Children(func() {
			test := quietButton(c, "Send test notification").FontSize(11).Border(1, t.Border)
			test.Disabled(!a.NotificationsEnabled)
			if test.Clicked() {
				a.sendTestNotification()
			}
			if !a.NotificationsEnabled {
				caption(c, "Turn on system notifications to test.")
			}
		})
		if notice := strings.TrimSpace(a.NotificationTestNotice); notice != "" {
			mutedText(c, notice, 10).Selectable()
		}
	})
}

func parseMCPServers(text string) ([]any, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return []any{}, nil
	}
	var servers []any
	if err := json.Unmarshal([]byte(text), &servers); err != nil {
		return nil, fmt.Errorf("MCP servers must be a JSON array: %w", err)
	}
	for i, server := range servers {
		obj, ok := server.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("MCP server %d must be an object", i+1)
		}
		name, _ := obj["name"].(string)
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("MCP server %d needs a name", i+1)
		}
		kind, _ := obj["type"].(string)
		command, _ := obj["command"].(string)
		url, _ := obj["url"].(string)
		if (kind == "http" || kind == "sse") && strings.TrimSpace(url) == "" {
			return nil, fmt.Errorf("MCP server %q needs a url", name)
		}
		if kind != "http" && kind != "sse" && strings.TrimSpace(command) == "" {
			return nil, fmt.Errorf("MCP server %q needs a command", name)
		}
	}
	return servers, nil
}

func prettyJSON(value any) string {
	if value == nil {
		return "[]"
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "[]"
	}
	return string(data)
}
