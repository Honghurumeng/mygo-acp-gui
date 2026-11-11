package ui

import (
	"path/filepath"
	"strings"

	mui "github.com/egoist/mygo/ui"
)

const (
	sidebarWidth          = 292
	workspacePreviewLimit = 5
)

// syncWorkspaces makes sure the workspace list covers the current draft and
// every open conversation.
func (a *App) syncWorkspaces() {
	if a.WorkspaceCollapsed == nil {
		a.WorkspaceCollapsed = make(map[string]bool)
	}
	if a.WorkspaceExpanded == nil {
		a.WorkspaceExpanded = make(map[string]bool)
	}
	a.ensureWorkspace(a.Workspace)
	for _, chat := range a.chatsSnapshot() {
		a.ensureWorkspace(chat.Workspace)
	}
}

// ensureWorkspace appends an absolute path to the sidebar list once.
func (a *App) ensureWorkspace(path string) {
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) {
		return
	}
	path = filepath.Clean(path)
	for _, existing := range a.Workspaces {
		if existing == path {
			return
		}
	}
	a.Workspaces = append(a.Workspaces, path)
}

func sameWorkspace(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" {
		return a == b
	}
	if filepath.IsAbs(a) && filepath.IsAbs(b) {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return a == b
}

func (a *App) chatsInWorkspace(workspace string) []*Chat {
	var chats []*Chat
	for _, chat := range a.Chats {
		if sameWorkspace(chat.Workspace, workspace) {
			chats = append(chats, chat)
		}
	}
	return chats
}

func (a *App) sidebar(c *mui.Context) {
	t := c.Theme()
	mui.Column(c).Width(sidebarWidth).FillHeight().
		Background(t.Surface).
		BorderWidth(0, 1, 0, 0).BorderColor(t.Border).
		Children(func() {
			mui.Row(c).Gap(8).AlignItems(mui.Center).Padding(14, 14, 8, 14).Children(func() {
				mui.Text(c, "ACP GUI").FontSize(16).Bold().Grow(1)
				mui.Text(c, "v"+a.Version).FontSize(10).TextColor(t.TextMuted)
			})

			mui.Column(c).Gap(8).Padding(0, 12, 10, 12).Grow(1).Children(func() {
				a.newConversationButton(c)
				mui.Row(c).Gap(6).AlignItems(mui.Center).Children(func() {
					mui.Text(c, "Workspaces").FontSize(10).Bold().TextColor(t.TextMuted).Grow(1)
					if mui.Button(c, "＋").Label("Add workspace").FontSize(13).Clicked() {
						a.addWorkspace()
					}
				})
				mui.Scroll(c).Grow(1).Gap(2).Children(func() {
					for _, workspace := range a.Workspaces {
						a.workspaceGroup(c, workspace)
					}
				})
			})
			a.sidebarStatus(c)
		})
}

func (a *App) newConversationButton(c *mui.Context) {
	t := c.Theme()
	button := mui.ButtonBase(c).FillWidth().Height(38).Gap(6).Center().
		Radius(10).Background(t.Accent).TextColor(t.AccentText).
		Label("New conversation")
	button.Children(func() {
		mui.Text(c, "＋  New conversation").FontSize(12).Bold()
	})
	if button.Clicked() {
		a.newChatTabIn(a.defaultWorkspace())
	}
}

func (a *App) workspaceGroup(c *mui.Context, workspace string) {
	t := c.Theme()
	collapsed := a.WorkspaceCollapsed[workspace]
	label := filepath.Base(workspace)
	if label == "" || label == "." || label == string(filepath.Separator) {
		label = workspace
	}
	chats := a.chatsInWorkspace(workspace)

	mui.Row(c).Gap(2).AlignItems(mui.Center).Children(func() {
		head := mui.ButtonBase(c).Grow(1).Gap(5).Padding(4, 8).Radius(8).
			Background(t.Background)
		head.Children(func() {
			arrow := "▸"
			if !collapsed {
				arrow = "▾"
			}
			mui.Text(c, arrow).FontSize(10).TextColor(t.TextMuted)
			mui.Text(c, label).FontSize(11).Bold().Grow(1).SingleLine()
			if len(chats) > 0 {
				mui.Text(c, formatCount(len(chats))).FontSize(9).TextColor(t.TextMuted)
			}
		})
		head.Tooltip(workspace)
		if head.Clicked() {
			a.WorkspaceCollapsed[workspace] = !collapsed
		}
		plus := mui.Button(c, "＋").Label("New conversation in " + label).FontSize(12)
		if plus.Clicked() {
			a.newChatTabIn(workspace)
		}
	})

	if collapsed {
		return
	}

	chats = a.chatsInWorkspace(workspace)
	if len(chats) == 0 {
		mui.Text(c, "No conversations yet").FontSize(9).TextColor(t.TextMuted).Padding(2, 16)
		return
	}

	expanded := a.WorkspaceExpanded[workspace]
	limit := len(chats)
	if !expanded && limit > workspacePreviewLimit {
		limit = workspacePreviewLimit
	}
	mui.Column(c).Gap(1).Padding(0, 0, 0, 12).Children(func() {
		for _, chat := range chats[:limit] {
			a.conversationRow(c, chat)
		}
		if len(chats) > workspacePreviewLimit {
			if expanded {
				if mui.Button(c, "Show less").Label("Show less in " + label).FontSize(9).Clicked() {
					a.WorkspaceExpanded[workspace] = false
				}
			} else {
				more := len(chats) - workspacePreviewLimit
				if mui.Button(c, "Show "+formatCount(more)+" more").Label("Show more in " + label).FontSize(9).Clicked() {
					a.WorkspaceExpanded[workspace] = true
				}
			}
		}
	})
}

func (a *App) conversationRow(c *mui.Context, chat *Chat) {
	t := c.Theme()
	active := chat == a.activeChat()
	background := t.Background
	if active {
		background = t.SurfaceHover
	}
	mui.Row(c).Gap(2).AlignItems(mui.Center).Children(func() {
		button := mui.ButtonBase(c).Grow(1).Gap(5).Padding(4, 10).Radius(8).Background(background)
		button.Children(func() {
			switch {
			case chat.Streaming:
				mui.Spinner(c)
			case chat.Attention:
				mui.Text(c, "!").FontSize(10).TextColor(t.Warning)
			case chat.Unread:
				mui.Text(c, "•").FontSize(10).TextColor(t.Accent)
			}
			mui.Text(c, chat.name()).FontSize(11).Grow(1).SingleLine()
		})
		if button.Clicked() {
			a.selectChat(chat)
		}

		menu := mui.ButtonBase(c).Padding(4, 6).Radius(8).Background(background).
			Label("Conversation actions for " + chat.name())
		menu.Children(func() {
			mui.Text(c, "⋯").FontSize(11).TextColor(t.TextMuted)
		})
		if menu.Clicked() {
			chat.MenuOpen = !chat.MenuOpen
		}
		mui.Popover(c, menu, &chat.MenuOpen, func() {
			mui.Column(c).Width(132).Gap(2).Children(func() {
				if mui.Button(c, "Rename").FillWidth().FontSize(11).Clicked() {
					chat.MenuOpen = false
					a.openRename(chat)
				}
				if mui.Button(c, "Delete").FillWidth().FontSize(11).TextColor(t.Danger).Clicked() {
					chat.MenuOpen = false
					a.removeChat(chat)
				}
			})
		})
	})
}

func (a *App) openRename(chat *Chat) {
	if chat == nil {
		return
	}
	a.RenameChat = chat
	a.RenameText = chat.Title
	if strings.TrimSpace(a.RenameText) == "" {
		a.RenameText = chat.name()
	}
	a.RenameOpen = true
}

func (a *App) renameDialog(c *mui.Context) {
	if !a.RenameOpen || a.RenameChat == nil {
		return
	}
	chat := a.RenameChat
	mui.Modal(c, &a.RenameOpen, func() {
		mui.Column(c).Gap(10).Width(360).Children(func() {
			mui.Text(c, "Rename conversation").FontSize(16).Bold()
			mui.TextInput(c, &a.RenameText).
				Label("Conversation name").
				Placeholder("Conversation name")
			mui.Row(c).Gap(8).Justify(mui.End).Children(func() {
				if mui.Button(c, "Cancel").Clicked() {
					a.RenameOpen = false
					a.RenameChat = nil
				}
				if mui.PrimaryButton(c, "Save").Clicked() {
					if title := strings.TrimSpace(a.RenameText); title != "" {
						chat.Title = title
					}
					a.RenameOpen = false
					a.RenameChat = nil
				}
			})
		})
	})
	if !a.RenameOpen {
		a.RenameChat = nil
	}
}

func (a *App) sidebarStatus(c *mui.Context) {
	if len(a.AgentStatuses) == 0 && a.SettingsWarning == "" {
		return
	}
	t := c.Theme()
	mui.Column(c).Gap(3).Padding(8, 12).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
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
			row := mui.Row(c).Gap(5).AlignItems(mui.Center)
			row.Children(func() {
				dot := mui.Box(c).Size(5, 5).Radius(3).Background(color)
				_ = dot
				mui.Text(c, status.Name).FontSize(9).TextColor(t.TextMuted).Grow(1).SingleLine()
				mui.Text(c, status.Status).FontSize(9).TextColor(color).SingleLine()
			})
			detail := strings.TrimSpace(status.Detail)
			if detail != "" {
				row.Tooltip(status.Name + ": " + detail)
			}
		}
		if !a.Resolving && !a.anyLiveChat() && !a.injectedAgents {
			if mui.Button(c, "Refresh agents").FontSize(9).Clicked() {
				a.retryDiscovery()
			}
		}
		if a.SettingsWarning != "" {
			mui.Text(c, "Settings: "+truncate(a.SettingsWarning, 80)).
				FontSize(9).TextColor(t.Warning).Tooltip(a.SettingsWarning)
		}
	})
}
