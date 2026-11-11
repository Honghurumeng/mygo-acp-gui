package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticsShowsStderrAndWritesLog(t *testing.T) {
	dataDir := t.TempDir()
	app, tt := newSettingsApp(t, dataDir, t.TempDir())
	connect(t, app, tt)
	waitFor(t, app, tt, func() bool {
		return strings.Contains(current(app).Stderr, "fake-agent ready")
	})

	tt.Frame()
	if err := tt.Click("Logs"); err != nil {
		t.Fatal(err)
	}
	if !app.DiagOpen {
		t.Fatal("diagnostics dialog did not open")
	}
	tt.Frame()
	for _, want := range []string{"Diagnostics", "Reveal log", "stderr"} {
		if !tt.HasText(want) {
			t.Errorf("diagnostics missing %q; texts: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("Close"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.DiagOpen {
		t.Error("diagnostics dialog did not close")
	}

	path := filepath.Join(dataDir, "logs", "fake.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	for _, want := range []string{"=== ", "command:", "fake-agent ready"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("log missing %q:\n%s", want, data)
		}
	}

	// The in-memory tail is bounded even if the agent is chatty.
	chat := current(app)
	chat.Stderr = strings.Repeat("x", stderrTailLimit+100)
	app.appendStderr(chat, "tail")
	if len(chat.Stderr) != stderrTailLimit {
		t.Errorf("stderr tail = %d bytes, want %d", len(chat.Stderr), stderrTailLimit)
	}
	if !strings.HasSuffix(chat.Stderr, "tail") {
		t.Errorf("stderr tail lost the newest bytes")
	}
}

func TestDiagnosticsWithoutChatShowsDiscoveryStatuses(t *testing.T) {
	app := New(Options{
		Version: "test",
		Agents:  nil,
	})
	app.AgentStatuses = []AgentStatus{{ID: "pi", Name: "pi", Status: AgentMissing, Detail: "node not found"}}
	app.openDiagnostics()
	if app.DiagChat != nil {
		t.Error("diagnostics picked a chat when none is open")
	}
	if !app.DiagOpen {
		t.Error("diagnostics did not open")
	}
	app.Shutdown()
}
