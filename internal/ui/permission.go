package ui

import (
	"context"
	"strings"
	"sync"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/session"
)

type permissionPrompt struct {
	Chat    *Chat
	Request session.PermissionRequest
	result  chan acp.RequestPermissionResult
	once    sync.Once
}

func (p *permissionPrompt) respond(result acp.RequestPermissionResult) {
	p.once.Do(func() {
		p.result <- result
	})
}

// askPermission blocks the ACP request goroutine until the user answers in
// the dialog or the session closes. Prompts from background chats are shown
// too: the chat's own process is blocked either way.
func (a *App) askPermission(chat *Chat, ctx context.Context, req session.PermissionRequest) (acp.RequestPermissionResult, error) {
	p := &permissionPrompt{
		Chat:    chat,
		Request: req,
		result:  make(chan acp.RequestPermissionResult, 1),
	}
	a.Post(func() {
		if a.closed.Load() || chat == nil || chat.Closed || chat.Session != req.Session ||
			(req.SessionID != "" && chat.Info.SessionID != "" && req.SessionID != chat.Info.SessionID) {
			p.respond(acp.CancelledPermission())
			return
		}
		a.enqueuePermission(p)
	})

	select {
	case result := <-p.result:
		a.Post(func() {
			a.removePermission(p)
		})
		return result, nil
	case <-ctx.Done():
		p.respond(acp.CancelledPermission())
		a.Post(func() {
			a.removePermission(p)
		})
		return acp.CancelledPermission(), nil
	}
}

func (a *App) enqueuePermission(p *permissionPrompt) {
	if p == nil {
		return
	}
	a.permissionQueue = append(a.permissionQueue, p)
	if p.Chat != nil {
		p.Chat.Attention = true
	}
	if a.Permission == nil {
		a.showNextPermission()
	}
}

// removePermission takes a prompt out of the queue after its request ended,
// as when the session closes while the dialog is open.
func (a *App) removePermission(p *permissionPrompt) {
	if p == nil {
		return
	}
	a.permissionQueue = removePrompt(a.permissionQueue, p)
	if a.Permission == p {
		a.Permission = nil
		a.PermissionOpen = false
	}
	a.refreshAttention(p.Chat)
	if a.Permission == nil {
		a.showNextPermission()
	}
}

func (a *App) showNextPermission() {
	for len(a.permissionQueue) > 0 {
		p := a.permissionQueue[0]
		a.permissionQueue = a.permissionQueue[1:]
		if p.Chat == nil || p.Chat.Closed || p.Chat.Session != p.Request.Session {
			p.respond(acp.CancelledPermission())
			a.refreshAttention(p.Chat)
			continue
		}
		a.Permission = p
		a.PermissionOpen = true
		return
	}
	a.Permission = nil
	a.PermissionOpen = false
}

func (a *App) finishPermission(p *permissionPrompt, result acp.RequestPermissionResult) {
	if p == nil {
		return
	}
	p.respond(result)
	a.removePermission(p)
}

func removePrompt(prompts []*permissionPrompt, target *permissionPrompt) []*permissionPrompt {
	out := prompts[:0]
	for _, p := range prompts {
		if p != target {
			out = append(out, p)
		}
	}
	return out
}

// cancelPermissionsFor settles every prompt owned by chat, for example when
// the chat is closed or starts a new conversation.
func (a *App) cancelPermissionsFor(chat *Chat) {
	if chat == nil {
		return
	}
	var pending []*permissionPrompt
	kept := a.permissionQueue[:0]
	for _, p := range a.permissionQueue {
		if p.Chat == chat {
			pending = append(pending, p)
		} else {
			kept = append(kept, p)
		}
	}
	a.permissionQueue = kept
	if a.Permission != nil && a.Permission.Chat == chat {
		pending = append(pending, a.Permission)
		a.Permission = nil
		a.PermissionOpen = false
	}
	for _, p := range pending {
		p.respond(acp.CancelledPermission())
	}
	a.refreshAttention(chat)
	if a.Permission == nil {
		a.showNextPermission()
	}
}

func (a *App) refreshAttention(chat *Chat) {
	if chat == nil {
		return
	}
	for _, p := range a.permissionQueue {
		if p.Chat == chat {
			chat.Attention = true
			return
		}
	}
	if a.Permission != nil && a.Permission.Chat == chat {
		chat.Attention = true
		return
	}
	chat.Attention = false
}

func (a *App) permissionDialog(c *mui.Context) {
	if !a.PermissionOpen || a.Permission == nil {
		return
	}
	p := a.Permission
	req := p.Request
	t := c.Theme()

	dialog := mui.Modal(c, &a.PermissionOpen, func() {
		mui.Column(c).Gap(10).Width(440).Children(func() {
			mui.Text(c, "Permission required").FontSize(18).Bold()
			if p.Chat != nil {
				mui.Text(c, "Chat: "+p.Chat.name()).FontSize(11).TextColor(t.TextMuted)
			}
			title := req.ToolCall.Title
			if title == "" {
				title = "The agent wants to use a tool."
			}
			mui.Text(c, title).FontSize(14)
			if req.ToolCall.Kind != "" || req.ToolCall.Status != "" {
				mui.Text(c, strings.TrimSpace(req.ToolCall.Kind+" · "+req.ToolCall.Status)).
					FontSize(11).
					TextColor(t.TextMuted)
			}
			if detail := compactJSON(req.ToolCall.RawInput); detail != "" {
				mui.Text(c, detail).
					FontSize(11).
					TextColor(t.TextMuted).
					Selectable()
			}
			mui.Text(c, "Choose what the agent may do.").FontSize(12).TextColor(t.TextMuted)

			mui.Row(c).Gap(8).Wrap().Justify(mui.End).Children(func() {
				for _, option := range req.Options {
					label := option.Name
					if label == "" {
						label = option.OptionID
					}
					var button mui.Element
					if strings.Contains(option.Kind, "allow") {
						button = mui.PrimaryButton(c, label)
					} else {
						button = mui.Button(c, label)
					}
					if button.Clicked() {
						a.finishPermission(p, acp.SelectedPermission(option.OptionID))
						return
					}
				}
				if mui.Button(c, "Cancel").Clicked() {
					a.finishPermission(p, acp.CancelledPermission())
				}
			})
		})
	})
	if dialog.Dismissed() {
		a.finishPermission(p, acp.CancelledPermission())
	}
}
