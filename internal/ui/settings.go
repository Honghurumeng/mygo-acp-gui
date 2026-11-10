package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	mui "github.com/egoist/mygo/ui"
)

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
	a.SettingsOpen = true
}

func (a *App) settingsDialog(c *mui.Context) {
	if !a.SettingsOpen {
		return
	}
	t := c.Theme()
	mui.Modal(c, &a.SettingsOpen, func() {
		mui.Column(c).Gap(10).Width(620).Children(func() {
			mui.Text(c, "MCP servers").FontSize(18).Bold()
			mui.Text(c, "Passed to session/new, load and resume. Applies to the next chat you create or open.").
				FontSize(11).TextColor(t.TextMuted)
			mui.TextArea(c, &a.MCPText).
				Label("JSON array").
				Lines(8, 20).
				Placeholder(mcpExample)
			mui.Text(c, "stdio: name + command + args + env; HTTP: type \"http\" + name + url + headers.").
				FontSize(10).TextColor(t.TextMuted)
			if a.MCPError != "" {
				mui.Text(c, a.MCPError).FontSize(12).TextColor(t.Danger).Selectable()
			}
			mui.Row(c).Gap(8).Wrap().Justify(mui.End).Children(func() {
				if mui.Button(c, "Insert example").Clicked() {
					a.MCPText = mcpExample
					a.MCPError = ""
				}
				if mui.Button(c, "Cancel").Clicked() {
					a.SettingsOpen = false
				}
				if mui.PrimaryButton(c, "Apply").Clicked() {
					servers, err := parseMCPServers(a.MCPText)
					if err != nil {
						a.MCPError = err.Error()
					} else {
						a.MCPServers = servers
						a.MCPText = prettyJSON(servers)
						a.MCPError = ""
						a.SettingsOpen = false
					}
				}
			})
		})
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
