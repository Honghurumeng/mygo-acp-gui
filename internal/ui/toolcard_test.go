package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	if got := parseToolCall(wait).Detail; got != "" {
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
	if got := parseToolCall(closeAgent).Detail; got != "" {
		t.Errorf("closeAgent exposed transport JSON: %q", got)
	}
	applyToolPresentation(message, closeAgent)
	if message.Title != "Close subagent" || message.Detail != "" {
		t.Errorf("closeAgent presentation = %+v", message)
	}

	// A generic tool named wait keeps its payload unless the result contains
	// subagent state; the title is not enough to classify it.
	generic := session.ToolCall{Title: "wait", RawInput: json.RawMessage(`{"seconds":1}`)}
	if got := parseToolCall(generic).Detail; got != `{"seconds":1}` {
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

// A file change draws as a diff: the header names the file and the line
// counts, and the rawInput JSON the old card showed is gone.
func TestEditToolCardShowsDiffNotJSON(t *testing.T) {
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
		Agents:    []agent.LaunchSpec{{ID: "fake", Name: "fake-agent"}},
	})
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
		Title:  "edit app.go",
		Status: "completed",
		// The first update showed the flattened rawInput; the diff replaces it.
		Detail: `{"newText":"TWO\n","oldText":"two\n","path":"app.go"}`,
	}
	applyToolPresentation(message, session.ToolCall{
		ToolCallID: "tool-1",
		Title:      "edit app.go",
		Kind:       "edit",
		Status:     "completed",
		Content:    json.RawMessage(`[{"type":"diff","path":"app.go","oldText":"one\ntwo\n","newText":"one\nTWO\n"}]`),
	})
	chat.Messages = append(chat.Messages, message)
	tt.Frame()

	if message.DetailOpen {
		t.Fatal("diff card starts open")
	}
	for _, want := range []string{"Edited", "app.go", "+1", "−1", "Completed"} {
		if !tt.HasText(want) {
			t.Errorf("header misses %q; texts: %q", want, tt.Texts())
		}
	}
	if tt.HasText("oldText") || tt.HasText(`"newText"`) {
		t.Errorf("rawInput JSON leaked into the card; texts: %q", tt.Texts())
	}

	if err := tt.Click("Toggle tool output: edit app.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !message.DetailOpen {
		t.Fatal("click did not open the diff card")
	}
}

// A command card shows the command, the exit code and, once opened, the
// output; an exit code reads as a failure however the agent labelled it.
func TestExecuteToolCardShowsExitAndOutput(t *testing.T) {
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
		Agents:    []agent.LaunchSpec{{ID: "fake", Name: "fake-agent"}},
	})
	tt := mui.NewTester(app.View, 1060, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})

	chat := app.activeChat()
	message := &Message{
		ID:       app.nextID(),
		Role:     RoleTool,
		Done:     true,
		Title:    "go test ./...",
		Status:   "completed",
		ToolKind: "execute",
		ToolExit: intPtr(2),
		Detail:   "FAIL\tpkg\t0.1s",
	}
	chat.Messages = append(chat.Messages, message)
	tt.Frame()

	if !tt.HasText("go test ./...") || !tt.HasText("exit 2") {
		t.Fatalf("command header missing; texts: %q", tt.Texts())
	}
	if tt.HasText("FAIL") {
		t.Error("command output shown before expanding")
	}
	if err := tt.Click("Toggle tool output: go test ./..."); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !message.DetailOpen || !tt.HasText("FAIL") {
		t.Errorf("output missing after expanding; texts: %q", tt.Texts())
	}
}

// A read card names the file and its range, and a search card sums the result
// rows and highlights the pattern instead of dumping raw JSON.
func TestReadAndSearchToolCards(t *testing.T) {
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
		Agents:    []agent.LaunchSpec{{ID: "fake", Name: "fake-agent"}},
	})
	tt := mui.NewTester(app.View, 1060, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})

	chat := app.activeChat()
	read := &Message{
		ID: app.nextID(), Role: RoleTool, Done: true, Title: "read a.go",
		Status: "completed", ToolKind: "read", ToolPaths: []string{"a.go"},
		ToolOffset: 10, ToolLimit: 20, Detail: "package ui\n\nvar x = 1\n",
	}
	search := &Message{
		ID: app.nextID(), Role: RoleTool, Done: true, Title: `grep "needle" in .`,
		Status: "completed", ToolKind: "search", ToolPattern: "needle",
		Detail: "a.go:1:needle\nb.go:2:needle\n",
	}
	chat.Messages = append(chat.Messages, read, search)
	tt.Frame()

	if !tt.HasText("Read") || !tt.HasText("a.go") || !tt.HasText("10–29") {
		t.Errorf("read header missing; texts: %q", tt.Texts())
	}
	if !tt.HasText("Search") || !tt.HasText("2 matches in 2 files") {
		t.Errorf("search header missing; texts: %q", tt.Texts())
	}
	if err := tt.Click("Toggle tool output: read a.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !read.DetailOpen {
		t.Error("read card did not open")
	}
}

// Reasoning is not an action: it draws without a card surface and without the
// raw JSON. A fetch names its host and links the URL. Structured bodies are
// highlighted instead of shown as one muted line.
func TestThinkFetchAndStructuredToolCards(t *testing.T) {
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
		Agents:    []agent.LaunchSpec{{ID: "fake", Name: "fake-agent"}},
	})
	tt := mui.NewTester(app.View, 1060, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})

	chat := app.activeChat()
	think := &Message{
		ID: app.nextID(), Role: RoleTool, Done: true, Title: "Guardian Review",
		Status: "completed", ToolKind: "think", Detail: "Checked the patch.",
	}
	fetch := &Message{
		ID: app.nextID(), Role: RoleTool, Done: true, Title: "fetch docs",
		Status: "completed", ToolKind: "fetch", ToolURL: "https://example.com/docs",
		Detail: "Documentation text.",
	}
	other := &Message{
		ID: app.nextID(), Role: RoleTool, Done: true, Title: "MCP search on docs",
		Status: "completed", ToolKind: "other", Detail: `{"query":"acp","limit":5}`,
	}
	chat.Messages = append(chat.Messages, think, fetch, other)
	tt.Frame()

	if !tt.HasText("Guardian Review") {
		t.Errorf("think row missing; texts: %q", tt.Texts())
	}
	if !tt.HasText("example.com") {
		t.Errorf("fetch host missing; texts: %q", tt.Texts())
	}
	if !tt.HasText("MCP search on docs") {
		t.Errorf("other tool title missing; texts: %q", tt.Texts())
	}

	if err := tt.Click("Toggle tool output: fetch docs"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("https://example.com/docs") {
		t.Errorf("fetch link missing after expanding; texts: %q", tt.Texts())
	}
	if err := tt.Click("Toggle tool output: MCP search on docs"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("query") {
		t.Errorf("structured body missing after expanding; texts: %q", tt.Texts())
	}
}

// The card's Open file action switches the right column to Files and opens the
// change's file in the shared viewer.
func TestToolCardOpenFileOpensViewer(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := New(Options{
		Version:   "test",
		Workspace: workspace,
		Agents:    []agent.LaunchSpec{{ID: "fake", Name: "fake-agent"}},
	})
	tt := mui.NewTester(app.View, 1060, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})

	chat := app.activeChat()
	chat.Messages = append(chat.Messages, &Message{
		ID: app.nextID(), Role: RoleTool, Done: true, Title: "edit a.go",
		Status: "completed", ToolKind: "edit",
		ToolDiffs: []ToolDiff{{Path: "a.go", Old: "x", New: "y", Operation: "update"}},
	})
	tt.Frame()
	if err := tt.Click("Toggle tool output: edit a.go"); err != nil {
		t.Fatal(err)
	}
	// The panel animates open over 200ms; hit testing needs it settled.
	tt.Advance(250 * time.Millisecond)
	tt.Frame()
	if err := tt.Click("Open file"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.Files.ViewerOpen || app.Files.ViewerPath != "a.go" {
		t.Fatalf("viewer = open:%v path:%q", app.Files.ViewerOpen, app.Files.ViewerPath)
	}
	if app.RightTab != rightTabFiles {
		t.Errorf("right tab = %q, want files", app.RightTab)
	}
}

// Sparse or odd payloads still render one card each and open without panicking:
// agents may send a diff without a path, a fetch without a scheme, invalid JSON
// or a negative exit code.
func TestToolCardsHandleSparsePayloads(t *testing.T) {
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
		Agents:    []agent.LaunchSpec{{ID: "fake", Name: "fake-agent"}},
	})
	tt := mui.NewTester(app.View, 1060, 900)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})

	chat := app.activeChat()
	messages := []*Message{
		{ID: app.nextID(), Role: RoleTool, Done: true, Title: "edit", Status: "completed",
			ToolKind: "edit", ToolDiffs: []ToolDiff{{Operation: "update"}}},
		{ID: app.nextID(), Role: RoleTool, Done: true, Title: "read", Status: "completed",
			ToolKind: "read", Detail: "x"},
		{ID: app.nextID(), Role: RoleTool, Done: true, Title: "fetch", Status: "completed",
			ToolKind: "fetch", ToolURL: "not a url", Detail: "text"},
		{ID: app.nextID(), Role: RoleTool, Done: true, Title: "grep x", Status: "completed",
			ToolKind: "search", Detail: "no result rows"},
		{ID: app.nextID(), Role: RoleTool, Done: true, Title: "think", Status: "completed",
			ToolKind: "think", Detail: "reasoning"},
		{ID: app.nextID(), Role: RoleTool, Done: true, Title: "MCP x", Status: "completed",
			ToolKind: "other", Detail: "[1,2,"},
		{ID: app.nextID(), Role: RoleTool, Done: true, Title: "false", Status: "completed",
			ToolKind: "execute", Detail: "out", ToolExit: intPtr(-1)},
	}
	chat.Messages = append(chat.Messages, messages...)
	tt.Frame()
	for _, message := range messages {
		if err := tt.Click("Toggle tool output: " + message.Title); err != nil {
			t.Fatalf("%s: %v", message.Title, err)
		}
		tt.Advance(250 * time.Millisecond)
		tt.Frame()
	}
	for _, message := range messages {
		if !message.DetailOpen {
			t.Errorf("%s did not open", message.Title)
		}
	}
}

// A flat body longer than a card shows is cut once, with Show all putting the
// whole text back.
func TestToolCardLongTextShowsAll(t *testing.T) {
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
		Agents:    []agent.LaunchSpec{{ID: "fake", Name: "fake-agent"}},
	})
	tt := mui.NewTester(app.View, 1060, 900)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})

	chat := app.activeChat()
	message := &Message{
		ID: app.nextID(), Role: RoleTool, Done: true, Title: "MCP long",
		Status: "completed", ToolKind: "other",
		Detail: strings.Repeat("a", toolTextCardMaxRunes+500),
	}
	chat.Messages = append(chat.Messages, message)
	tt.Frame()
	if err := tt.Click("Toggle tool output: MCP long"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	tt.Frame()
	tt.Frame()
	if err := tt.Click("Show all"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !message.toolOutputOpen {
		t.Fatal("Show all did not expand the body")
	}
}
