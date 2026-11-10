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
	"github.com/reed/acp-gui/internal/session"
)

func TestChatWithFakeAgent(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	if !tt.HasText("Connected to Fake ACP agent") {
		t.Fatalf("not connected; texts: %q", tt.Texts())
	}

	// Send, stream, and finish.
	app.Input = "hello"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !app.Streaming && lastAssistantHas(app, "收到：hello")
	})
	if !tt.HasText("收到：hello") {
		t.Errorf("reply not rendered; texts: %q", tt.Texts())
	}
	if app.Title != "Fake session title" {
		t.Errorf("session title = %q", app.Title)
	}
	if app.Usage == nil || app.Usage.Used != 1200 {
		t.Errorf("usage = %+v", app.Usage)
	}
	if !tt.HasText("Fake session title") {
		t.Errorf("title not rendered; texts: %q", tt.Texts())
	}
	if !tt.HasText("context 1.2k / 10k") {
		t.Errorf("usage not rendered; texts: %q", tt.Texts())
	}

	// Stop sends session/cancel and the fake agent reports cancelled.
	app.Input = "/slow cancel me"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Streaming && lastAssistantHas(app, "收到")
	})
	if err := tt.Click("Stop"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !app.Streaming && app.LastStop == "cancelled"
	})
	if app.LastStop != "cancelled" {
		t.Errorf("last stop = %q", app.LastStop)
	}

	// A permission dialog blocks the ACP request until answered.
	app.Input = "/permission"
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
		return !app.Streaming && app.Permission == nil && lastAssistantHas(app, "permission granted")
	})
	if !tt.HasText("permission granted") {
		t.Errorf("permission reply not rendered; texts: %q", tt.Texts())
	}
}

func TestConnectRejectsRelativeWorkspace(t *testing.T) {
	app, tt := newFakeApp(t)
	app.Workspace = "relative/workspace"
	tt.Frame()
	if err := tt.Click("Connect"); err != nil {
		t.Fatal(err)
	}
	if app.WorkspaceError == "" {
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
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
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

func connect(t *testing.T, app *App, tt *mui.Tester) {
	t.Helper()
	tt.Frame()
	if err := tt.Click("Connect"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Connected })
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

func lastAssistantHas(app *App, want string) bool {
	for i := len(app.Messages) - 1; i >= 0; i-- {
		m := app.Messages[i]
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
		return !app.Streaming && lastAssistantHas(app, "收到：enter hello")
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
	waitFor(t, app, tt, func() bool { return len(app.Commands) == 2 })
	tt.Frame()
	for _, want := range []string{"Model", "Thinking", "Mode", "demo/small"} {
		if !tt.HasText(want) {
			t.Errorf("config UI missing %q; texts: %q", want, tt.Texts())
		}
	}

	if err := tt.Click("demo/small"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("demo/large"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		option := app.Info.ConfigOption("model")
		return option != nil && option.CurrentValue == "demo/large" && app.ConfigBusy == ""
	})
	tt.Frame()
	if err := tt.Click("Standard"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Plan"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Info.CurrentModeID == "plan" && app.ConfigBusy == "" })

	if err := tt.Click("Commands"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("/plan"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Input == "/plan " && !app.CommandsOpen })

	app.Input = "/plan"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return len(app.Plan) == 2 && app.PlanOpen && !app.Streaming })
}

func TestHistoryBeforeConnectLoadsSession(t *testing.T) {
	app, tt := newFakeApp(t)
	tt.Frame()
	if err := tt.Click("History"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.HistoryOpen && !app.HistoryLoading && len(app.HistorySessions) == 1
	})
	tt.Frame()
	if err := tt.Click("Open"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Connected && app.Info.SessionID == "fake-history" && !app.replaying && app.SessionBusy == ""
	})
	if !hasRoleText(app, RoleUser, "Question from history") {
		t.Errorf("user history missing; texts: %q", tt.Texts())
	}
	if !lastAssistantHas(app, "Answer from history") {
		t.Errorf("assistant history missing; texts: %q", tt.Texts())
	}
}

func TestCloseChatThenNewChat(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	tt.Frame()
	if err := tt.Click("Close chat"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Info.SessionID == "" && app.SessionBusy == "" })
	if !tt.HasText("No active session") {
		t.Errorf("no active session state missing; texts: %q", tt.Texts())
	}
	tt.Frame()
	if err := tt.Click("New chat"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Info.SessionID != "" && app.SessionBusy == "" })
	if app.Info.SessionID == "fake-session" {
		t.Errorf("new chat reused the closed session id: %q", app.Info.SessionID)
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
	app.Input = "look"
	app.Attachments = []Attachment{{
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
		return !app.Streaming && lastAssistantHas(app, "images=1")
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

	tt.Frame()
	if err := tt.Click("Connect"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Connected })
	app.Input = "hello"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !app.Streaming && lastAssistantHas(app, "mcp=1")
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
	for _, message := range app.Messages {
		if message.Role == role && strings.Contains(message.Text, want) {
			return true
		}
	}
	return false
}

func TestHistoryWhileConnectedLoadsAndResumes(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	tt.Frame()
	if err := tt.Click("History"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.HistoryOpen && !app.HistoryLoading && len(app.HistorySessions) == 1
	})
	tt.Frame()
	if err := tt.Click("Open"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Info.SessionID == "fake-history" && !app.replaying && app.SessionBusy == ""
	})
	if !lastAssistantHas(app, "Answer from history") {
		t.Errorf("history was not replayed; texts: %q", tt.Texts())
	}

	tt.Frame()
	if err := tt.Click("History"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.HistoryOpen && !app.HistoryLoading && len(app.HistorySessions) == 1
	})
	tt.Frame()
	if err := tt.Click("Resume"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Info.SessionID == "fake-history" && app.SessionBusy == "" && len(app.Messages) == 0
	})
}
