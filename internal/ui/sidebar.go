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
				mui.Text(c, "ACP GUI").FontSize(a.workspaceFont(16)).Bold().Grow(1)
				mutedText(c, "v"+a.Version, a.workspaceFont(10))
			})

			mui.Column(c).Gap(8).Padding(0, 0, 10, 0).Grow(1).Children(func() {
				mui.Row(c).Gap(6).AlignItems(mui.Center).Padding(0, 14, 0, 12).Children(func() {
					mui.Text(c, "Workspaces").FontSize(a.workspaceFont(10)).Bold().TextColor(t.TextMuted).Grow(1)
					if quietButton(c, "＋").Label("Add workspace").FontSize(a.workspaceFont(13)).Clicked() {
						a.addWorkspace()
					}
				})
				mui.Column(c).Gap(8).Padding(0, 12).Children(func() {
					a.savedSessionsStatus(c)
					a.sidebarFilters(c)
				})
				// The scroll view runs to the sidebar's right edge so its overlay
				// scrollbar sits near the border, not inside the content gutter.
				// Rows keep a matching inset that also clears the scrollbar.
				mui.Scroll(c).Grow(1).Children(func() {
					mui.Column(c).Gap(2).Padding(0, 14, 0, 12).Children(func() {
						for _, workspace := range a.shownWorkspaces() {
							a.workspaceGroup(c, workspace)
						}
					})
				})
			})
			a.sidebarStatus(c)
		})
}

func (a *App) savedSessionsStatus(c *mui.Context) {
	t := c.Theme()
	switch {
	case a.SavedLoading:
		mui.Row(c).Gap(6).AlignItems(mui.Center).Children(func() {
			mui.Spinner(c)
			a.workspaceCaption(c, "Syncing saved sessions from agent…")
		})
	case a.SavedError != "":
		mui.Text(c, truncate("Saved sessions: "+a.SavedError, 120)).
			FontSize(a.workspaceFont(9)).TextColor(t.Danger).Tooltip(a.SavedError)
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
			if quietButton(c, label).Label(tip).FontSize(a.workspaceFont(9)).Clicked() {
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
			if quietButton(c, label).Label(tip).FontSize(a.workspaceFont(9)).Clicked() {
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
			mutedText(c, arrow, a.workspaceFont(10))
			title := mui.Text(c, display).FontSize(a.workspaceFont(11)).Bold().Grow(1).SingleLine()
			if removed {
				title.TextColor(t.TextMuted)
			}
			if total > 0 {
				mutedText(c, formatCount(total), a.workspaceFont(9))
			}
		})
		if head.Clicked() {
			a.toggleWorkspaceCollapsed(workspace)
		}

		menu := menuTrigger(c, "Workspace actions for "+label, t.Background, a.workspaceFont(11))
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
					if quietButton(c, "Restore workspace").FillWidth().FontSize(a.workspaceFont(11)).Clicked() {
						open = false
						a.restoreWorkspace(workspace)
					}
					return
				}
				if quietButton(c, "Remove workspace").FillWidth().FontSize(a.workspaceFont(11)).
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
			plus := quietButton(c, "＋").Label("New conversation in " + label).FontSize(a.workspaceFont(12))
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
		a.workspaceCaption(c, "No conversations yet").Padding(2, 16)
		return
	}

	expanded := a.WorkspaceExpanded[workspace]
	total = len(chats) + len(saved)
	limit := total
	if !expanded && limit > workspacePreviewLimit {
		limit = workspacePreviewLimit
	}
	mui.Column(c).Gap(1).Padding(0, 0, 0, 12).Children(func() {
		for i := 0; i < limit; i++ {
			// Open conversations come first, then the workspace's saved
			// history; the preview limit applies to the combined list.
			if i < len(chats) {
				a.conversationRow(c, chats[i])
			} else {
				a.savedSessionRow(c, saved[i-len(chats)])
			}
		}
		if total > workspacePreviewLimit {
			if expanded {
				if quietButton(c, "Show less").Label("Show less in " + label).FontSize(a.workspaceFont(9)).Clicked() {
					a.WorkspaceExpanded[workspace] = false
				}
			} else {
				more := total - workspacePreviewLimit
				if quietButton(c, "Show "+formatCount(more)+" more").Label("Show more in " + label).FontSize(a.workspaceFont(9)).Clicked() {
					a.WorkspaceExpanded[workspace] = true
				}
			}
		}
	})
}

func (a *App) conversationRow(c *mui.Context, chat *Chat) {
	t := c.Theme()
	state := chatRowStateOf(chat, chat == a.activeChat())
	background := chatRowBackground(t, state)
	mui.Row(c).Gap(2).AlignItems(mui.Center).Children(func() {
		button := mui.ButtonBase(c).Grow(1).Gap(5).Padding(4, 10).Radius(chatRowRadius).
			Background(background).
			Draw(func(p *mui.Painter, r mui.Rect) { chatRowOverlay(p, r, state) })
		button.Children(func() {
			agentBadge(c, chat.Agent.ID, 11)
			mui.Text(c, chat.name()).FontSize(a.workspaceFont(11)).Grow(1).MinWidth(0).SingleLine()
		})
		if button.Clicked() {
			a.selectChat(chat)
		}

		menu := menuTrigger(c, "Conversation actions for "+chat.name(), background, a.workspaceFont(11))
		if menu.Clicked() {
			chat.MenuOpen = !chat.MenuOpen
		}
		mui.Popover(c, menu, &chat.MenuOpen, func() {
			mui.Column(c).Width(132).Gap(2).Children(func() {
				if quietButton(c, "Rename").FillWidth().FontSize(a.workspaceFont(11)).Clicked() {
					chat.MenuOpen = false
					a.openRename(chat)
				}
				if quietButton(c, "Delete").FillWidth().FontSize(a.workspaceFont(11)).TextColor(t.Danger).Clicked() {
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
			mui.Text(c, "Rename conversation").FontSize(a.workspaceFont(16)).Bold()
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
							if strings.TrimSpace(chat.TaskTitle) != "" {
								chat.TaskTitle = title
							}
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
	t := c.Theme()
	mui.Column(c).Gap(3).Padding(8, 12).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		mui.Row(c).Gap(8).AlignItems(mui.Start).Children(func() {
			mui.Column(c).Grow(1).Gap(3).Children(func() {
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
						label := status.Name
						if version := agentVersionLabel(status.Version); version != "" {
							label += " " + version
						}
						mutedText(c, label, a.workspaceFont(9)).Grow(1).SingleLine()
						mui.Text(c, status.Status).FontSize(a.workspaceFont(9)).TextColor(color).SingleLine()
					})
					detail := strings.TrimSpace(status.Detail)
					if detail != "" {
						row.Tooltip(status.Name + ": " + detail)
					}
				}
				if !a.Resolving && !a.anyLiveChat() && !a.injectedAgents {
					if quietButton(c, "Refresh agents").FontSize(a.workspaceFont(9)).Clicked() {
						a.retryDiscovery()
					}
				}
				if a.SettingsWarning != "" {
					mui.Text(c, "Settings: "+truncate(a.SettingsWarning, 80)).
						FontSize(a.workspaceFont(9)).TextColor(t.Warning).Tooltip(a.SettingsWarning)
				}
			})

			settings := buttonBase(c, "Settings").Padding(5, 7).Radius(8).Shrink(0)
			switch {
			case settings.Pressed():
				settings.Background(t.SurfacePressed)
			case settings.Hovered():
				settings.Background(t.SurfaceHover)
			default:
				settings.Background(t.Background)
			}
			settings.Label("Settings").Tooltip("Settings")
			settings.Children(func() {
				mui.Icon(c, settingsIcon).Size(14, 14).TextColor(t.TextMuted)
			})
			if settings.Clicked() {
				a.openSettings()
			}
		})
	})
}

// agentVersionLabel prefixes a CLI version for the sidebar without doubling
// an existing leading v.
func agentVersionLabel(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	if strings.HasPrefix(version, "v") || strings.HasPrefix(version, "V") {
		return version
	}
	return "v" + version
}
