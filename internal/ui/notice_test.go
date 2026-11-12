package ui

import (
	"strings"
	"testing"
)

// pi-acp's startup prelude and retry status are adapter text, not assistant
// output. They must not leak into the model's answer.
func TestStartupAndRetryNoticesStayOutOfAssistantMessage(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)

	current(app).Input = "/notice hello"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !current(app).Streaming && lastAssistantHas(app, "收到：/notice hello")
	})

	chat := current(app)
	var assistant strings.Builder
	var retries []*Message
	for _, message := range chat.Messages {
		switch message.Role {
		case RoleAssistant:
			if strings.TrimSpace(message.Text) == "" && strings.TrimSpace(message.Thought) == "" {
				t.Errorf("empty assistant row left before notice: %+v", chat.Messages)
			}
			assistant.WriteString(message.Text)
		case RoleSystem:
			if message.NoticeKind == "auto_retry" {
				retries = append(retries, message)
			}
		}
	}

	if text := assistant.String(); strings.Contains(text, "pi v9.9.9") || strings.Contains(text, "Retrying") {
		t.Errorf("adapter status leaked into assistant text: %q", text)
	}
	if len(retries) != 1 {
		t.Fatalf("retry notices = %d, want 1 merged row: %+v", len(retries), retries)
	}
	if retries[0].Text != "Retry finished, resuming." {
		t.Errorf("retry notice = %q", retries[0].Text)
	}
	if retries[0].NoticeLevel != "info" {
		t.Errorf("retry level = %q", retries[0].NoticeLevel)
	}
	if !tt.HasText("Retry finished, resuming.") {
		t.Errorf("retry notice not rendered; texts: %q", tt.Texts())
	}
	if strings.Contains(strings.Join(tt.Texts(), "\n"), "pi v9.9.9") {
		t.Errorf("startup prelude rendered; texts: %q", tt.Texts())
	}
}
