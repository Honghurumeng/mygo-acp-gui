package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/archive"
)

func TestSavedSessionRenamePersistsAcrossRestart(t *testing.T) {
	app, tt := newFakeApp(t)
	waitSavedSessions(t, app, tt, 1)
	tt.Frame()
	if err := tt.Click("Saved session actions for Fake history"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Rename"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	app.RenameText = "Pinned history"
	tt.Frame()
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		rows := app.savedSessionsFor(app.Workspace)
		return len(rows) == 1 && savedSessionTitle(rows[0]) == "Pinned history"
	})

	index := app.archiveIndex("fake", "fake-history")
	if index < 0 {
		t.Fatal("archived session disappeared")
	}
	record := app.Archived[index]
	if record.ShowName != "Pinned history" {
		t.Errorf("showName = %q", record.ShowName)
	}
	if record.Title != "Fake history" {
		t.Errorf("agent title was overwritten: %q", record.Title)
	}
	dataDir := filepath.Dir(app.archive.Path())
	loaded, err := archive.Open(app.archive.Path()).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Sessions) != 1 || loaded.Sessions[0].ShowName != "Pinned history" {
		t.Fatalf("persisted archive = %+v", loaded)
	}

	restarted := New(Options{
		Version:   "test",
		Workspace: app.Workspace,
		DataDir:   dataDir,
		Agents:    app.Agents,
	})
	restarted.MD.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
	tt2 := mui.NewTester(restarted.View, 1060, 760)
	t.Cleanup(func() {
		restarted.Shutdown()
		restarted.Flush()
	})
	tt2.Frame()
	rows := restarted.savedSessionsFor(restarted.Workspace)
	if len(rows) != 1 || savedSessionTitle(rows[0]) != "Pinned history" {
		t.Fatalf("restarted rows = %+v", rows)
	}
	if !tt2.HasText("Pinned history") {
		t.Errorf("renamed title not shown after restart; texts: %q", tt2.Texts())
	}
}

func TestDeleteSavedSessionIsSoftAndSurvivesSync(t *testing.T) {
	app, tt := newFakeApp(t)
	waitSavedSessions(t, app, tt, 1)
	tt.Frame()
	if err := tt.Click("Saved session actions for Fake history"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Delete"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return len(app.savedSessionsFor(app.Workspace)) == 0 && app.hiddenArchivedCount() == 1
	})

	index := app.archiveIndex("fake", "fake-history")
	if index < 0 || app.Archived[index].Show {
		t.Fatalf("archive record = %+v", app.Archived)
	}
	loaded, err := archive.Open(app.archive.Path()).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Sessions) != 1 || loaded.Sessions[0].Show {
		t.Fatalf("persisted archive = %+v", loaded)
	}

	// The agent still lists this session. A manual sync must not resurrect it.
	app.refreshSavedSessions()
	waitFor(t, app, tt, func() bool {
		return !app.SavedLoading && len(app.SavedSessions) > 0
	})
	if len(app.savedSessionsFor(app.Workspace)) != 0 {
		t.Fatalf("deleted session was resurrected by sync: %+v", app.SavedSessions)
	}
	if app.hiddenArchivedCount() != 1 {
		t.Errorf("hidden count = %d, want 1", app.hiddenArchivedCount())
	}

	// The hidden toggle exposes the record and Restore makes it visible again.
	tt.Frame()
	if err := tt.Click("Show deleted (1)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Fake history") {
		t.Fatalf("deleted row not shown; texts: %q", tt.Texts())
	}
	if err := tt.Click("Saved session actions for Fake history"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Restore"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.hiddenArchivedCount() == 0 && len(app.savedSessionsFor(app.Workspace)) == 1
	})
}

func TestDeleteOpenChatSoftDeletesArchivedSession(t *testing.T) {
	app, tt := newFakeApp(t)
	chat := connectTab(t, app, tt)
	waitFor(t, app, tt, func() bool {
		return chat.Info.SessionID != "" && app.archiveIndex("fake", chat.Info.SessionID) >= 0
	})
	sessionID := chat.Info.SessionID

	tt.Frame()
	if err := tt.Click("Conversation actions for " + chat.name()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Delete"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.chatIndex(chat) < 0
	})
	index := app.archiveIndex("fake", sessionID)
	if index < 0 {
		t.Fatal("deleted chat was not written to the archive")
	}
	if app.Archived[index].Show {
		t.Fatal("deleted chat stayed visible in the archive")
	}
}

func TestSavedSessionUsesRecordedAgent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-agent integration test is Unix-only for now")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	node, err = filepath.Abs(node)
	if err != nil {
		t.Fatal(err)
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	script := filepath.Join(filepath.Dir(file), "..", "..", "scripts", "fake-agent.mjs")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("fake agent: %v", err)
	}
	workspace := t.TempDir()
	dataDir := t.TempDir()
	agents := []agent.LaunchSpec{
		{ID: "fake", Name: "fake-agent", Command: node, Args: []string{script}, Env: os.Environ(), Source: agent.SourceBuiltin},
		{ID: "other", Name: "other-agent", Command: node, Args: []string{script}, Env: os.Environ(), Source: agent.SourceBuiltin},
	}
	seedSessionArchive(t, dataDir, archive.Session{
		AgentID:   "other",
		SessionID: "fake-history",
		Cwd:       workspace,
		Title:     "Other agent session",
		UpdatedAt: "2026-01-01T00:00:00Z",
		Show:      true,
	})
	app := New(Options{Version: "test", Workspace: workspace, DataDir: dataDir, Agents: agents})
	app.MD.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
	tt := mui.NewTester(app.View, 1060, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	waitSavedSessions(t, app, tt, 1)
	tt.Frame()
	if err := tt.Click("Open saved session: Other agent session"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		chat := app.activeChat()
		return chat != nil && chat.Connected && chat.Info.SessionID == "fake-history" && chat.Busy == "" &&
			!chat.replaying && lastAssistantHas(app, "Answer from history")
	})
	if got := app.activeChat().Agent.ID; got != "other" {
		t.Fatalf("opened with agent %q, want other", got)
	}
}
