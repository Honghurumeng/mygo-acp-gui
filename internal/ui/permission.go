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
// the dialog or the session closes.
func (a *App) askPermission(ctx context.Context, req session.PermissionRequest) (acp.RequestPermissionResult, error) {
	p := &permissionPrompt{
		Request: req,
		result:  make(chan acp.RequestPermissionResult, 1),
	}
	a.Post(func() {
		if a.closed.Load() || a.Session != req.Session ||
			(req.SessionID != "" && a.Info.SessionID != "" && req.SessionID != a.Info.SessionID) {
			p.respond(acp.CancelledPermission())
			return
		}
		if a.Permission != nil {
			a.Permission.respond(acp.CancelledPermission())
		}
		a.Permission = p
		a.PermissionOpen = true
	})

	select {
	case result := <-p.result:
		a.Post(func() {
			if a.Permission == p {
				a.Permission = nil
				a.PermissionOpen = false
			}
		})
		return result, nil
	case <-ctx.Done():
		p.respond(acp.CancelledPermission())
		a.Post(func() {
			if a.Permission == p {
				a.Permission = nil
				a.PermissionOpen = false
			}
		})
		return acp.CancelledPermission(), nil
	}
}

func (a *App) finishPermission(p *permissionPrompt, result acp.RequestPermissionResult) {
	if p == nil {
		return
	}
	p.respond(result)
	if a.Permission == p {
		a.Permission = nil
		a.PermissionOpen = false
	}
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
