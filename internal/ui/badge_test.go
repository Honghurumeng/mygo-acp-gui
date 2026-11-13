package ui

import (
	"testing"

	"github.com/reed/acp-gui/internal/agent"
)

func TestDockBadgeCountsWaitingChats(t *testing.T) {
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
		Agents:    []agent.LaunchSpec{{ID: "fake", Name: "fake-agent"}},
	})
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	var counts []int
	app.setDockBadge = func(n int) { counts = append(counts, n) }

	first := current(app)
	second := app.newChatTab()

	// A background chat that finished while another one was selected.
	first.Unread = true
	app.updateDockBadge()

	// Still running: not finished, so not counted yet.
	first.Streaming = true
	app.updateDockBadge()
	first.Streaming = false
	app.updateDockBadge()

	// A permission request counts even when the chat is on screen.
	second.Attention = true
	app.updateDockBadge()

	// Turning the setting off clears the badge.
	app.DockBadgeEnabled = false
	app.updateDockBadge()

	// Removing the waiting chats clears it again, without duplicate calls.
	app.DockBadgeEnabled = true
	second.Attention = false
	first.Closed = true
	app.updateDockBadge()
	app.updateDockBadge()

	want := []int{1, 0, 1, 2, 0}
	if len(counts) != len(want) {
		t.Fatalf("badge counts = %v, want %v", counts, want)
	}
	for i := range want {
		if counts[i] != want[i] {
			t.Fatalf("badge counts = %v, want %v", counts, want)
		}
	}
}

// A turn that finishes in a background chat raises the badge, and selecting
// that chat clears it again; while the turn streams the badge stays clear.
func TestBackgroundTurnSetsDockBadge(t *testing.T) {
	app, tt := newFakeApp(t)
	var counts []int
	app.setDockBadge = func(n int) { counts = append(counts, n) }

	first := connectTab(t, app, tt)
	second := newTab(t, app, tt)
	connectTab(t, app, tt)
	sendTo(t, app, tt, first, "/slow background")
	waitFor(t, app, tt, func() bool { return first.Streaming })

	app.selectChat(second)
	tt.Frame()
	waitFor(t, app, tt, func() bool {
		return !first.Streaming && chatAssistantHas(first, "mcp=")
	})
	if len(counts) == 0 || counts[len(counts)-1] != 1 {
		t.Fatalf("badge counts = %v, want the finished background chat", counts)
	}

	app.selectChat(first)
	if counts[len(counts)-1] != 0 {
		t.Fatalf("badge counts = %v, want the badge cleared", counts)
	}
}

func TestActiveChatIsUnreadWhenWindowLosesFocus(t *testing.T) {
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
		Agents:    []agent.LaunchSpec{{ID: "fake", Name: "fake-agent"}},
	})
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	var counts []int
	app.setDockBadge = func(n int) { counts = append(counts, n) }
	focused := false
	app.isFocused = func() bool { return focused }

	chat := current(app)
	app.markChatUnread(chat)
	if !chat.Unread {
		t.Fatal("a finished turn in an unfocused window was not marked unread")
	}
	if counts[len(counts)-1] != 1 {
		t.Fatalf("badge counts = %v, want 1", counts)
	}

	// Coming back to the window shows the active chat, so it is read again.
	focused = true
	app.clearActiveUnread()
	if chat.Unread {
		t.Fatal("focusing the window did not clear the active chat")
	}
	if counts[len(counts)-1] != 0 {
		t.Fatalf("badge counts = %v, want the badge cleared", counts)
	}
}

func TestPermissionRequestSetsDockBadge(t *testing.T) {
	app, tt := newFakeApp(t)
	var counts []int
	app.setDockBadge = func(n int) { counts = append(counts, n) }

	current(app).Input = "/permission"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Permission != nil })
	if len(counts) == 0 || counts[len(counts)-1] != 1 {
		t.Fatalf("badge counts = %v, want a pending permission", counts)
	}

	if err := tt.Click("Allow once"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !current(app).Streaming && app.Permission == nil
	})
	if counts[len(counts)-1] != 0 {
		t.Fatalf("badge counts = %v, want the badge cleared", counts)
	}
}
