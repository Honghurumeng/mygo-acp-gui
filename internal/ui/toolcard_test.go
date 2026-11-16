package ui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	mui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/session"
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

// Tool cards, errors, notices and turn summaries keep the reply measure: on a
// wide window they stop at chatMaxWidth instead of stretching across the list.
func TestConversationRowsShareMaxWidth(t *testing.T) {
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
	app.GitPanelOpen = false
	long := strings.Repeat("wide ", 200)
	errText := "error " + long
	sysText := "notice " + long
	app.addMessage(chat, &Message{ID: app.nextID(), Role: RoleTool, Done: true,
		Title: "wide tool", Status: "completed", Detail: "output"})
	app.addMessage(chat, &Message{ID: app.nextID(), Role: RoleError, Done: true, Text: errText})
	app.addMessage(chat, &Message{ID: app.nextID(), Role: RoleSystem, Done: true, Text: sysText})
	app.addMessage(chat, &Message{ID: app.nextID(), Role: RoleTurn, Done: true, Elapsed: time.Second})
	tt := mui.NewTester(app.View, 1400, 760)
	tt.Frame()

	tool, ok := tt.Find("Completed")
	if !ok {
		t.Fatalf("tool card missing; texts: %q", tt.Texts())
	}
	errRect, ok := tt.Find("⚠ " + errText)
	if !ok {
		t.Fatalf("error row missing; texts: %q", tt.Texts())
	}
	if errRect.W < chatMaxWidth-1 || errRect.W > chatMaxWidth {
		t.Errorf("error row is %v wide, the measure should be %v", errRect.W, chatMaxWidth)
	}
	// Every row ends at the error row's left edge plus the measure; the tool
	// card's status sits one padding inside its card.
	wantRight := errRect.X + chatMaxWidth
	if got := tool.X + tool.W; got > wantRight-8 {
		t.Errorf("tool card ends at %v, want inside the row edge at %v", got, wantRight)
	}
	sysRect, ok := tt.Find(sysText)
	if !ok {
		t.Fatalf("system row missing; texts: %q", tt.Texts())
	}
	if sysRect.W > chatMaxWidth {
		t.Errorf("system row is %v wide, want at most %v", sysRect.W, chatMaxWidth)
	}
	turn, ok := tt.Find("Took 1s")
	if !ok {
		t.Fatalf("turn summary missing; texts: %q", tt.Texts())
	}
	// The summary is right-aligned in its row, so its right edge is the
	// row's: it should meet the measure's right edge, not the window's.
	if got := turn.X + turn.W; got < wantRight-2 || got > wantRight+2 {
		t.Errorf("turn summary ends at %v, want the row edge at %v", got, wantRight)
	}
}

func TestSubagentControlCallsUseReadableEvents(t *testing.T) {
	state := json.RawMessage(`{"agentsStates":{"child-1":{"status":"completed"}},"senderThreadId":"root"}`)
	wait := session.ToolCall{Title: "wait", Content: state}
	if got := toolDetail(wait); got != "" {
		t.Errorf("wait exposed transport JSON: %q", got)
	}
	message := &Message{Title: "wait", Detail: `{"agentId":"child-1"}`}
	applyToolPresentation(message, wait)
	if message.Title != "Wait for subagent" || message.Detail != "" {
		t.Errorf("wait presentation = %+v", message)
	}
	// A later status-only update omits the payload that identified the call.
	// It must not regress the title or restore raw JSON.
	applyToolPresentation(message, session.ToolCall{Title: "wait", Status: "completed"})
	if message.Title != "Wait for subagent" || message.Detail != "" {
		t.Errorf("wait status update regressed presentation: %+v", message)
	}

	closeAgent := session.ToolCall{Title: "closeAgent", RawInput: json.RawMessage(`{"agentId":"child-1"}`)}
	if got := toolDetail(closeAgent); got != "" {
		t.Errorf("closeAgent exposed transport JSON: %q", got)
	}
	applyToolPresentation(message, closeAgent)
	if message.Title != "Close subagent" || message.Detail != "" {
		t.Errorf("closeAgent presentation = %+v", message)
	}

	// A generic tool named wait keeps its payload unless the result contains
	// subagent state; the title is not enough to classify it.
	generic := session.ToolCall{Title: "wait", RawInput: json.RawMessage(`{"seconds":1}`)}
	if got := toolDetail(generic); got != `{"seconds":1}` {
		t.Errorf("generic wait detail = %q", got)
	}
	message = &Message{}
	applyToolPresentation(message, generic)
	if message.Title != "wait" || message.Detail != `{"seconds":1}` {
		t.Errorf("generic wait presentation = %+v", message)
	}
	if got := toolStatusLabel("completed"); got != "Completed" {
		t.Errorf("completed tool status = %q", got)
	}
}
