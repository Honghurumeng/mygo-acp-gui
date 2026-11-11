package ui

import (
	"encoding/base64"
	"errors"
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
	"github.com/reed/acp-gui/internal/session"
)

func TestChatWithFakeAgent(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	if !tt.HasText("Connected to Fake ACP agent") {
		t.Fatalf("not connected; texts: %q", tt.Texts())
	}

	// Send, stream, and finish.
	current(app).Input = "hello"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !current(app).Streaming && lastAssistantHas(app, "收到：hello")
	})
	if !tt.HasText("收到：hello") {
		t.Errorf("reply not rendered; texts: %q", tt.Texts())
	}
	if current(app).Title != "Fake session title" {
		t.Errorf("session title = %q", current(app).Title)
	}
	if current(app).Usage == nil || current(app).Usage.Used != 1200 {
		t.Errorf("usage = %+v", current(app).Usage)
	}
	if !tt.HasText("Fake session title") {
		t.Errorf("title not rendered; texts: %q", tt.Texts())
	}
	if !tt.HasText("12%") {
		t.Errorf("usage ring not rendered; texts: %q", tt.Texts())
	}

	// Stop sends session/cancel and the fake agent reports cancelled.
	current(app).Input = "/slow cancel me"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return current(app).Streaming && lastAssistantHas(app, "收到")
	})
	if err := tt.Click("Stop"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !current(app).Streaming && current(app).LastStop == "cancelled"
	})
	if current(app).LastStop != "cancelled" {
		t.Errorf("last stop = %q", current(app).LastStop)
	}

	// A permission dialog blocks the ACP request until answered.
	current(app).Input = "/permission"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Permission != nil })
	tt.Frame()
	if !tt.HasText("Permission required") || !tt.HasText("Allow once") {
		t.Fatalf("permission dialog missing; texts: %q", tt.Texts())
	}
	if err := tt.Click("Allow once"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !current(app).Streaming && app.Permission == nil && lastAssistantHas(app, "permission granted")
	})
	if !tt.HasText("permission granted") {
		t.Errorf("permission reply not rendered; texts: %q", tt.Texts())
	}
}

func TestConnectRejectsRelativeWorkspace(t *testing.T) {
	app, tt := newFakeApp(t)
	current(app).Workspace = "relative/workspace"
	app.Connect()
	tt.Frame()
	chat := app.activeChat()
	if chat == nil || chat.WorkspaceError == "" {
		t.Fatal("relative workspace was accepted")
	}
	if !tt.HasText("workspace must be an absolute path") {
		t.Errorf("workspace error not shown; texts: %q", tt.Texts())
	}
}

func newFakeApp(t *testing.T) (*App, *mui.Tester) {
	t.Helper()
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
	seedSessionArchive(t, dataDir, archive.Session{
		AgentID:   "fake",
		SessionID: "fake-history",
		Cwd:       workspace,
		Title:     "Fake history",
		UpdatedAt: "2026-01-01T00:00:00Z",
		Show:      true,
	})
	app := New(Options{
		Version:   "test",
		Workspace: workspace,
		DataDir:   dataDir,
		Agents: []agent.LaunchSpec{{
			ID:      "fake",
			Name:    "fake-agent",
			Command: node,
			Args:    []string{script},
			Env:     os.Environ(),
			Source:  agent.SourceBuiltin,
		}},
	})
	app.MD.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
	tt := mui.NewTester(app.View, 1060, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	return app, tt
}

func seedSessionArchive(t *testing.T, dataDir string, sessions ...archive.Session) {
	t.Helper()
	store := archive.Open(filepath.Join(dataDir, archiveFileName))
	if err := store.Save(archive.File{Version: archive.CurrentVersion, Sessions: sessions}); err != nil {
		t.Fatal(err)
	}
}

func connect(t *testing.T, app *App, tt *mui.Tester) {
	t.Helper()
	app.Connect()
	waitFor(t, app, tt, func() bool { return current(app).Connected })
}

func current(app *App) *Chat {
	if chat := app.activeChat(); chat != nil {
		return chat
	}
	return &Chat{}
}

func waitFor(t *testing.T, app *App, tt *mui.Tester, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		app.Flush()
		tt.Frame()
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for UI state; texts: %q", tt.Texts())
}

func waitSavedSessions(t *testing.T, app *App, tt *mui.Tester, count int) {
	t.Helper()
	waitFor(t, app, tt, func() bool {
		// SavedSessions includes open and hidden records; the sidebar shows
		// only the rows for the current workspace that are not open/tabs.
		return !app.SavedLoading && len(app.savedSessionsFor(app.Workspace)) == count
	})
}

func lastAssistantHas(app *App, want string) bool {
	chat := current(app)
	for i := len(chat.Messages) - 1; i >= 0; i-- {
		m := chat.Messages[i]
		if m.Role == RoleAssistant {
			return strings.Contains(m.Text, want)
		}
	}
	return false
}

func TestComposerEnterSends(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	tt.Frame()
	if err := tt.Click("Message"); err != nil {
		t.Fatal(err)
	}
	tt.Type("enter hello")
	tt.Key(0, mui.KeyEnter)
	waitFor(t, app, tt, func() bool {
		return !current(app).Streaming && lastAssistantHas(app, "收到：enter hello")
	})
}

func TestConnectErrorShowsLoginHint(t *testing.T) {
	err := &session.AuthRequiredError{
		Methods: []acp.AuthMethod{{ID: "command-code-cli", Name: "Command Code CLI login"}},
		Err:     errors.New("session/new: -32000"),
	}
	got := connectError(err, agent.LaunchSpec{ID: agent.CommandCodeID, LoginHint: "command-code login"})
	for _, want := range []string{"Authentication required", "Command Code CLI login", "command-code login"} {
		if !strings.Contains(got, want) {
			t.Errorf("connectError = %q, missing %q", got, want)
		}
	}
}

func TestSessionConfigModeAndCommands(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	waitFor(t, app, tt, func() bool { return len(current(app).Commands) == 2 })
	tt.Frame()

	// The model chip opens the agent/provider/model/thinking panel.
	if err := tt.Click("demo/small"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"Agent", "Provider", "Model", "Thinking", "demo/large"} {
		if !tt.HasText(want) {
			t.Errorf("model panel missing %q; texts: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("demo/large"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		option := current(app).Info.ConfigOption("model")
		return option != nil && option.CurrentValue == "demo/large" && current(app).ConfigBusy == ""
	})
	if err := tt.Click("high"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		option := current(app).Info.ConfigOption("thought_level")
		return option != nil && option.CurrentValue == "high" && current(app).ConfigBusy == ""
	})

	// Mode has its own chip; clicking the current mode opens its menu.
	current(app).ModelOpen = false
	tt.Frame()
	if err := tt.Click("Standard"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Plan"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return current(app).Info.CurrentModeID == "plan" && current(app).ConfigBusy == "" })

	// Typing / opens the command palette instead of a Commands button.
	current(app).Input = "/"
	tt.Frame()
	if err := tt.Click("/plan"); err != nil {
		t.Fatal(err)
	}
	if current(app).Input != "/plan " {
		t.Fatalf("command insert = %q", current(app).Input)
	}

	current(app).Input = "/plan"
	tt.Frame()
	current(app).Commands = nil
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return len(current(app).Plan) == 2 && current(app).PlanOpen && !current(app).Streaming })
}

func TestSavedSessionsBeforeConnectOpensHistory(t *testing.T) {
	app, tt := newFakeApp(t)
	waitSavedSessions(t, app, tt, 1)
	tt.Frame()
	if !tt.HasText("Fake history") {
		t.Fatalf("saved session not shown in sidebar; texts: %q", tt.Texts())
	}
	if err := tt.Click("Open saved session: Fake history"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return current(app).Connected && current(app).Info.SessionID == "fake-history" && !current(app).replaying &&
			current(app).Busy == "" && hasRoleText(app, RoleUser, "Question from history") &&
			lastAssistantHas(app, "Answer from history")
	})
}

func TestCloseChatThenNewChat(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	tt.Frame()
	app.closeActiveSession()
	waitFor(t, app, tt, func() bool { return current(app).Info.SessionID == "" && current(app).Busy == "" })
	if !tt.HasText("No active session") {
		t.Errorf("no active session state missing; texts: %q", tt.Texts())
	}
	app.newSession()
	waitFor(t, app, tt, func() bool { return current(app).Info.SessionID != "" && current(app).Busy == "" })
	if current(app).Info.SessionID == "fake-session" {
		t.Errorf("new chat reused the closed session id: %q", current(app).Info.SessionID)
	}
	if app.archiveIndex("fake", current(app).Info.SessionID) < 0 {
		t.Errorf("new chat was not recorded in the archive: %q", current(app).Info.SessionID)
	}
}

func TestSendImageAttachment(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	const pngBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	png, err := base64.StdEncoding.DecodeString(pngBase64)
	if err != nil {
		t.Fatal(err)
	}
	bitmap, err := mui.DecodeBitmap(png)
	if err != nil {
		t.Fatal(err)
	}
	current(app).Input = "look"
	current(app).Attachments = []Attachment{{
		Name:     "pixel.png",
		MimeType: "image/png",
		Data:     pngBase64,
		Bitmap:   bitmap,
	}}
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !current(app).Streaming && lastAssistantHas(app, "images=1")
	})
}

func TestMCPDialogAndPrompt(t *testing.T) {
	app, tt := newFakeApp(t)
	app.openMCPSettings()
	app.MCPText = `[{"name":"demo","command":"true"}]`
	tt.Frame()
	if err := tt.Click("Apply"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return !app.SettingsOpen && len(app.MCPServers) == 1 })

	app.Connect()
	waitFor(t, app, tt, func() bool { return current(app).Connected })
	current(app).Input = "hello"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !current(app).Streaming && lastAssistantHas(app, "mcp=1")
	})
}

func TestParseMCPServersRejectsInvalid(t *testing.T) {
	if _, err := parseMCPServers(`[{"name":"x"}]`); err == nil {
		t.Error("stdio server without command was accepted")
	}
	if _, err := parseMCPServers(`[{"type":"http","name":"x"}]`); err == nil {
		t.Error("http server without url was accepted")
	}
	if got, err := parseMCPServers(`[{"name":"x","command":"true"}]`); err != nil || len(got) != 1 {
		t.Errorf("valid server: %v %v", got, err)
	}
}

func hasRoleText(app *App, role MessageRole, want string) bool {
	chat := current(app)
	for _, message := range chat.Messages {
		if message.Role == role && strings.Contains(message.Text, want) {
			return true
		}
	}
	return false
}

func TestSavedSessionsOpenWithHistory(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	waitSavedSessions(t, app, tt, 1)

	// Clicking the row opens with session/load, so the agent replays the
	// conversation into the empty tab.
	tt.Frame()
	if err := tt.Click("Open saved session: Fake history"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return current(app).Info.SessionID == "fake-history" && !current(app).replaying &&
			current(app).Busy == "" &&
			hasRoleText(app, RoleUser, "Question from history") &&
			lastAssistantHas(app, "Answer from history")
	})
	for _, message := range current(app).Messages {
		if !message.Done {
			t.Fatalf("replayed message is not done: %+v", message)
		}
	}

	// Closing the ACP session makes the saved row available again. It still
	// opens through session/load, replaying history into the existing process.
	app.closeActiveSession()
	waitFor(t, app, tt, func() bool {
		return current(app).Info.SessionID == "" && current(app).Busy == ""
	})
	tt.Frame()
	if err := tt.Click("Open saved session: Fake history"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return current(app).Info.SessionID == "fake-history" && !current(app).replaying &&
			current(app).Busy == "" && hasRoleText(app, RoleUser, "Question from history") &&
			lastAssistantHas(app, "Answer from history")
	})
}
