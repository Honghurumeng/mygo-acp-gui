package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/reed/acp-gui/internal/acp"
)

// desktopNotification is one system notification. Keeping it apart from
// mygo.NotificationOptions lets tests capture what the app would show
// without starting the desktop backend.
type desktopNotification struct {
	ID       string
	Title    string
	Subtitle string
	Body     string
	Group    string
}

// canNotify reports whether an event in chat deserves a system notification:
// only when the user cannot already see it in the app, either because the
// window is closed or unfocused, or because another chat is on screen. The
// permission dialog and the sidebar's finished-turn marker cover the rest.
func (a *App) canNotify(chat *Chat) bool {
	if a == nil || chat == nil || chat.Closed || a.closed.Load() || chat.replaying {
		return false
	}
	if chat != a.activeChat() {
		return true
	}
	win := a.window()
	return win == nil || !win.IsFocused()
}

// notifyDesktop shows n on the desktop. A nil notify func uses MyGo, which
// needs a packaged app; with no window ever created (tests) there is no app
// to notify. Delivery runs off the UI thread because Show may wait for the
// system's permission answer.
func (a *App) notifyDesktop(n desktopNotification) {
	if n.Title == "" {
		return
	}
	if a.notify != nil {
		a.notify(n)
		return
	}
	if a.updateWin == nil {
		return
	}
	go func() {
		if a.closed.Load() || !mygo.NotificationsSupported() {
			return
		}
		_ = mygo.NewNotification(mygo.NotificationOptions{
			ID:       n.ID,
			Title:    n.Title,
			Subtitle: n.Subtitle,
			Body:     n.Body,
			Group:    n.Group,
		}).Show()
	}()
}

// notifyPermission asks for attention when a permission request arrives for
// a chat the user is not looking at. Reusing the chat's ID replaces an older
// permission notification from the same chat instead of stacking them.
func (a *App) notifyPermission(p *permissionPrompt) {
	if p == nil || !a.canNotify(p.Chat) {
		return
	}
	body := toolDescription(p.Request.ToolCall.RawInput)
	if body == "" {
		body = strings.TrimSpace(p.Request.ToolCall.Title)
	}
	if body == "" {
		body = "The agent wants to use a tool."
	}
	key := notificationChatKey(p.Chat)
	a.notifyDesktop(desktopNotification{
		ID:       "acp-gui:permission:" + key,
		Title:    "Permission needed",
		Subtitle: p.Chat.name(),
		Body:     body,
		Group:    "acp-gui:" + key,
	})
}

// notifyTurnFinished announces a completed turn outside the app. A turn the
// user cancelled themselves is not news, so it stays silent.
func (a *App) notifyTurnFinished(chat *Chat, elapsed time.Duration, stopReason string, err error) {
	if stopReason == "cancelled" || errors.Is(err, acp.ErrClosed) || !a.canNotify(chat) {
		return
	}
	title := "Task complete"
	body := notificationSummary(chat)
	switch {
	case err != nil:
		title = "Task failed"
		body = singleLine(err.Error())
	case body == "" && elapsed > 0:
		body = "Finished in " + formatElapsed(elapsed)
	case body == "":
		body = "The agent finished the turn."
	}
	body = truncate(body, 240)
	key := notificationChatKey(chat)
	a.notifyDesktop(desktopNotification{
		ID:       "acp-gui:turn:" + key,
		Title:    title,
		Subtitle: chat.name(),
		Body:     body,
		Group:    "acp-gui:" + key,
	})
}

// notificationSummary is the end of the agent's answer, flattened to one
// line: the notification preview should say what the turn produced.
func notificationSummary(chat *Chat) string {
	if chat == nil {
		return ""
	}
	for i := len(chat.Messages) - 1; i >= 0; i-- {
		message := chat.Messages[i]
		if message == nil || message.Role != RoleAssistant {
			continue
		}
		if text := strings.TrimSpace(message.Text); text != "" {
			return singleLine(text)
		}
	}
	return ""
}

// notificationChatKey identifies a conversation for grouping and replacing
// notifications. The ACP session ID makes it stable while the app runs.
func notificationChatKey(chat *Chat) string {
	if chat == nil {
		return "app"
	}
	if id := strings.TrimSpace(chat.Info.SessionID); id != "" {
		return fmt.Sprintf("%s:%s", chat.Agent.ID, id)
	}
	return fmt.Sprintf("chat-%d", chat.ID)
}
