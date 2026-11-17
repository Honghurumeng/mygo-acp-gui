package ui

import (
	"strings"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/session"
)

func TestSubagentSessionStreamsIntoParentChat(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	chat := current(app)

	chat.Input = "/subagent"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !chat.Streaming && chatMessageHas(chat, "Parent received the subagent result.")
	})

	sub := chat.Subagents["fake-subagent-1"]
	if sub == nil {
		t.Fatalf("subagent session was not registered: %+v", chat.Messages)
	}
	if sub.Title != "Researcher" || sub.Description != "Inspects the workspace" || sub.State != "completed" {
		t.Errorf("subagent state = %+v", sub)
	}
	if sub.Card == nil || sub.Card.Status != "completed" || !strings.Contains(sub.Card.Title, "Researcher") {
		t.Errorf("subagent card = %+v", sub.Card)
	}
	if sub.Card != nil && sub.Card.Role != RoleSubagent {
		t.Errorf("subagent card role = %v, want RoleSubagent", sub.Card.Role)
	}
	for _, want := range []string{
		"Inspect the requested workspace.",
		"I will inspect the files first.",
		"Read workspace files",
		"Subagent found the relevant files.",
	} {
		if !chatMessageHas(chat, want) {
			t.Errorf("subagent transcript missing %q: %+v", want, chat.Messages)
		}
	}
	taskRows := 0
	for _, message := range chat.Messages {
		if message != nil && message.Role == RoleInbound && strings.Contains(message.Text, "Inspect the requested workspace.") {
			taskRows++
		}
	}
	if taskRows != 1 {
		t.Errorf("subagent task rendered %d times, want one recipient-view row: %+v", taskRows, chat.Messages)
	}

	tt.Frame()
	for _, want := range []string{"SUBAGENT", "Researcher", "Completed", "Subagent found the relevant files.", "Parent received the subagent result."} {
		if !tt.HasText(want) {
			t.Errorf("subagent UI missing %q; texts: %q", want, tt.Texts())
		}
	}
	for _, hidden := range []string{"I will inspect the files first.", "Read workspace files"} {
		if tt.HasText(hidden) {
			t.Errorf("collapsed subagent exposed child transcript %q; texts: %q", hidden, tt.Texts())
		}
	}
	heading, headingOK := tt.Find("SUBAGENT")
	description, descriptionOK := tt.Find("Inspects the workspace")
	if !headingOK || !descriptionOK {
		t.Fatalf("subagent card content missing; texts: %q", tt.Texts())
	}
	if description.Y <= heading.Y {
		t.Errorf("subagent description is above the card header: heading=%+v description=%+v", heading, description)
	}

	rows := conversationRows(chat)
	for _, want := range []string{"I will inspect the files first.", "Read workspace files"} {
		if rowsAfterContain(rows, want) {
			t.Errorf("collapsed subagent rows contain %q", want)
		}
	}
	sub.Card.DetailOpen = true
	rows = conversationRows(chat)
	cardRow := -1
	for i, message := range rows {
		if message == sub.Card {
			cardRow = i
			break
		}
	}
	if cardRow < 0 {
		t.Fatal("expanded conversation has no subagent card")
	}
	for _, want := range []string{"Inspect the requested workspace.", "I will inspect the files first.", "Read workspace files", "Subagent found the relevant files."} {
		if !rowsAfterContain(rows[cardRow+1:], want) {
			t.Errorf("expanded subagent rows missing %q", want)
		}
	}
}

func TestSubagentCardTogglesTranscript(t *testing.T) {
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
	sub := &Subagent{ID: "child-1", Title: "Researcher", State: "running"}
	sub.Card = &Message{
		ID:         app.nextID(),
		Role:       RoleSubagent,
		Title:      "Subagent · Researcher",
		Status:     "in_progress",
		SubagentID: sub.ID,
		Done:       true,
	}
	chat.Subagents = map[string]*Subagent{sub.ID: sub}
	app.addMessage(chat, sub.Card)
	app.addMessage(chat, &Message{
		ID:           app.nextID(),
		Role:         RoleAssistant,
		Text:         "Child result.",
		SubagentID:   sub.ID,
		SubagentName: sub.Title,
		Done:         true,
	})
	app.addMessage(chat, &Message{
		ID:           app.nextID(),
		Role:         RoleTool,
		Title:        "Read child files",
		Status:       "completed",
		ToolCallID:   "child-tool-1",
		SubagentID:   sub.ID,
		SubagentName: sub.Title,
		Done:         true,
	})

	tt := mui.NewTester(app.View, 1060, 760)
	tt.Frame()
	if tt.HasText("Read child files") {
		t.Fatal("collapsed card exposed the child transcript")
	}
	label := "Toggle subagent details: Subagent · Researcher"
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !sub.Card.DetailOpen || !tt.HasText("Read child files") {
		t.Fatalf("click did not expand child transcript; texts: %q", tt.Texts())
	}
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if sub.Card.DetailOpen || tt.HasText("Read child files") {
		t.Fatalf("second click did not collapse child transcript; texts: %q", tt.Texts())
	}
}

func TestSubagentStatusLabels(t *testing.T) {
	tests := []struct {
		state string
		want  string
	}{
		{state: "starting", want: "Starting"},
		{state: "in_progress", want: "Running"},
		{state: "pending", want: "Waiting"},
		{state: "idle", want: "Completed"},
		{state: "completed", want: "Completed"},
		{state: "cancelled", want: "Cancelled"},
		{state: "failed", want: "Failed"},
		{state: "disconnected", want: "Disconnected"},
		{state: "unknown", want: "Unknown"},
	}
	for _, test := range tests {
		t.Run(test.state, func(t *testing.T) {
			if got := subagentStatusLabel(subagentStatus(&Subagent{State: test.state})); got != test.want {
				t.Errorf("subagentStatusLabel(%q) = %q, want %q", test.state, got, test.want)
			}
		})
	}
}

func TestLastSubagentReplyIsPlainSummary(t *testing.T) {
	chat := &Chat{Messages: []*Message{
		{Role: RoleAssistant, Text: "older reply", SubagentID: "child-1"},
		{Role: RoleAssistant, Text: "Found `app.go` and **finished**.\n\nDetails omitted.", SubagentID: "child-1"},
		{Role: RoleAssistant, Text: "parent reply"},
	}}
	if got := lastSubagentReply(chat, "child-1"); got != "Found app.go and finished. Details omitted." {
		t.Errorf("summary = %q", got)
	}
}

func TestSubagentPermissionRoutesToParentChat(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	chat := current(app)

	chat.Input = "/subagent /permission"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Permission != nil && app.Permission.Request.SessionID == "fake-subagent-1"
	})
	if sub := chat.Subagents["fake-subagent-1"]; sub == nil || sub.Title != "Researcher" {
		t.Errorf("permission subagent is not registered: %+v", sub)
	}
	tt.Frame()
	if !tt.HasText("Subagent: Researcher") || !tt.HasText("Let the subagent run a command") {
		t.Fatalf("subagent permission dialog missing context; texts: %q", tt.Texts())
	}

	if err := tt.Click("Allow once"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Permission == nil && !chat.Streaming && chatMessageHas(chat, "Subagent permission granted.")
	})
}

func TestTaskSummaryIgnoresSubagentReply(t *testing.T) {
	chat := &Chat{Messages: []*Message{
		{Role: RoleAssistant, Text: "Main task summary."},
		{Role: RoleAssistant, Text: "Child investigation notes.", SubagentID: "child-1", SubagentName: "Researcher"},
	}}
	if got := lastAgentParagraph(chat, nil); got != "Main task summary." {
		t.Errorf("task summary = %q, want the root assistant reply", got)
	}
}

func TestNotificationSummaryIgnoresSubagentReply(t *testing.T) {
	chat := &Chat{Messages: []*Message{
		{Role: RoleAssistant, Text: "Main answer."},
		{Role: RoleAssistant, Text: "Child investigation notes.", SubagentID: "child-1", SubagentName: "Researcher"},
	}}
	if got := notificationSummary(chat); got != "Main answer." {
		t.Errorf("notification summary = %q, want the root assistant reply", got)
	}
}

func chatMessageHas(chat *Chat, want string) bool {
	if chat == nil {
		return false
	}
	for _, message := range chat.Messages {
		if message != nil && (strings.Contains(message.Text, want) || strings.Contains(message.Thought, want) ||
			strings.Contains(message.Title, want) || strings.Contains(message.Detail, want)) {
			return true
		}
	}
	return false
}

func rowsAfterContain(rows []*Message, want string) bool {
	for _, message := range rows {
		if message == nil {
			continue
		}
		if strings.Contains(message.Text, want) || strings.Contains(message.Thought, want) ||
			strings.Contains(message.Title, want) || strings.Contains(message.Detail, want) {
			return true
		}
	}
	return false
}

func TestSubagentUserChunkUsesInboundRow(t *testing.T) {
	app := New(Options{Version: "test", Workspace: t.TempDir(), Agents: []agent.LaunchSpec{}})
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	chat := app.activeChat()
	sub := app.ensureSubagent(chat, "child", "root")
	if sub == nil {
		t.Fatal("subagent was not created")
	}
	app.applySubagentEvent(chat, session.Event{Kind: session.UpdateUserChunk, SessionID: "child", Text: "do the thing"})
	app.applySubagentEvent(chat, session.Event{Kind: session.UpdateUserChunk, SessionID: "child", Text: " now"})
	var inbound []*Message
	for _, message := range chat.Messages {
		if message.Role == RoleInbound && message.SubagentID == "child" {
			inbound = append(inbound, message)
		}
	}
	if len(inbound) != 1 || inbound[0].Text != "do the thing now" {
		t.Fatalf("inbound rows = %+v", inbound)
	}

	// An assistant reply closes the inbound row; the next user chunk starts a
	// new one instead of appending to the conversation's earlier row.
	app.applySubagentEvent(chat, session.Event{Kind: session.UpdateMessageChunk, SessionID: "child", Text: "working"})
	app.applySubagentEvent(chat, session.Event{Kind: session.UpdateUserChunk, SessionID: "child", Text: "next"})
	inbound = nil
	for _, message := range chat.Messages {
		if message.Role == RoleInbound && message.SubagentID == "child" {
			inbound = append(inbound, message)
		}
	}
	if len(inbound) != 2 || inbound[0].Text != "do the thing now" || inbound[1].Text != "next" {
		t.Fatalf("inbound rows after reply = %+v", inbound)
	}
}
