package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	mui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/reed/acp-gui/internal/acp"
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
	// session/load replays reasoning as agent_thought_chunk; it must land on the
	// same assistant row as the replayed answer rather than being dropped.
	thought, ok := lastAssistantThought(app)
	if !ok || !strings.Contains(thought, "Reasoning from history") {
		t.Fatalf("replayed thought = %q (found=%v)", thought, ok)
	}
}

func TestSavedSessionsSyncWithoutLiveChat(t *testing.T) {
	app, tt := newFakeApp(t)
	// Start from an empty archive so the row can only come from session/list.
	app.Archived = nil
	app.syncArchiveRows()
	app.savedSynced = nil

	app.syncSavedSessionsOnce("fake")
	waitFor(t, app, tt, func() bool {
		return !app.SavedLoading && len(app.savedSessionsFor(app.Workspace)) == 1
	})
	if index := app.archiveIndex("fake", "fake-history"); index < 0 {
		t.Fatalf("history was not imported without a live chat: %+v", app.Archived)
	}
}

func TestImportIgnoresUnaddedWorkspaces(t *testing.T) {
	app, _ := newFakeApp(t)
	before := len(app.Workspaces)

	app.mergeArchivedSessions("fake", []acp.SessionSummary{
		{SessionID: "added", Cwd: app.Workspace, Title: "Added"},
		{SessionID: "other", Cwd: "/definitely/not/added", Title: "Other"},
	})

	if app.archiveIndex("fake", "added") < 0 {
		t.Errorf("session in an added workspace was dropped: %+v", app.Archived)
	}
	if app.archiveIndex("fake", "other") >= 0 {
		t.Errorf("session outside the added workspaces was imported: %+v", app.Archived)
	}
	if got := len(app.Workspaces); got != before {
		t.Errorf("import auto-added workspaces: %d -> %d (%+v)", before, got, app.Workspaces)
	}
}

func TestWorkspaceSyncImportsOnlyThatWorkspace(t *testing.T) {
	app, tt := newFakeApp(t)
	// Drop the seeded row so only the new workspace can contribute.
	app.Archived = nil
	app.syncArchiveRows()

	other := t.TempDir()
	app.showWorkspace(other)
	app.syncWorkspaceSessions(other)

	waitFor(t, app, tt, func() bool {
		return !app.SavedLoading && len(app.savedSessionsFor(other)) == 1
	})
	if rows := app.savedSessionsFor(app.Workspace); len(rows) != 0 {
		t.Errorf("workspace sync leaked rows into another workspace: %+v", rows)
	}
	if row := app.SavedSessions[0]; canonicalWorkspace(row.Cwd) != canonicalWorkspace(other) {
		t.Errorf("imported cwd = %q, want %q", row.Cwd, other)
	}
}

func TestLoadArchiveCleansAttachmentPathTitles(t *testing.T) {
	store := archive.Open(filepath.Join(t.TempDir(), archiveFileName))
	err := store.Save(archive.File{
		Version: archive.CurrentVersion,
		Sessions: []archive.Session{
			{
				AgentID:   "pi",
				SessionID: "path-and-text",
				Cwd:       "/work/mine",
				Title:     "/var/folders/a/one.png 看看这个项目",
				Show:      true,
			},
			{
				AgentID:   "pi",
				SessionID: "path-only",
				Cwd:       "/work/mine",
				Title:     "/var/folders/a/two.png",
				Show:      true,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	app := &App{
		Workspace:  "/work/mine",
		Workspaces: []string{"/work/mine"},
		archive:    store,
	}
	app.loadArchive()

	if got := app.Archived[0].Title; got != "看看这个项目" {
		t.Errorf("path + text title = %q", got)
	}
	if got := app.Archived[1].Title; got != imageSessionTitle {
		t.Errorf("path-only title = %q", got)
	}
	if got := savedSessionTitle(app.SavedSessions[0]); got != "看看这个项目" {
		t.Errorf("sidebar title = %q", got)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Sessions[0].Title != "看看这个项目" || loaded.Sessions[1].Title != imageSessionTitle {
		t.Errorf("cleaned titles were not persisted: %+v", loaded.Sessions)
	}
}

func TestSentPromptTimePersistsInArchive(t *testing.T) {
	app, tt := newFakeApp(t)
	chat := connectTab(t, app, tt)
	sendTo(t, app, tt, chat, "timed question")
	waitFor(t, app, tt, func() bool {
		return !chat.Streaming && chatAssistantHas(chat, "收到：timed question")
	})

	index := app.archiveIndex("fake", chat.Info.SessionID)
	if index < 0 {
		t.Fatalf("session was not archived: %+v", app.Archived)
	}
	var user *Message
	for _, message := range chat.Messages {
		if message.Role == RoleUser && message.Text == "timed question" {
			user = message
			break
		}
	}
	if user == nil || user.SentAt.IsZero() {
		t.Fatalf("sent prompt has no time: %+v", user)
	}
	turns := app.Archived[index].UserTurns
	if len(turns) != 1 || turns[0].Number != 1 {
		t.Fatalf("user turns = %+v, want one turn", turns)
	}
	parsed, err := time.Parse(time.RFC3339Nano, turns[0].SentAt)
	if err != nil {
		t.Fatalf("sent time %q: %v", turns[0].SentAt, err)
	}
	if !parsed.Equal(user.SentAt) {
		t.Errorf("archived time = %v, message time = %v", parsed, user.SentAt)
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
	if persisted == nil || len(persisted.UserTurns) != 1 ||
		persisted.UserTurns[0].SentAt != turns[0].SentAt {
		t.Fatalf("persisted user turns = %+v", loaded.Sessions)
	}
}

func TestTurnRuntimePersistsAndDisplays(t *testing.T) {
	app, tt := newFakeApp(t)
	chat := connectTab(t, app, tt)
	sendTo(t, app, tt, chat, "timed runtime")
	waitFor(t, app, tt, func() bool {
		return !chat.Streaming && chatAssistantHas(chat, "收到：timed runtime")
	})

	var summary *Message
	for _, message := range chat.Messages {
		if message.Role == RoleTurn {
			summary = message
		}
	}
	if summary == nil || summary.Elapsed <= 0 {
		t.Fatalf("turn summary = %+v, want a positive runtime", summary)
	}
	if want := "Took " + formatElapsed(summary.Elapsed); !tt.HasText(want) {
		t.Errorf("runtime %q not rendered; texts: %q", want, tt.Texts())
	}

	index := app.archiveIndex("fake", chat.Info.SessionID)
	if index < 0 {
		t.Fatalf("session was not archived: %+v", app.Archived)
	}
	turns := app.Archived[index].UserTurns
	if len(turns) != 1 || turns[0].DurationMs <= 0 {
		t.Fatalf("user turns = %+v, want one persisted runtime", turns)
	}
	if got, want := time.Duration(turns[0].DurationMs)*time.Millisecond, summary.Elapsed.Truncate(time.Millisecond); got != want {
		t.Errorf("archived runtime = %v, message runtime = %v", got, want)
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
	if persisted == nil || len(persisted.UserTurns) != 1 ||
		persisted.UserTurns[0].DurationMs != turns[0].DurationMs {
		t.Fatalf("persisted user turns = %+v", loaded.Sessions)
	}
}

func TestReplayUsesPersistedUserTurnTimeAndRuntime(t *testing.T) {
	app, tt := newFakeApp(t)
	waitSavedSessions(t, app, tt, 1)
	stamp := "2020-11-11T09:41:00Z"
	index := app.archiveIndex("fake", "fake-history")
	if index < 0 {
		t.Fatal("fake history is missing from the archive")
	}
	app.Archived[index].UserTurns = []archive.UserTurn{{Number: 1, SentAt: stamp, DurationMs: 83000}}
	app.saveArchive()

	tt.Frame()
	if err := tt.Click("Open saved session: Fake history"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		chat := app.activeChat()
		return chat != nil && chat.Connected && chat.Info.SessionID == "fake-history" &&
			!chat.replaying && lastAssistantHas(app, "Answer from history")
	})
	chat := app.activeChat()
	var user *Message
	for _, message := range chat.Messages {
		if message.Role == RoleUser && strings.Contains(message.Text, "Question from history") {
			user = message
			break
		}
	}
	if user == nil || user.SentAt.IsZero() {
		t.Fatalf("replayed prompt has no time: %+v", user)
	}
	parsed, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		t.Fatal(err)
	}
	if !user.SentAt.Equal(parsed) {
		t.Errorf("replayed time = %v, want %v", user.SentAt, parsed)
	}

	var summary *Message
	for _, message := range chat.Messages {
		if message.Role == RoleTurn {
			summary = message
		}
	}
	if summary == nil || summary.Elapsed != 83*time.Second {
		t.Fatalf("replayed runtime = %+v, want 1m 23s", summary)
	}
	if want := "Took 1m 23s"; !tt.HasText(want) {
		t.Errorf("runtime %q missing after replay; texts: %q", want, tt.Texts())
	}

	tt.Frame()
	if err := tt.Click("Jump"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if want := formatJumpTime(parsed, time.Now()); !tt.HasText(want) {
		t.Errorf("send time %q missing from jump dialog; texts: %q", want, tt.Texts())
	}
}

func TestLoadArchiveDoesNotAddWorkspaces(t *testing.T) {
	store := archive.Open(filepath.Join(t.TempDir(), archiveFileName))
	err := store.Save(archive.File{
		Version: archive.CurrentVersion,
		Sessions: []archive.Session{{
			AgentID:   "fake",
			SessionID: "outside",
			Cwd:       "/definitely/not/added",
			Show:      true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	app := &App{
		Workspace:  "/work/mine",
		Workspaces: []string{"/work/mine"},
		archive:    store,
	}
	app.loadArchive()

	if len(app.Workspaces) != 1 || app.Workspaces[0] != "/work/mine" {
		t.Fatalf("archive auto-added workspaces: %+v", app.Workspaces)
	}
	if len(app.SavedSessions) != 1 {
		t.Fatalf("archived row was dropped from the index: %+v", app.SavedSessions)
	}
}
