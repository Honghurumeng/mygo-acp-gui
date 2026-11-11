package ui

import (
	"path/filepath"
	"strconv"
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

// ensureWorkspace appends an absolute path to the sidebar list once. Symlink
// aliases of an existing workspace are not added as a second group.
func (a *App) ensureWorkspace(path string) {
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) {
		return
	}
	path = filepath.Clean(path)
	target := canonicalWorkspace(path)
	for _, existing := range a.Workspaces {
		if existing == path || sameWorkspace(canonicalWorkspace(existing), target) {
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
				mutedText(c, "v"+a.Version, 10)
			})

			mui.Column(c).Gap(8).Padding(0, 12, 10, 12).Grow(1).Children(func() {
				a.newConversationButton(c)
				mui.Row(c).Gap(6).AlignItems(mui.Center).Children(func() {
					mui.Text(c, "Workspaces").FontSize(10).Bold().TextColor(t.TextMuted).Grow(1)
					if quietButton(c, "＋").Label("Add workspace").FontSize(13).Clicked() {
						a.addWorkspace()
					}
				})
				a.savedSessionsStatus(c)
				a.sidebarFilters(c)
				mui.Scroll(c).Grow(1).Gap(2).Children(func() {
					for _, workspace := range a.shownWorkspaces() {
						a.workspaceGroup(c, workspace)
					}
				})
			})
			a.sidebarStatus(c)
		})
}

func (a *App) newConversationButton(c *mui.Context) {
	button := primaryButton(c, "＋  New conversation").
		FillWidth().Height(38).Gap(6).
		FontSize(12).Bold().
		Label("New conversation")
	if button.Clicked() {
		a.newChatTabIn(a.defaultWorkspace())
	}
}

func (a *App) savedSessionsStatus(c *mui.Context) {
	t := c.Theme()
	switch {
	case a.SavedLoading:
		mui.Row(c).Gap(6).AlignItems(mui.Center).Children(func() {
			mui.Spinner(c)
			caption(c, "Syncing saved sessions from agent…")
		})
	case a.SavedError != "":
		mui.Text(c, truncate("Saved sessions: "+a.SavedError, 120)).
			FontSize(9).TextColor(t.Danger).Tooltip(a.SavedError)
	}
}

// sidebarFilters expose locally removed rows: sessions stay in the archive and
// workspaces stay in settings, so both need a visible restore affordance.
func (a *App) sidebarFilters(c *mui.Context) {
	hiddenSessions := a.hiddenArchivedCount()
	removedWorkspaces := a.removedWorkspaceCount()
	if hiddenSessions == 0 && !a.ShowDeleted && removedWorkspaces == 0 && !a.ShowRemovedWorkspaces {
		return
	}
	mui.Row(c).Gap(6).AlignItems(mui.Center).Children(func() {
		if hiddenSessions > 0 || a.ShowDeleted {
			label := "Show deleted (" + strconv.Itoa(hiddenSessions) + ")"
			tip := "Show locally deleted saved sessions"
			if a.ShowDeleted {
				label = "Hide deleted"
				tip = "Hide locally deleted saved sessions"
			}
			if quietButton(c, label).Label(tip).FontSize(9).Clicked() {
				a.ShowDeleted = !a.ShowDeleted
			}
		}
		if removedWorkspaces > 0 || a.ShowRemovedWorkspaces {
			label := "Show removed (" + strconv.Itoa(removedWorkspaces) + ")"
			tip := "Show locally removed workspaces"
			if a.ShowRemovedWorkspaces {
				label = "Hide removed"
				tip = "Hide locally removed workspaces"
			}
			if quietButton(c, label).Label(tip).FontSize(9).Clicked() {
				a.ShowRemovedWorkspaces = !a.ShowRemovedWorkspaces
			}
		}
	})
}

func (a *App) workspaceGroup(c *mui.Context, workspace string) {
	t := c.Theme()
	collapsed := a.WorkspaceCollapsed[workspace]
	removed := a.workspaceRemoved(workspace)
	label := filepath.Base(workspace)
	if label == "" || label == "." || label == string(filepath.Separator) {
		label = workspace
	}
	display := label
	if removed {
		display = "[removed] " + label
	}
	chats := a.chatsInWorkspace(workspace)
	saved := a.savedSessionsFor(workspace)
	total := len(chats) + len(saved)

	mui.Row(c).Gap(2).AlignItems(mui.Center).Children(func() {
		head := mui.ButtonBase(c).Grow(1).Gap(5).Padding(4, 8).Radius(8).
			Background(t.Background)
		head.Children(func() {
			arrow := "▸"
			if !collapsed {
				arrow = "▾"
			}
			mutedText(c, arrow, 10)
			title := mui.Text(c, display).FontSize(11).Bold().Grow(1).SingleLine()
			if removed {
				title.TextColor(t.TextMuted)
			}
			if total > 0 {
				mutedText(c, formatCount(total), 9)
			}
		})
		tip := workspace
		if removed {
			tip += "\nRemoved locally; restore from the ⋯ menu."
		}
		head.Tooltip(tip)
		if head.Clicked() {
			a.WorkspaceCollapsed[workspace] = !collapsed
		}

		menu := menuTrigger(c, "Workspace actions for "+label, t.Background)
		if menu.Clicked() {
			if a.WorkspaceMenuOpen == workspace {
				a.WorkspaceMenuOpen = ""
			} else {
				a.WorkspaceMenuOpen = workspace
			}
		}
		open := a.WorkspaceMenuOpen == workspace
		mui.Popover(c, menu, &open, func() {
			mui.Column(c).Width(168).Gap(2).Children(func() {
				if removed {
					if quietButton(c, "Restore workspace").FillWidth().FontSize(11).Clicked() {
						open = false
						a.restoreWorkspace(workspace)
					}
					return
				}
				if quietButton(c, "Remove workspace").FillWidth().FontSize(11).
					TextColor(t.Danger).Clicked() {
					open = false
					a.removeWorkspace(workspace)
				}
			})
		})
		if !open && a.WorkspaceMenuOpen == workspace {
			a.WorkspaceMenuOpen = ""
		}

		if !removed {
			plus := quietButton(c, "＋").Label("New conversation in " + label).FontSize(12)
			if plus.Clicked() {
				a.newChatTabIn(workspace)
			}
		}
	})

	if collapsed {
		return
	}

	chats = a.chatsInWorkspace(workspace)
	saved = a.savedSessionsFor(workspace)
	if len(chats) == 0 && len(saved) == 0 {
		caption(c, "No conversations yet").Padding(2, 16)
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
				if quietButton(c, "Show less").Label("Show less in " + label).FontSize(9).Clicked() {
					a.WorkspaceExpanded[workspace] = false
				}
			} else {
				more := len(chats) - workspacePreviewLimit
				if quietButton(c, "Show "+formatCount(more)+" more").Label("Show more in " + label).FontSize(9).Clicked() {
					a.WorkspaceExpanded[workspace] = true
				}
			}
		}
		for _, item := range saved {
			a.savedSessionRow(c, item)
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
			agentBadge(c, chat.Agent.ID, 11)
			mui.Text(c, chat.name()).FontSize(11).Grow(1).SingleLine()
		})
		if button.Clicked() {
			a.selectChat(chat)
		}

		menu := menuTrigger(c, "Conversation actions for "+chat.name(), background)
		if menu.Clicked() {
			chat.MenuOpen = !chat.MenuOpen
		}
		mui.Popover(c, menu, &chat.MenuOpen, func() {
			mui.Column(c).Width(132).Gap(2).Children(func() {
				if quietButton(c, "Rename").FillWidth().FontSize(11).Clicked() {
					chat.MenuOpen = false
					a.openRename(chat)
				}
				if quietButton(c, "Delete").FillWidth().FontSize(11).TextColor(t.Danger).Clicked() {
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
	a.RenameSaved = false
	a.RenameAgentID = ""
	a.RenameSessionID = ""
	a.RenameText = a.archivedShowName(chat.Agent.ID, chat.Info.SessionID)
	if strings.TrimSpace(a.RenameText) == "" {
		a.RenameText = chat.Title
	}
	if strings.TrimSpace(a.RenameText) == "" {
		a.RenameText = chat.name()
	}
	a.RenameOpen = true
}

func (a *App) renameDialog(c *mui.Context) {
	if !a.RenameOpen || (a.RenameChat == nil && !a.RenameSaved) {
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
				if quietButton(c, "Cancel").Clicked() {
					a.RenameOpen = false
					a.RenameChat = nil
					a.RenameSaved = false
				}
				if primaryButton(c, "Save").Clicked() {
					if title := strings.TrimSpace(a.RenameText); title != "" {
						switch {
						case a.RenameSaved:
							a.setArchivedShowName(a.RenameAgentID, a.RenameSessionID, title)
						case chat != nil:
							chat.Title = title
							if strings.TrimSpace(chat.Info.SessionID) != "" {
								a.setArchivedShowName(chat.Agent.ID, chat.Info.SessionID, title)
							}
						}
					}
					a.RenameOpen = false
					a.RenameChat = nil
					a.RenameSaved = false
				}
			})
		})
	})
	if !a.RenameOpen {
		a.RenameChat = nil
		a.RenameSaved = false
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
				mutedText(c, status.Name, 9).Grow(1).SingleLine()
				mui.Text(c, status.Status).FontSize(9).TextColor(color).SingleLine()
			})
			detail := strings.TrimSpace(status.Detail)
			if detail != "" {
				row.Tooltip(status.Name + ": " + detail)
			}
		}
		if !a.Resolving && !a.anyLiveChat() && !a.injectedAgents {
			if quietButton(c, "Refresh agents").FontSize(9).Clicked() {
				a.retryDiscovery()
			}
		}
		if a.SettingsWarning != "" {
			mui.Text(c, "Settings: "+truncate(a.SettingsWarning, 80)).
				FontSize(9).TextColor(t.Warning).Tooltip(a.SettingsWarning)
		}
	})
}
