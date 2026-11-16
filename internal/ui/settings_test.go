package ui

import (
	"strings"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/codexprovider"
	"github.com/reed/acp-gui/internal/settings"
)

func TestSidebarShowsAgentCLIVersions(t *testing.T) {
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
		Agents: []agent.LaunchSpec{
			{ID: agent.PiID, Name: "pi", Version: "0.81.0"},
			{ID: agent.CommandCodeID, Name: "Command Code", Version: "1.88.5"},
			{ID: agent.CodexID, Name: "Codex", Version: "0.169.0"},
		},
	})
	tt := mui.NewTester(app.View, 1060, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	tt.Frame()

	for _, want := range []string{"pi v0.81.0", "Command Code v1.88.5", "Codex v0.169.0"} {
		if !tt.HasText(want) {
			t.Errorf("sidebar missing %q; texts: %q", want, tt.Texts())
		}
	}
	if tt.HasText("0.4.0") {
		t.Errorf("sidebar shows the pi-acp version; texts: %q", tt.Texts())
	}
}

func TestSettingsGearOpensPanelAndNotificationsPersist(t *testing.T) {
	dataDir := t.TempDir()
	app, tt := newSettingsApp(t, dataDir, t.TempDir())
	tt.Frame()

	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.SettingsOpen {
		t.Fatal("Settings button did not open the panel")
	}
	if !tt.HasText("Notifications") || !tt.HasText("Send test notification") {
		t.Fatalf("notification settings not shown; texts: %q", tt.Texts())
	}
	if !tt.HasText("Dock badge") {
		t.Fatalf("Dock badge setting not shown; texts: %q", tt.Texts())
	}
	if !app.NotificationsEnabled || !app.DockBadgeEnabled {
		t.Fatal("notifications and the Dock badge should default on")
	}

	if err := tt.Click("Toggle system notifications"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.NotificationsEnabled {
		t.Fatal("switch did not turn notifications off")
	}
	if err := tt.Click("Toggle Dock badge"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.DockBadgeEnabled {
		t.Fatal("switch did not turn the Dock badge off")
	}
	file := loadPersistedSettings(t, dataDir)
	if file.Notifications {
		t.Fatal("notifications off was not persisted")
	}
	if file.DockBadge {
		t.Fatal("Dock badge off was not persisted")
	}

	restarted := New(Options{
		Version:   "test",
		Workspace: app.Workspace,
		DataDir:   dataDir,
		Agents:    app.Agents,
	})
	t.Cleanup(func() {
		restarted.Shutdown()
		restarted.Flush()
	})
	if restarted.NotificationsEnabled {
		t.Fatal("notifications off was not restored")
	}
	if restarted.DockBadgeEnabled {
		t.Fatal("Dock badge off was not restored")
	}
}

func TestSettingsAppearancePersists(t *testing.T) {
	dataDir := t.TempDir()
	app, tt := newSettingsApp(t, dataDir, t.TempDir())
	tt.Frame()
	if app.Appearance != AppearanceSystem || app.Theme != ThemeDefault {
		t.Fatalf("defaults = %q/%q, want system/default", app.Appearance, app.Theme)
	}

	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Appearance"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"System", "Light", "Dark", "Default", "Nord", "Solarized", "Catppuccin", "Claude", "Green", "Green Soft", "VS Code", "Everforest", "One Dark", "Tokyo Night"} {
		if !tt.HasText(want) {
			t.Errorf("appearance settings missing %q; texts: %q", want, tt.Texts())
		}
	}

	if err := tt.Click("Dark"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.Appearance != AppearanceDark {
		t.Fatalf("appearance = %q, want dark", app.Appearance)
	}
	for _, choice := range []struct {
		label string
		want  string
	}{
		{"Theme: Solarized", ThemeSolarized},
		{"Theme: VS Code", ThemeVSCode},
		{"Theme: Green Soft", ThemeGreenSoft},
		{"Theme: Claude", ThemeClaude},
		{"Theme: Green", ThemeGreen},
	} {
		if err := tt.Click(choice.label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if app.Theme != choice.want {
			t.Fatalf("theme = %q, want %q", app.Theme, choice.want)
		}
	}
	// The catalog overflows the fixed settings pane; scroll to its end and
	// check the last family is still reachable and clickable.
	tt.Scroll(650, 400, 0, 600)
	tt.Frame()
	if err := tt.Click("Theme: Tokyo Night"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.Theme != ThemeTokyoNight {
		t.Fatalf("theme = %q, want %q", app.Theme, ThemeTokyoNight)
	}

	file := loadPersistedSettings(t, dataDir)
	if file.Appearance != AppearanceDark || file.Theme != ThemeTokyoNight {
		t.Fatalf("persisted appearance/theme = %q/%q, want dark/tokyo-night", file.Appearance, file.Theme)
	}

	restarted := New(Options{
		Version:   "test",
		Workspace: app.Workspace,
		DataDir:   dataDir,
		Agents:    app.Agents,
	})
	t.Cleanup(func() {
		restarted.Shutdown()
		restarted.Flush()
	})
	if restarted.Appearance != AppearanceDark || restarted.Theme != ThemeTokyoNight {
		t.Fatalf("restored appearance/theme = %q/%q, want dark/tokyo-night", restarted.Appearance, restarted.Theme)
	}
}

func TestSettingsTestNotification(t *testing.T) {
	app, tt := newSettingsApp(t, t.TempDir(), t.TempDir())
	notes := recordNotifications(app)
	app.openSettings()
	tt.Frame()

	if err := tt.Click("Send test notification"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(*notes) != 1 {
		t.Fatalf("notifications = %+v, want one test notification", *notes)
	}
	if (*notes)[0].Title != "ACP GUI" || (*notes)[0].ID != "acp-gui:test" {
		t.Errorf("test notification = %+v", (*notes)[0])
	}
	if app.NotificationTestNotice == "" {
		t.Error("test notification result is not shown")
	}
}

func TestSettingsCodexProviderPersists(t *testing.T) {
	dataDir := t.TempDir()
	app, tt := newSettingsApp(t, dataDir, t.TempDir())
	tt.Frame()

	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Codex provider"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"Codex provider", "Provider name", "Base URL", "API key", "Models", "Reasoning levels", "Minimal", "Medium", "High", "Title model", "Save provider"} {
		if !tt.HasText(want) {
			t.Errorf("Codex provider settings missing %q; texts: %q", want, tt.Texts())
		}
	}
	if !app.CodexProviderEfforts["medium"] {
		t.Fatal("medium should be checked by default")
	}

	app.lastSelections[agent.CodexID] = settings.Selection{
		ConfigOptions: map[string]string{"model": "gpt-5.6-luna", "reasoning_effort": "high"},
		Mode:          "agent",
	}
	app.CodexProviderDraft = codexprovider.Config{
		Enabled: true,
		Name:    "DeepSeek",
		BaseURL: "https://api.example.com/v1",
		APIKey:  "sk-test",
	}
	app.CodexProviderModelsText = "deepseek-chat\ndeepseek-reasoner\n"
	app.CodexProviderDraft.TitleModel = "deepseek-chat"
	tt.Frame()
	tt.Scroll(650, 400, 0, 500)
	tt.Frame()
	if err := tt.Click("High"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Medium"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.CodexProviderEfforts["medium"] || !app.CodexProviderEfforts["high"] {
		t.Fatalf("reasoning checkboxes = %+v, want high only", app.CodexProviderEfforts)
	}
	tt.Scroll(650, 400, 0, 400)
	tt.Frame()
	if err := tt.Click("Save provider"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.CodexProvider.Enabled || app.CodexProvider.BaseURL != "https://api.example.com/v1" {
		t.Fatalf("provider = %+v", app.CodexProvider)
	}
	if len(app.CodexProvider.Models) != 2 || app.CodexProvider.Models[0] != "deepseek-chat" {
		t.Fatalf("provider models = %+v", app.CodexProvider.Models)
	}
	if len(app.CodexProvider.ReasoningEfforts) != 1 || app.CodexProvider.ReasoningEfforts[0] != "high" {
		t.Fatalf("provider reasoning levels = %+v, want high only", app.CodexProvider.ReasoningEfforts)
	}
	if app.CodexProviderNotice == "" || strings.HasPrefix(app.CodexProviderNotice, "⚠") {
		t.Errorf("save notice = %q", app.CodexProviderNotice)
	}
	if selection := app.lastSelections[agent.CodexID]; len(selection.ConfigOptions) != 0 || selection.Mode != "agent" {
		t.Errorf("provider change kept stale model choices: %+v", selection)
	}

	file := loadPersistedSettings(t, dataDir)
	if !file.CodexProvider.Enabled || file.CodexProvider.APIKey != "sk-test" ||
		len(file.CodexProvider.Models) != 2 || file.CodexProvider.TitleModel != "deepseek-chat" ||
		len(file.CodexProvider.ReasoningEfforts) != 1 || file.CodexProvider.ReasoningEfforts[0] != "high" {
		t.Fatalf("persisted provider = %+v", file.CodexProvider)
	}

	restarted := New(Options{
		Version:   "test",
		Workspace: app.Workspace,
		DataDir:   dataDir,
		Agents:    app.Agents,
	})
	t.Cleanup(func() {
		restarted.Shutdown()
		restarted.Flush()
	})
	if !restarted.CodexProvider.Enabled || restarted.CodexProvider.Name != "DeepSeek" ||
		len(restarted.CodexProvider.ReasoningEfforts) != 1 || restarted.CodexProvider.ReasoningEfforts[0] != "high" {
		t.Fatalf("restored provider = %+v", restarted.CodexProvider)
	}
}

func TestSettingsCodexProviderRejectsBadBaseURL(t *testing.T) {
	app, tt := newSettingsApp(t, t.TempDir(), t.TempDir())
	tt.Frame()
	app.openSettings()
	app.SettingsSection = settingsSectionCodexProvider
	tt.Frame()

	app.CodexProviderDraft = codexprovider.Config{Enabled: true, BaseURL: "api.example.com"}
	tt.Frame()
	tt.Scroll(650, 400, 0, 900)
	tt.Frame()
	if err := tt.Click("Save provider"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.CodexProvider.Enabled {
		t.Fatal("invalid provider was saved")
	}
	if !strings.HasPrefix(app.CodexProviderNotice, "⚠") {
		t.Errorf("notice = %q, want an error", app.CodexProviderNotice)
	}
}

func TestSettingsCodexProviderRequiresReasoningLevel(t *testing.T) {
	app, _ := newSettingsApp(t, t.TempDir(), t.TempDir())
	app.CodexProviderDraft = codexprovider.Config{
		Enabled: true,
		BaseURL: "https://api.example.com/v1",
		Models:  []string{"deepseek-chat"},
	}
	app.CodexProviderEfforts = map[string]bool{}
	app.saveCodexProvider()
	if app.CodexProvider.Enabled {
		t.Fatal("provider with no reasoning level was saved")
	}
	if !strings.Contains(app.CodexProviderNotice, "Select at least one reasoning level") {
		t.Errorf("notice = %q, want a reasoning-level error", app.CodexProviderNotice)
	}
}
