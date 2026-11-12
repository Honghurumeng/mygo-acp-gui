package ui

import (
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/reed/acp-gui/internal/agent"
)

// Tool cards start collapsed: the command shows on one line and the output
// stays hidden until the header is clicked, and a second click closes it.
func TestToolCardStartsCollapsedAndToggles(t *testing.T) {
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
		Agents:    []agent.LaunchSpec{{ID: "fake", Name: "fake-agent"}},
	})
	app.MD.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
	tt := mui.NewTester(app.View, 1060, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})

	chat := app.activeChat()
	message := &Message{
		ID:     app.nextID(),
		Role:   RoleTool,
		Done:   true,
		Title:  "Run the test suite",
		Status: "completed",
		Detail: "42 tests passed",
	}
	chat.Messages = append(chat.Messages, message)
	tt.Frame()

	if message.DetailOpen {
		t.Fatal("tool card starts open")
	}
	if tt.HasText("42 tests passed") {
		t.Fatalf("tool output shown before expanding; texts: %q", tt.Texts())
	}

	open := "Toggle tool output: Run the test suite"
	if err := tt.Click(open); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !message.DetailOpen {
		t.Fatal("click did not open the tool card")
	}
	if !tt.HasText("42 tests passed") {
		t.Fatalf("tool output hidden after expanding; texts: %q", tt.Texts())
	}

	if err := tt.Click(open); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if message.DetailOpen {
		t.Fatal("second click did not collapse the tool card")
	}
	if tt.HasText("42 tests passed") {
		t.Fatalf("tool output still shown after collapsing; texts: %q", tt.Texts())
	}
}
