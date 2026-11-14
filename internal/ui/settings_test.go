package ui

import (
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
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
