package ui

import (
	"strings"
	"testing"
)

func TestPromptUsageFallback(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	chat := current(app)
	chat.Input = "/turnusage hello"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !current(app).Streaming && current(app).TurnUsage != nil
	})

	chat = current(app)
	if chat.Usage != nil {
		t.Errorf("context usage should be absent, got %+v", chat.Usage)
	}
	usage := chat.TurnUsage
	if usage.InputTokens != 1200 || usage.OutputTokens != 100 || usage.ThoughtTokens != 40 {
		t.Errorf("turn usage = %+v", usage)
	}
	if !tt.HasText("turn 1.2k in · 100 out · 40 thought") {
		t.Errorf("turn usage not rendered; texts: %q", tt.Texts())
	}

	// A new turn clears the previous report until its own result arrives.
	chat.Input = "plain"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !current(app).Streaming && lastAssistantHas(app, "收到：plain")
	})
	if current(app).TurnUsage != nil {
		t.Errorf("turn usage carried into the next turn: %+v", current(app).TurnUsage)
	}
	if !tt.HasText("12%") {
		t.Errorf("context usage missing; texts: %q", tt.Texts())
	}
	if strings.Contains(strings.Join(tt.Texts(), "\n"), "turn 1.2k in") {
		t.Errorf("stale turn usage still rendered; texts: %q", tt.Texts())
	}
}
