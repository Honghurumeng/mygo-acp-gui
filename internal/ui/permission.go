package ui

import (
	"context"
	"encoding/json"
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
	a.showNextPermission()
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
	a.showNextPermission()
}

// showNextPermission shows only a prompt owned by the active chat. Requests
// from other chats stay queued, with their sidebar marker, until the user
// selects that chat. Leaving a chat hides its dialog again without answering.
func (a *App) showNextPermission() {
	active := a.activeChat()
	if active != nil && active.Closed {
		active = nil
	}
	if a.Permission != nil {
		if a.Permission.Chat == active {
			return
		}
		// Keep the visible prompt pending; switching back reveals it again.
		a.permissionQueue = append([]*permissionPrompt{a.Permission}, a.permissionQueue...)
		a.Permission = nil
		a.PermissionOpen = false
	}
	for i := 0; i < len(a.permissionQueue); {
		p := a.permissionQueue[i]
		if p.Chat == nil || p.Chat.Closed || p.Chat.Session != p.Request.Session {
			a.permissionQueue = append(a.permissionQueue[:i], a.permissionQueue[i+1:]...)
			p.respond(acp.CancelledPermission())
			a.refreshAttention(p.Chat)
			continue
		}
		if active == nil || p.Chat != active {
			i++
			continue
		}
		a.permissionQueue = append(a.permissionQueue[:i], a.permissionQueue[i+1:]...)
		a.Permission = p
		a.PermissionOpen = true
		a.refreshAttention(p.Chat)
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
	a.showNextPermission()
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

	permissionModal(c, "Permission required", func() {
		if p.Chat != nil {
			mutedText(c, "Chat: "+p.Chat.name(), 11)
		}
		title := req.ToolCall.Title
		description := toolDescription(req.ToolCall.RawInput)
		if description != "" {
			// The description says what the call does; the title often
			// repeats the raw command, so it stays a muted subtitle.
			mui.Text(c, description).FontSize(14)
			if toolTitleAddsInfo(title, description) {
				mutedText(c, title, 11)
			}
		} else {
			if title == "" {
				title = "The agent wants to use a tool."
			}
			mui.Text(c, title).FontSize(14)
		}
		if req.ToolCall.Kind != "" || req.ToolCall.Status != "" {
			mutedText(c, strings.TrimSpace(req.ToolCall.Kind+" · "+req.ToolCall.Status), 11)
		}
		if detail := compactJSON(req.ToolCall.RawInput); detail != "" {
			codeBlock(c, "json", detail, "Input")
		}
		mutedText(c, "Choose what the agent may do.", 12)

		mui.Row(c).Gap(8).Wrap().Justify(mui.End).Children(func() {
			for _, option := range req.Options {
				label := option.Name
				if label == "" {
					label = option.OptionID
				}
				var button mui.Element
				switch {
				case strings.Contains(option.Kind, "allow"):
					button = primaryButton(c, label)
				case strings.Contains(option.Kind, "reject"):
					button = dangerButton(c, label)
				default:
					button = quietButton(c, label)
				}
				if button.Clicked() {
					a.finishPermission(p, acp.SelectedPermission(option.OptionID))
					return
				}
			}
			if quietButton(c, "Cancel").Clicked() {
				a.finishPermission(p, acp.CancelledPermission())
			}
		})
	})
}

// permissionModal shows content over a dim until one of its buttons settles
// the request. MyGo's DialogBase marks itself dismissed on a backdrop click
// or Escape; open is local to one frame here, so those presses are swallowed
// instead of rejecting the ACP request.
func permissionModal(c *mui.Context, title string, content func()) {
	t := c.Theme()
	open := true
	mui.DialogBase(c, &open, func(backdrop, panel mui.Element) {
		arrive(backdrop.Background(shade(0.4)), mui.Pose{Fade: 1})
		panel.FillWidth().MaxWidth(t.Space(110)).Padding(t.Space(5)).Radius(t.Radius*1.5).
			Background(t.Background).Shadow(0, 12, 32, 0, shade(0.24))
		arrive(panel, mui.Pose{Scale: 0.95, Fade: 1})
		mui.Column(c).FillWidth().Gap(t.Space(3)).Children(func() {
			mui.Text(c, title).Bold().FontSize(t.Rem(1.1))
			content()
		})
	})
}

// toolDescription returns the agent's short explanation of a tool call.
// Command Code puts it in rawInput.description for shell-like tools.
func toolDescription(raw json.RawMessage) string {
	var input struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return ""
	}
	return strings.TrimSpace(input.Description)
}

// toolTitleAddsInfo reports whether the title still carries information once
// the description is the headline. Titles like "Agent: <description>" only
// repeat it, while "Shell: <command>" stays useful.
func toolTitleAddsInfo(title, description string) bool {
	if title == "" || title == description {
		return false
	}
	if _, detail, ok := strings.Cut(title, ": "); ok {
		detail = strings.TrimSuffix(detail, "...")
		if detail != "" && strings.HasPrefix(description, detail) {
			return false
		}
	}
	return true
}
