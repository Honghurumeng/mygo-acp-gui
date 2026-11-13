package ui

import (
	"path/filepath"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/reed/acp-gui/internal/settings"
)

// TestModelThinkingAndModePersistAndRestore drives the model panel, checks
// that the selection reaches settings.json, then starts a second app from the
// same data directory and expects the draft and the connected session to use
// it again.
func TestModelThinkingAndModePersistAndRestore(t *testing.T) {
	dataDir := t.TempDir()
	workspace := t.TempDir()
	app, tt := newSettingsApp(t, dataDir, workspace)
	connect(t, app, tt)

	tt.Frame()
	if err := tt.Click("demo/small"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
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
	current(app).ModelOpen = false
	tt.Frame()
	if err := tt.Click("Standard"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Plan"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return current(app).Info.CurrentModeID == "plan" && current(app).ConfigBusy == ""
	})

	file := loadPersistedSettings(t, dataDir)
	selection, ok := file.LastSelections["fake"]
	if !ok {
		t.Fatalf("lastSelections = %+v, missing fake", file.LastSelections)
	}
	if selection.ConfigOptions["model"] != "demo/large" || selection.ConfigOptions["thought_level"] != "high" {
		t.Errorf("saved config = %+v", selection.ConfigOptions)
	}
	if selection.Mode != "plan" {
		t.Errorf("saved mode = %q, want plan", selection.Mode)
	}

	agents := app.Agents
	app.Shutdown()
	app.Flush()

	restarted := New(Options{Version: "test", Workspace: workspace, DataDir: dataDir, Agents: agents})
	restarted.MD.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
	tt2 := mui.NewTester(restarted.View, 1060, 760)
	t.Cleanup(func() {
		restarted.Shutdown()
		restarted.Flush()
	})

	chat := current(restarted)
	if got := chat.PendingConfig["model"]; got != "demo/large" {
		t.Fatalf("restored model = %q, want demo/large", got)
	}
	if got := chat.PendingConfig["thought_level"]; got != "high" {
		t.Fatalf("restored thinking = %q, want high", got)
	}
	if got := chat.PendingMode; got != "plan" {
		t.Fatalf("restored mode = %q, want plan", got)
	}

	connect(t, restarted, tt2)
	waitFor(t, restarted, tt2, func() bool {
		ch := current(restarted)
		model := ch.Info.ConfigOption("model")
		thinking := ch.Info.ConfigOption("thought_level")
		return ch.Connected && ch.ConfigBusy == "" &&
			model != nil && model.CurrentValue == "demo/large" &&
			thinking != nil && thinking.CurrentValue == "high" &&
			ch.Info.CurrentModeID == "plan"
	})
}

// TestAgentSwitchRestoresSavedSelection checks the per-agent part: switching
// a draft to another agent brings that agent's own last model, thinking and
// mode instead of leaving the previous agent's picks behind.
func TestAgentSwitchRestoresSavedSelection(t *testing.T) {
	dataDir := t.TempDir()
	store := settings.Open(filepath.Join(dataDir, "settings.json"))
	err := store.Save(settings.File{
		Version: settings.CurrentVersion,
		LastSelections: map[string]settings.Selection{
			"other": {
				ConfigOptions: map[string]string{"model": "demo/reasoning", "thought_level": "xhigh"},
				Mode:          "plan",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	app, tt := newSettingsApp(t, dataDir, t.TempDir())
	tt.Frame()
	chat := current(app)
	// Hide the sidebar so "other-agent" is unique to the model panel; the
	// sidebar also lists every agent's status.
	app.SidebarOpen = false
	chat.ModelOpen = true
	tt.Frame()
	if err := tt.Click("other-agent"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := chat.PendingConfig["model"]; got != "demo/reasoning" {
		t.Errorf("pending model = %q, want demo/reasoning", got)
	}
	if got := chat.PendingConfig["thought_level"]; got != "xhigh" {
		t.Errorf("pending thinking = %q, want xhigh", got)
	}
	if chat.PendingMode != "plan" {
		t.Errorf("pending mode = %q, want plan", chat.PendingMode)
	}
}

// TestStaleSavedSelectionIsSkipped verifies that a model the agent no longer
// advertises is dropped at Connect instead of being sent blind.
func TestStaleSavedSelectionIsSkipped(t *testing.T) {
	dataDir := t.TempDir()
	store := settings.Open(filepath.Join(dataDir, "settings.json"))
	err := store.Save(settings.File{
		Version: settings.CurrentVersion,
		LastSelections: map[string]settings.Selection{
			"fake": {ConfigOptions: map[string]string{"model": "gone/model"}, Mode: "gone-mode"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	app, tt := newSettingsApp(t, dataDir, t.TempDir())
	if got := current(app).PendingConfig["model"]; got != "gone/model" {
		t.Fatalf("pending model = %q", got)
	}
	connect(t, app, tt)
	ch := current(app)
	option := ch.Info.ConfigOption("model")
	if option == nil || option.CurrentValue != "demo/small" {
		t.Fatalf("model = %+v, want the agent default demo/small", option)
	}
	if ch.Info.CurrentModeID != "default" {
		t.Errorf("mode = %q, want the agent default", ch.Info.CurrentModeID)
	}
}
