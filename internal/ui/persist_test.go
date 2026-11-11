package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/settings"
)

// newSettingsApp builds two injected agents and a data directory so the
// persistence paths can be exercised without discovery or a real agent.
func newSettingsApp(t *testing.T, dataDir, workspace string) (*App, *mui.Tester) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	script := filepath.Join(filepath.Dir(file), "..", "..", "scripts", "fake-agent.mjs")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("fake agent: %v", err)
	}
	agents := []agent.LaunchSpec{
		{
			ID:      "fake",
			Name:    "fake-agent",
			Command: node,
			Args:    []string{script},
			Env:     os.Environ(),
			Source:  agent.SourceBuiltin,
		},
		{
			ID:      "other",
			Name:    "other-agent",
			Command: node,
			Args:    []string{script},
			Env:     os.Environ(),
			Source:  agent.SourceBuiltin,
		},
	}
	app := New(Options{
		Version:   "test",
		Workspace: workspace,
		DataDir:   dataDir,
		Agents:    agents,
	})
	app.MD.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
	tt := mui.NewTester(app.View, 1060, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	return app, tt
}

func loadPersistedSettings(t *testing.T, dataDir string) settings.File {
	t.Helper()
	file, err := settings.Open(filepath.Join(dataDir, "settings.json")).Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	return file
}

func TestSettingsSavedOnConnectAndMCPApply(t *testing.T) {
	dataDir := t.TempDir()
	workspace := t.TempDir()
	app, tt := newSettingsApp(t, dataDir, workspace)
	connect(t, app, tt)

	file := loadPersistedSettings(t, dataDir)
	if file.LastAgent != "fake" {
		t.Errorf("lastAgent = %q, want fake", file.LastAgent)
	}
	if file.LastWorkspace != workspace {
		t.Errorf("lastWorkspace = %q, want %q", file.LastWorkspace, workspace)
	}

	app.openMCPSettings()
	app.MCPText = `[{"name":"demo","command":"true"}]`
	tt.Frame()
	if err := tt.Click("Apply"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return !app.SettingsOpen && len(app.MCPServers) == 1 })

	file = loadPersistedSettings(t, dataDir)
	if len(file.MCPServers) != 1 {
		t.Fatalf("persisted MCP servers = %+v", file.MCPServers)
	}
	server, ok := file.MCPServers[0].(map[string]any)
	if !ok || server["name"] != "demo" {
		t.Errorf("persisted MCP server = %#v", file.MCPServers[0])
	}
}

func TestSettingsRestoreOnStart(t *testing.T) {
	dataDir := t.TempDir()
	workspace := t.TempDir()
	store := settings.Open(filepath.Join(dataDir, "settings.json"))
	if err := store.Save(settings.File{
		Version:       settings.CurrentVersion,
		LastAgent:     "other",
		LastWorkspace: workspace,
		MCPServers:    []any{map[string]any{"name": "demo", "command": "true"}},
	}); err != nil {
		t.Fatal(err)
	}

	app, _ := newSettingsApp(t, dataDir, "")
	if app.Workspace != workspace {
		t.Errorf("workspace = %q, want restored %q", app.Workspace, workspace)
	}
	if app.AgentIndex != 1 || app.Agents[app.AgentIndex].ID != "other" {
		t.Errorf("agent index = %d (%+v), want other", app.AgentIndex, app.Agents)
	}
	if len(app.MCPServers) != 1 {
		t.Errorf("MCP servers = %+v", app.MCPServers)
	}

	chat := app.newChatTab()
	if chat.Workspace != workspace {
		t.Errorf("new chat workspace = %q", chat.Workspace)
	}
	if chat.Agent.ID != "other" {
		t.Errorf("new chat agent = %q", chat.Agent.ID)
	}
	if len(chat.MCPServers) != 1 {
		t.Errorf("new chat MCP servers = %+v", chat.MCPServers)
	}
}

func TestExplicitWorkspaceWinsOverSaved(t *testing.T) {
	dataDir := t.TempDir()
	saved := t.TempDir()
	store := settings.Open(filepath.Join(dataDir, "settings.json"))
	if err := store.Save(settings.File{Version: settings.CurrentVersion, LastWorkspace: saved}); err != nil {
		t.Fatal(err)
	}
	explicit := t.TempDir()
	app, _ := newSettingsApp(t, dataDir, explicit)
	if app.Workspace != explicit {
		t.Errorf("workspace = %q, want explicit %q", app.Workspace, explicit)
	}
}

func TestShutdownPersistsDraftState(t *testing.T) {
	dataDir := t.TempDir()
	app, _ := newSettingsApp(t, dataDir, "")
	chat := app.newChatTab()
	workspace := t.TempDir()
	chat.Workspace = workspace
	chat.Agent = app.Agents[1]
	app.Shutdown()

	file := loadPersistedSettings(t, dataDir)
	if file.LastAgent != "other" {
		t.Errorf("lastAgent = %q", file.LastAgent)
	}
	if file.LastWorkspace != workspace {
		t.Errorf("lastWorkspace = %q, want %q", file.LastWorkspace, workspace)
	}
}

func TestCorruptSettingsDoNotBlockStartup(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	app, _ := newSettingsApp(t, dataDir, "")
	if !strings.Contains(app.SettingsWarning, "invalid JSON") {
		t.Errorf("warning = %q", app.SettingsWarning)
	}
	if len(app.Agents) != 2 || app.AgentIndex != 0 {
		t.Errorf("startup state = %d agents, index %d", len(app.Agents), app.AgentIndex)
	}
}
