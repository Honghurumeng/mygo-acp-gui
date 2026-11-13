package ui

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	crycode "github.com/ELY-Open-Source/CrychicUI/code"
	cryoverlay "github.com/ELY-Open-Source/CrychicUI/overlay"
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

	// answered keeps the dismissal branch below from cancelling a request the
	// buttons already settled.
	answered := false
	cryoverlay.Modal(c, "Permission required", &a.PermissionOpen, cryoverlay.ModalOptions{}, func() {
		mui.Column(c).FillWidth().Gap(10).Children(func() {
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
				crycode.CodeBlock(c, "json", detail, crycode.CodeBlockOptions{Title: "Input"})
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
						answered = true
						a.finishPermission(p, acp.SelectedPermission(option.OptionID))
						return
					}
				}
				if quietButton(c, "Cancel").Clicked() {
					answered = true
					a.finishPermission(p, acp.CancelledPermission())
				}
			})
		})
	})
	if !answered && !a.PermissionOpen {
		a.finishPermission(p, acp.CancelledPermission())
	}
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
