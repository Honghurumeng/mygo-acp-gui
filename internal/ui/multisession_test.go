package ui

import (
	"strings"
	"testing"

	mui "github.com/egoist/mygo/ui"
)

// newTab opens a draft chat from the tab strip.
func newTab(t *testing.T, app *App, tt *mui.Tester) *Chat {
	t.Helper()
	tt.Frame()
	if err := tt.Click("New conversation"); err != nil {
		t.Fatal(err)
	}
	chat := app.activeChat()
	if chat == nil {
		t.Fatal("new tab did not create a chat")
	}
	return chat
}

func connectTab(t *testing.T, app *App, tt *mui.Tester) *Chat {
	t.Helper()
	tt.Frame()
	if err := tt.Click("Connect"); err != nil {
		t.Fatal(err)
	}
	chat := app.activeChat()
	waitFor(t, app, tt, func() bool { return chat != nil && chat.Connected })
	return chat
}

func sendTo(t *testing.T, app *App, tt *mui.Tester, chat *Chat, text string) {
	t.Helper()
	app.selectChat(chat)
	chat.Input = text
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
}

func chatAssistantHas(chat *Chat, want string) bool {
	for i := len(chat.Messages) - 1; i >= 0; i-- {
		m := chat.Messages[i]
		if m.Role == RoleAssistant {
			return strings.Contains(m.Text, want)
		}
	}
	return false
}

func TestMultipleChatsRunInParallel(t *testing.T) {
	app, tt := newFakeApp(t)
	first := connectTab(t, app, tt)

	first.Input = "first chat"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return !first.Streaming && chatAssistantHas(first, "收到：first chat") })

	second := newTab(t, app, tt)
	if second.Agent.ID != first.Agent.ID || second.Workspace != first.Workspace {
		t.Fatalf("new tab did not inherit agent/workspace: %+v", second)
	}
	second = connectTab(t, app, tt)
	if second == first || first.Session == nil || second.Session == nil {
		t.Fatal("chats do not have independent sessions")
	}

	second.Input = "second chat"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return !second.Streaming && chatAssistantHas(second, "收到：second chat") })
	if !chatAssistantHas(first, "收到：first chat") || chatAssistantHas(first, "second chat") {
		t.Fatalf("first chat was changed by the second: %+v", first.Messages)
	}
	if chatAssistantHas(second, "first chat") {
		t.Fatalf("second chat saw the first conversation: %+v", second.Messages)
	}

	// Switching through the tab strip keeps each transcript.
	first.Title = "First"
	second.Title = "Second"
	tt.Frame()
	if err := tt.Click("First"); err != nil {
		t.Fatal(err)
	}
	if app.activeChat() != first {
		t.Fatal("clicking the tab did not select the first chat")
	}
	if !tt.HasText("first chat") {
		t.Errorf("first transcript not rendered; texts: %q", tt.Texts())
	}
}

func TestBackgroundChatKeepsStreaming(t *testing.T) {
	app, tt := newFakeApp(t)
	first := connectTab(t, app, tt)
	second := newTab(t, app, tt)
	second = connectTab(t, app, tt)

	sendTo(t, app, tt, first, "/slow background")
	waitFor(t, app, tt, func() bool { return first.Streaming })

	// Switch away while the first chat is still streaming.
	app.selectChat(second)
	tt.Frame()
	waitFor(t, app, tt, func() bool { return !first.Streaming && chatAssistantHas(first, "mcp=") })
	if !first.Unread {
		t.Error("background chat did not mark itself unread")
	}
	if second.Streaming {
		t.Fatal("second chat unexpectedly streams")
	}

	app.selectChat(first)
	if first.Unread {
		t.Error("switching to the chat did not clear unread")
	}
	tt.Frame()
	if !tt.HasText("background") {
		t.Errorf("background reply not rendered after switching; texts: %q", tt.Texts())
	}
}

func TestPermissionFromBackgroundChat(t *testing.T) {
	app, tt := newFakeApp(t)
	first := connectTab(t, app, tt)
	second := newTab(t, app, tt)
	second = connectTab(t, app, tt)

	sendTo(t, app, tt, first, "/permission")
	waitFor(t, app, tt, func() bool { return app.Permission != nil && app.Permission.Chat == first })

	app.selectChat(second)
	tt.Frame()
	if app.activeChat() != second {
		t.Fatal("could not switch away from the requesting chat")
	}
	if err := tt.Click("Allow once"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Permission == nil && !first.Streaming && chatAssistantHas(first, "permission granted")
	})
	if chatAssistantHas(second, "permission granted") {
		t.Fatal("permission answer leaked into another chat")
	}
	if first.Attention {
		t.Error("attention marker was not cleared after answering")
	}
}

func TestCloseTabKeepsOtherChats(t *testing.T) {
	app, tt := newFakeApp(t)
	first := connectTab(t, app, tt)
	second := newTab(t, app, tt)
	second = connectTab(t, app, tt)

	tt.Frame()
	if err := tt.Click("Conversation actions for " + second.name()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Delete"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(app.Chats) != 1 || app.Chats[0] != first {
		t.Fatalf("chats after close = %+v", app.Chats)
	}
	if first.Session == nil || !first.Connected {
		t.Fatal("closing one tab disconnected another")
	}
	if app.activeChat() != first {
		t.Fatal("active chat did not move to the remaining tab")
	}
}

func TestHistoryOpensNewTabWhenChatHasMessages(t *testing.T) {
	app, tt := newFakeApp(t)
	first := connectTab(t, app, tt)

	first.Input = "keep this"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return !first.Streaming && chatAssistantHas(first, "收到：keep this") })

	tt.Frame()
	if err := tt.Click("History"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.HistoryOpen && !app.HistoryLoading && len(app.HistorySessions) == 1
	})
	tt.Frame()
	if err := tt.Click("Open"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		chat := app.activeChat()
		return chat != first && chat != nil && chat.Info.SessionID == "fake-history" && chat.Busy == ""
	})
	if len(app.Chats) != 2 {
		t.Fatalf("chats = %d, want 2", len(app.Chats))
	}
	if first.Session == nil || !chatAssistantHas(first, "收到：keep this") {
		t.Fatal("opening history lost the running chat")
	}
}

func TestPermissionQueueKeepsChatsSeparate(t *testing.T) {
	app, tt := newFakeApp(t)
	first := connectTab(t, app, tt)
	second := newTab(t, app, tt)
	second = connectTab(t, app, tt)

	sendTo(t, app, tt, first, "/permission")
	waitFor(t, app, tt, func() bool { return app.Permission != nil && app.Permission.Chat == first })

	// The second chat asks while the first dialog is open. The request
	// queues behind it, and the tab is marked.
	app.selectChat(second)
	second.Input = "/permission"
	if !app.send() {
		t.Fatal("could not start the second prompt")
	}
	waitFor(t, app, tt, func() bool { return len(app.permissionQueue) == 1 && second.Attention })

	tt.Frame()
	if err := tt.Click("Allow once"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Permission != nil && app.Permission.Chat == second })
	tt.Frame()
	if err := tt.Click("Allow once"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Permission == nil && !first.Streaming && !second.Streaming &&
			chatAssistantHas(first, "permission granted") && chatAssistantHas(second, "permission granted")
	})
}
