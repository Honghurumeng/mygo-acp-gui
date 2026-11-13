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
		},
	})
	tt := mui.NewTester(app.View, 1060, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	tt.Frame()

	for _, want := range []string{"pi v0.81.0", "Command Code v1.88.5"} {
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
	if !app.NotificationsEnabled {
		t.Fatal("notifications should default on")
	}

	if err := tt.Click("Toggle system notifications"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.NotificationsEnabled {
		t.Fatal("switch did not turn notifications off")
	}
	file := loadPersistedSettings(t, dataDir)
	if file.Notifications {
		t.Fatal("notifications off was not persisted")
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
