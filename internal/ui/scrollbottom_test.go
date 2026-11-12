package ui

import (
	"fmt"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
)

// TestJumpToLatestButtonFollowsTheBottom is the conversation's floating
// action: it shows only while the list is scrolled away from its end, and a
// click returns the list to the end and hides it again.
func TestJumpToLatestButtonFollowsTheBottom(t *testing.T) {
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
		Agents:    []agent.LaunchSpec{{ID: "fake", Name: "fake-agent"}},
	})
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	chat := current(app)
	for i := 0; i < 60; i++ {
		app.addMessage(chat, &Message{ID: app.nextID(), Role: RoleAssistant, Done: true,
			Text: fmt.Sprintf("answer %d with enough text to wrap onto another line in the conversation list", i)})
	}
	tt := mui.NewTester(app.View, 1060, 760)
	tt.Frame()

	if !chat.list.AtEnd() {
		t.Fatal("the conversation did not start at its end")
	}
	if _, ok := tt.Find("Jump to latest"); ok {
		t.Fatal("the button shows while the list is at its end")
	}

	// A row arriving while the list follows its end glides it down; that is
	// still following, not scrolled away, so the button does not flash.
	app.addMessage(chat, &Message{ID: app.nextID(), Role: RoleAssistant, Done: true, Text: "a new answer"})
	tt.Frame()
	if _, ok := tt.Find("Jump to latest"); ok {
		t.Fatal("the button shows while the list glides to a new row")
	}

	// Scrolling up leaves the end: the next frame shows the button.
	tt.Move(600, 300)
	tt.Scroll(600, 300, 0, -600)
	tt.Frame()
	if chat.list.AtEnd() {
		t.Fatal("the list stayed at its end after scrolling up")
	}
	button, ok := tt.Find("Jump to latest")
	if !ok {
		t.Fatalf("the button is missing away from the end; texts: %q", tt.Texts())
	}
	if button.Y < 0 || button.Y+button.H > 760 {
		t.Errorf("the button is outside the window at y=%v h=%v", button.Y, button.H)
	}

	if err := tt.Click("Jump to latest"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !chat.list.AtEnd() {
		t.Error("clicking the button did not return to the end")
	}
	if _, ok := tt.Find("Jump to latest"); ok {
		t.Error("the button stayed after returning to the end")
	}
}
