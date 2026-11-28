package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/acp"
)

// newTab opens a draft chat from the workspace "＋" button.
func newTab(t *testing.T, app *App, tt *mui.Tester) *Chat {
	t.Helper()
	tt.Frame()
	label := "New conversation in " + filepath.Base(app.defaultWorkspace())
	if err := tt.Click(label); err != nil {
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
	app.Connect()
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

	// Leaving the chat hides its dialog without answering. The request stays
	// queued and the tab keeps the permission marker.
	app.selectChat(second)
	tt.Frame()
	if app.activeChat() != second {
		t.Fatal("could not switch away from the requesting chat")
	}
	if app.Permission != nil || app.PermissionOpen {
		t.Fatal("background permission stayed visible after switching away")
	}
	if !first.Attention {
		t.Error("background chat is not marked as waiting for permission")
	}
	if tt.HasText("Permission required") {
		t.Error("permission dialog is drawn for another chat")
	}

	// Switching back shows the same, still unanswered request.
	app.selectChat(first)
	tt.Frame()
	if app.Permission == nil || app.Permission.Chat != first {
		t.Fatal("returning to the chat did not reveal its permission")
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

// A request that arrives while another chat is on screen stays queued; it
// must not switch tabs or draw a dialog over the current conversation.
func TestPermissionFromBackgroundChatDoesNotStealFocus(t *testing.T) {
	app, tt := newFakeApp(t)
	first := connectTab(t, app, tt)
	second := newTab(t, app, tt)
	second = connectTab(t, app, tt)

	sendTo(t, app, tt, first, "/permission")
	app.selectChat(second)
	waitFor(t, app, tt, func() bool { return first.Attention })
	if app.activeChat() != second {
		t.Fatalf("permission switched to %q", app.activeChat().name())
	}
	if app.Permission != nil || app.PermissionOpen {
		t.Fatal("background permission popped up without opening its chat")
	}
	tt.Frame()
	if tt.HasText("Permission required") || tt.HasText("Allow once") {
		t.Error("permission dialog is drawn over another chat")
	}

	app.selectChat(first)
	tt.Frame()
	if app.Permission == nil || app.Permission.Chat != first {
		t.Fatal("opening the requesting chat did not show its permission")
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
}

func TestSavedSessionInAnotherWorkspaceStartsOwnProcess(t *testing.T) {
	app, tt := newFakeApp(t)
	first := connectTab(t, app, tt)
	firstPID := first.Session.PID()

	other := t.TempDir()
	app.openSavedSession(savedSession{
		SessionSummary: acp.SessionSummary{SessionID: "fake-history", Cwd: other, Title: "Other history"},
		AgentID:        "fake",
		Show:           true,
	})
	waitFor(t, app, tt, func() bool {
		chat := app.activeChat()
		return chat != nil && chat != first && chat.Connected &&
			chat.Info.SessionID == "fake-history" && chat.Busy == ""
	})

	second := app.activeChat()
	if !sameWorkspace(second.Workspace, other) {
		t.Fatalf("workspace = %q, want %q", second.Workspace, other)
	}
	if second.Session == nil || second.Session.PID() == firstPID {
		t.Fatal("saved session reused the process of another workspace")
	}
	if first.Session == nil || !first.Connected || first.Session.PID() != firstPID {
		t.Fatal("the existing chat's process was closed or replaced")
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

func TestForceStopFromSidebarKeepsTranscript(t *testing.T) {
	app, tt := newFakeApp(t)
	first := connectTab(t, app, tt)
	second := newTab(t, app, tt)
	second = connectTab(t, app, tt)
	sendTo(t, app, tt, second, "keep me")
	waitFor(t, app, tt, func() bool {
		return !second.Streaming && second.Busy == "" && chatAssistantHas(second, "收到：keep me")
	})
	sessionID := second.Info.SessionID
	if !second.canForceStop() {
		t.Fatal("connected chat cannot force stop")
	}

	tt.Frame()
	if err := tt.Click("Conversation actions for " + second.name()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Force stop") {
		t.Fatalf("conversation menu missing Force stop; texts: %q", tt.Texts())
	}
	if err := tt.Click("Force stop"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Force stop this agent process?") {
		t.Fatalf("force stop confirmation missing; texts: %q", tt.Texts())
	}
	if err := tt.Click("Force stop"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return second.Session == nil && !second.Connected && second.sleeping()
	})
	if second.Info.SessionID != sessionID {
		t.Fatalf("force stop lost the session id: %q, want %q", second.Info.SessionID, sessionID)
	}
	if !chatAssistantHas(second, "收到：keep me") {
		t.Fatalf("force stop lost the transcript: %+v", second.Messages)
	}
	if len(app.Chats) != 2 || app.Chats[0] != first || app.Chats[1] != second {
		t.Fatalf("chats after force stop = %+v", app.Chats)
	}
	if first.Session == nil || !first.Connected {
		t.Fatal("force stopping one chat disconnected another")
	}
	if app.activeChat() != second {
		t.Fatal("force stopping a background chat changed the active chat")
	}
}

func TestForceStopDisabledForDraft(t *testing.T) {
	app, tt := newFakeApp(t)
	chat := app.activeChat()
	if chat == nil {
		t.Fatal("no draft chat")
	}
	if chat.canForceStop() {
		t.Fatal("draft without a process can force stop")
	}
	chat.Title = "Draft title"
	tt.Frame()
	if err := tt.Click("Conversation actions for " + chat.name()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Force stop") {
		t.Fatalf("conversation menu missing Force stop; texts: %q", tt.Texts())
	}
	// The disabled item takes no press, so the draft keeps its state.
	if err := tt.Click("Force stop"); err == nil {
		tt.Frame()
	}
	if chat.Title != "Draft title" || chat.Closed || len(app.Chats) != 1 || app.activeChat() != chat {
		t.Fatal("disabled Force stop changed the draft")
	}
}

func TestSavedSessionOpensNewTabWhenChatHasMessages(t *testing.T) {
	app, tt := newFakeApp(t)
	first := connectTab(t, app, tt)

	first.Input = "keep this"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return !first.Streaming && chatAssistantHas(first, "收到：keep this") })
	waitSavedSessions(t, app, tt, 1)

	tt.Frame()
	if err := tt.Click("Open saved session: Fake history"); err != nil {
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

	// Switching away parks the first prompt. The second chat's own request
	// shows immediately because that chat is now on screen.
	app.selectChat(second)
	tt.Frame()
	if app.Permission != nil || !first.Attention {
		t.Fatalf("first prompt was not parked: permission=%v attention=%v", app.Permission, first.Attention)
	}
	second.Input = "/permission"
	if !app.send() {
		t.Fatal("could not start the second prompt")
	}
	waitFor(t, app, tt, func() bool { return app.Permission != nil && app.Permission.Chat == second })

	tt.Frame()
	if err := tt.Click("Allow once"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Permission == nil && !second.Streaming && chatAssistantHas(second, "permission granted")
	})
	if chatAssistantHas(first, "permission granted") {
		t.Fatal("second answer settled the first chat")
	}

	app.selectChat(first)
	tt.Frame()
	if app.Permission == nil || app.Permission.Chat != first {
		t.Fatal("first prompt did not return")
	}
	if err := tt.Click("Allow once"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Permission == nil && !first.Streaming && !second.Streaming &&
			chatAssistantHas(first, "permission granted") && chatAssistantHas(second, "permission granted")
	})
}

func TestDisconnectLeavesFreshDraft(t *testing.T) {
	app, tt := newFakeApp(t)
	chat := connectTab(t, app, tt)
	chat.Title = "Older conversation"
	// Pretend the tab was opened long ago. Without resetting the draft's
	// creation time its row would sort by that old stamp after the process
	// closes, even though it is a brand-new conversation.
	chat.createdAt = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	tt.Frame()

	app.disconnectChat(chat)
	waitFor(t, app, tt, func() bool { return chat.Session == nil && !chat.Connected })

	if chat.Title != "" || chat.Info.SessionID != "" || len(chat.Messages) != 0 {
		t.Fatalf("closed process did not reset the tab: %+v", chat)
	}
	if time.Since(chat.createdAt) > time.Minute {
		t.Fatalf("draft kept its old creation time: %v", chat.createdAt)
	}
	rows := app.workspaceRows(chat.Workspace)
	if len(rows) == 0 || rows[0].chat != chat {
		t.Fatalf("fresh draft is not the first row: %+v", rows)
	}
	if !tt.HasText(chat.name()) {
		t.Fatalf("draft missing from the sidebar; name %q texts: %q", chat.name(), tt.Texts())
	}
}

func TestNewConversationReplacesUnusedDraft(t *testing.T) {
	app, tt := newFakeApp(t)
	first := connectTab(t, app, tt)
	tt.Frame()
	app.disconnectChat(first)
	waitFor(t, app, tt, func() bool { return first.Session == nil && !first.Connected })
	tt.Frame()

	newest := newTab(t, app, tt)
	if len(app.Chats) != 1 || app.Chats[0] != newest {
		t.Fatalf("unused draft was not replaced: %+v", app.Chats)
	}
	if newest == first || !first.Closed {
		t.Fatalf("new conversation reused the old draft: newest=%+v first=%+v", newest, first)
	}
	rows := app.workspaceRows(newest.Workspace)
	if len(rows) == 0 || rows[0].chat != newest {
		t.Fatalf("new conversation is not the first row: %+v", rows)
	}
}

func TestNewConversationKeepsDraftWithInput(t *testing.T) {
	app, tt := newFakeApp(t)
	draft := app.activeChat()
	draft.Input = "not sent yet"
	tt.Frame()

	next := newTab(t, app, tt)
	if next == draft || len(app.Chats) != 2 || app.Chats[0] != draft {
		t.Fatalf("draft with input was dropped: %+v", app.Chats)
	}
	if draft.Input != "not sent yet" {
		t.Fatalf("draft input = %q", draft.Input)
	}
}

func TestForceStopInterruptsStreamingTurn(t *testing.T) {
	app, tt := newFakeApp(t)
	chat := connectTab(t, app, tt)
	chat.Input = "/slow keep streaming"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return chat.Streaming })

	app.openForceStop(chat)
	if !app.ForceStopOpen || app.ForceStopChat != chat {
		t.Fatal("force stop dialog did not open")
	}
	app.confirmForceStop()
	waitFor(t, app, tt, func() bool {
		return chat.Session == nil && !chat.Connected && !chat.Streaming
	})
	if !chat.sleeping() {
		t.Fatal("force stopped streaming chat is not sleeping")
	}
	if !hasRoleText(app, RoleSystem, "Stopped by user.") {
		t.Fatalf("force stop notice missing; messages: %+v", chat.Messages)
	}
	for _, message := range chat.Messages {
		if message.Role == RoleAssistant && !message.Done {
			t.Fatal("interrupted assistant row is still streaming")
		}
	}
	if chat.Info.SessionID == "" {
		t.Fatal("force stop lost the session id")
	}
}
