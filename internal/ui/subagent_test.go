package ui

import (
	"strings"
	"testing"
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
	for _, want := range []string{"Subagent · Researcher", "Subagent found the relevant files."} {
		if !tt.HasText(want) {
			t.Errorf("subagent UI missing %q; texts: %q", want, tt.Texts())
		}
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
