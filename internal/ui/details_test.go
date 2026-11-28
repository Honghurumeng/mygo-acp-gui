package ui

import (
	"strings"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/reed/acp-gui/internal/agent"
)

// A finished turn reads as body text plus the final summary: the thinking
// block and the tool cards fold into one counts row until it is clicked.
func TestTurnDetailsFoldsThinkingAndTools(t *testing.T) {
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
	prompt := &Message{ID: app.nextID(), Role: RoleUser, Text: "Fix the failing test", Done: true}
	reply := &Message{ID: app.nextID(), Role: RoleAssistant, Done: true,
		Thought: "I should read a.go first.", Text: "Looking at a.go."}
	tool := &Message{ID: app.nextID(), Role: RoleTool, Done: true,
		Title: "read a.go", Status: "completed", Detail: "package a"}
	thoughtOnly := &Message{ID: app.nextID(), Role: RoleAssistant, Done: true,
		Thought: "The fix is in b.go."}
	summary := &Message{ID: app.nextID(), Role: RoleAssistant, Text: "Done: all tests pass.", Done: true}
	chat.Messages = append(chat.Messages, prompt, reply, tool, thoughtOnly, summary)

	tt.Frame()
	for _, want := range []string{"Looking at a.go.", "Done: all tests pass.",
		"1 thought", "1 thought · 1 tool call"} {
		if !tt.HasText(want) {
			t.Fatalf("folded turn missing %q; texts: %q", want, tt.Texts())
		}
	}
	for _, hidden := range []string{"read a.go", "I should read a.go first.", "The fix is in b.go.", "Thinking"} {
		if tt.HasText(hidden) {
			t.Errorf("folded turn exposes %q; texts: %q", hidden, tt.Texts())
		}
	}

	// The reply's body text splits the run: its own thought counts above the
	// text, the tool and the following thought-only reply count below it.
	rows := conversationRows(chat)
	if len(rows) != 5 {
		t.Fatalf("folded rows = %d, want 5 (prompt, two runs, two replies): %v", len(rows), rowRoles(rows))
	}
	if rows[1].Role != RoleDetails || rows[1].GroupThoughts != 1 || rows[1].GroupTools != 0 {
		t.Fatalf("first run = %+v, want one thought", rows[1])
	}
	if rows[3].Role != RoleDetails || rows[3].GroupThoughts != 1 || rows[3].GroupTools != 1 {
		t.Fatalf("second run = %+v, want one thought and one tool", rows[3])
	}
	if !rows[2].hideThought {
		t.Error("reply above the fold still draws its thinking block")
	}

	if err := tt.Click("Toggle thinking and tool details: 1 thought · 1 tool call"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"read a.go", "Thinking", "Done: all tests pass."} {
		if !tt.HasText(want) {
			t.Errorf("expanded run missing %q; texts: %q", want, tt.Texts())
		}
	}
	if rows := conversationRows(chat); len(rows) != 7 {
		t.Fatalf("expanded rows = %d, want 7: %v", len(rows), rowRoles(rows))
	}

	if err := tt.Click("Toggle thinking and tool details: 1 thought · 1 tool call"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("read a.go") || tt.HasText("Thinking") {
		t.Errorf("second click did not fold the run; texts: %q", tt.Texts())
	}
}

// Opening one run's details leaves the other runs folded: the state is keyed
// by the run's first hidden row, not shared by the chat.
func TestTurnDetailsFoldPerTurn(t *testing.T) {
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
	first := &Message{ID: app.nextID(), Role: RoleUser, Text: "first", Done: true}
	firstTool := &Message{ID: app.nextID(), Role: RoleTool, Done: true, Title: "read a.go"}
	firstReply := &Message{ID: app.nextID(), Role: RoleAssistant, Text: "First answer.", Done: true}
	second := &Message{ID: app.nextID(), Role: RoleUser, Text: "second", Done: true}
	secondTool := &Message{ID: app.nextID(), Role: RoleTool, Done: true, Title: "read b.go"}
	secondReply := &Message{ID: app.nextID(), Role: RoleAssistant, Text: "Second answer.", Done: true}
	chat.Messages = append(chat.Messages,
		first, firstTool, firstReply, second, secondTool, secondReply)

	// The run starts at the first hidden row, not at the prompt.
	*chat.detailsOpen(firstTool.ID) = true
	tt.Frame()
	if !tt.HasText("read a.go") {
		t.Fatalf("opened run hides its tool card; texts: %q", tt.Texts())
	}
	if tt.HasText("read b.go") {
		t.Errorf("second run follows the first run's fold state; texts: %q", tt.Texts())
	}
	if *chat.detailsOpen(secondTool.ID) {
		t.Errorf("second run unexpectedly opened")
	}
}

// Messages before the first prompt keep their flat shape: startup notices and
// replayed fragments are not a turn, and the tool card tests build chats
// without prompts.
func TestTurnDetailsKeepFlatWithoutPrompt(t *testing.T) {
	chat := &Chat{Messages: []*Message{
		{ID: 1, Role: RoleAssistant, Text: "startup", Done: true},
		{ID: 2, Role: RoleTool, Title: "read a.go", Done: true},
	}}
	rows := conversationRows(chat)
	if len(rows) != 2 {
		t.Fatalf("rows = %v, want both drawn flat", rowRoles(rows))
	}
	if rows[1].Role != RoleTool {
		t.Fatalf("tool row was folded: %v", rowRoles(rows))
	}
}

func TestDetailsLabel(t *testing.T) {
	cases := []struct {
		thoughts, tools int
		want            string
	}{
		{0, 0, ""},
		{1, 0, "1 thought"},
		{2, 0, "2 thoughts"},
		{0, 1, "1 tool call"},
		{0, 3, "3 tool calls"},
		{1, 1, "1 thought · 1 tool call"},
		{4, 2, "4 thoughts · 2 tool calls"},
	}
	for _, c := range cases {
		if got := detailsLabel(c.thoughts, c.tools); got != c.want {
			t.Errorf("detailsLabel(%d, %d) = %q, want %q", c.thoughts, c.tools, got, c.want)
		}
	}
}

// A think-kind tool call (codex reasoning) counts as thinking, not as a tool.
func TestTurnDetailsCountsThinkAsThought(t *testing.T) {
	chat := &Chat{Messages: []*Message{
		{ID: 1, Role: RoleUser, Text: "question", Done: true},
		{ID: 2, Role: RoleTool, ToolKind: "think", Title: "Guardian Review", Done: true},
		{ID: 3, Role: RoleAssistant, Text: "answer", Done: true},
	}}
	rows := conversationRows(chat)
	if len(rows) != 3 {
		t.Fatalf("rows = %v", rowRoles(rows))
	}
	if rows[1].Role != RoleDetails || rows[1].GroupThoughts != 1 || rows[1].GroupTools != 0 {
		t.Fatalf("details row = %+v, want one thought and no tools", rows[1])
	}
}

func rowRoles(rows []*Message) string {
	names := make([]string, len(rows))
	for i, row := range rows {
		names[i] = strings.ToLower(messageRoleName(row.Role))
	}
	return strings.Join(names, ",")
}

func messageRoleName(role MessageRole) string {
	switch role {
	case RoleUser:
		return "User"
	case RoleAssistant:
		return "Assistant"
	case RoleTool:
		return "Tool"
	case RoleDetails:
		return "Details"
	case RoleSystem:
		return "System"
	case RoleError:
		return "Error"
	case RoleTurn:
		return "Turn"
	case RoleInbound:
		return "Inbound"
	case RoleSubagent:
		return "Subagent"
	}
	return "Unknown"
}

// A reply with body text splits the run: the thinking and tool before it count
// together above that text, and the work after it gets its own counts row.
func TestTurnDetailsSplitsAtReplyText(t *testing.T) {
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
	prompt := &Message{ID: app.nextID(), Role: RoleUser, Text: "为什么 TestFoo 挂了？修一下", Done: true}
	thinkA := &Message{ID: app.nextID(), Role: RoleAssistant, Done: true,
		Thought: "先看测试文件"}
	read := &Message{ID: app.nextID(), Role: RoleTool, ToolKind: "read", Done: true,
		Title: "read internal/foo/foo_test.go"}
	thinkB := &Message{ID: app.nextID(), Role: RoleAssistant, Done: true,
		Thought: "断言写错了", Text: "找到了：断言写错了。"}
	edit := &Message{ID: app.nextID(), Role: RoleTool, ToolKind: "edit", Done: true,
		Title: "edit internal/foo/foo_test.go"}
	summary := &Message{ID: app.nextID(), Role: RoleAssistant, Done: true,
		Text: "已修复，全部通过。"}
	chat.Messages = append(chat.Messages, prompt, thinkA, read, thinkB, edit, summary)

	tt.Frame()
	rows := conversationRows(chat)
	want := []string{"user", "details", "assistant", "details", "assistant"}
	if got := rowRoles(rows); got != strings.Join(want, ",") {
		t.Fatalf("rows = %s, want %s", got, strings.Join(want, ","))
	}
	if rows[1].GroupThoughts != 2 || rows[1].GroupTools != 1 {
		t.Errorf("first run = %d thoughts / %d tools, want 2/1",
			rows[1].GroupThoughts, rows[1].GroupTools)
	}
	if rows[3].GroupThoughts != 0 || rows[3].GroupTools != 1 {
		t.Errorf("second run = %d thoughts / %d tools, want 0/1",
			rows[3].GroupThoughts, rows[3].GroupTools)
	}
	for _, hidden := range []string{"read internal/foo/foo_test.go", "edit internal/foo/foo_test.go", "Thinking"} {
		if tt.HasText(hidden) {
			t.Errorf("folded runs expose %q; texts: %q", hidden, tt.Texts())
		}
	}

	first, ok := tt.Find("2 thoughts · 1 tool call")
	if !ok {
		t.Fatalf("first counts row missing; texts: %q", tt.Texts())
	}
	body, ok := tt.Find("找到了：断言写错了。")
	if !ok {
		t.Fatalf("reply body missing; texts: %q", tt.Texts())
	}
	second, ok := tt.Find("1 tool call")
	if !ok {
		t.Fatalf("second counts row missing; texts: %q", tt.Texts())
	}
	tail, ok := tt.Find("已修复，全部通过。")
	if !ok {
		t.Fatalf("summary missing; texts: %q", tt.Texts())
	}
	if !(first.Y < body.Y && body.Y < second.Y && second.Y < tail.Y) {
		t.Errorf("order wrong: first=%v body=%v second=%v summary=%v",
			first.Y, body.Y, second.Y, tail.Y)
	}

	if err := tt.Click("Toggle thinking and tool details: 1 tool call"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("edit internal/foo/foo_test.go") {
		t.Fatalf("second run did not open; texts: %q", tt.Texts())
	}
	if tt.HasText("read internal/foo/foo_test.go") {
		t.Errorf("opening the second run also opened the first; texts: %q", tt.Texts())
	}
	if !tt.HasText("2 thoughts · 1 tool call") {
		t.Errorf("first counts row disappeared; texts: %q", tt.Texts())
	}
}
