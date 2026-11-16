package ui

import (
	"strings"
	"testing"

	"github.com/reed/acp-gui/internal/archive"
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

func TestContextUsagePersistsInArchive(t *testing.T) {
	app, tt := newFakeApp(t)
	chat := connectTab(t, app, tt)
	sendTo(t, app, tt, chat, "remember the context")
	waitFor(t, app, tt, func() bool {
		return !chat.Streaming && chat.Usage != nil
	})

	index := app.archiveIndex("fake", chat.Info.SessionID)
	if index < 0 {
		t.Fatalf("session was not archived: %+v", app.Archived)
	}
	usage := app.Archived[index].Usage
	if usage == nil || usage.Used != 1200 || usage.Size != 10000 {
		t.Fatalf("archived usage = %+v, want 1200/10000", usage)
	}
	if usage.Cost == nil || usage.Cost.Amount != 0.0012 || usage.Cost.Currency != "USD" {
		t.Fatalf("archived cost = %+v", usage.Cost)
	}

	loaded, err := archive.Open(app.archive.Path()).Load()
	if err != nil {
		t.Fatal(err)
	}
	var persisted *archive.Session
	for i := range loaded.Sessions {
		if loaded.Sessions[i].AgentID == "fake" && loaded.Sessions[i].SessionID == chat.Info.SessionID {
			persisted = &loaded.Sessions[i]
			break
		}
	}
	if persisted == nil || persisted.Usage == nil || persisted.Usage.Used != 1200 ||
		persisted.Usage.Size != 10000 || persisted.Usage.Cost == nil {
		t.Fatalf("persisted usage = %+v", loaded.Sessions)
	}
}

func TestSavedSessionRestoresContextUsage(t *testing.T) {
	app, tt := newFakeApp(t)
	index := app.archiveIndex("fake", "fake-history")
	if index < 0 {
		t.Fatal("fake history is missing from the archive")
	}
	app.Archived[index].Usage = &archive.Usage{Used: 1200, Size: 10000}
	app.saveArchive()

	tt.Frame()
	if err := tt.Click("Open saved session: Fake history"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		chat := app.activeChat()
		return chat != nil && chat.Connected && chat.Info.SessionID == "fake-history" && !chat.replaying
	})
	chat := app.activeChat()
	if chat.Usage == nil || chat.Usage.Used != 1200 || chat.Usage.Size != 10000 {
		t.Fatalf("restored usage = %+v, want 1200/10000", chat.Usage)
	}
	if !tt.HasText("12%") {
		t.Errorf("restored context ring not rendered; texts: %q", tt.Texts())
	}
}

func TestTinyContextShowsLessThanOnePercent(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)

	current(app).Input = "/tinyusage hello"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !current(app).Streaming && lastAssistantHas(app, "收到：/tinyusage hello")
	})
	if !tt.HasText("<1%") {
		t.Errorf("tiny context not shown as <1%%; texts: %q", tt.Texts())
	}
}
