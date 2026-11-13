package ui

import (
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/settings"
)

func TestNormalizeFontScales(t *testing.T) {
	if got := normalizeFontScales(settings.Fonts{}); got != settings.DefaultFonts() {
		t.Errorf("zero fonts = %+v, want %+v", got, settings.DefaultFonts())
	}
	got := normalizeFontScales(settings.Fonts{
		WorkspaceList: 60,
		Chat:          0,
		Composer:      200,
		Git:           125,
	})
	want := settings.Fonts{
		WorkspaceList: minFontScale,
		Chat:          settings.DefaultFontScale,
		Composer:      maxFontScale,
		Git:           125,
	}
	if got != want {
		t.Errorf("normalized fonts = %+v, want %+v", got, want)
	}
}

func TestScaleFont(t *testing.T) {
	for _, test := range []struct {
		size    float32
		percent int
		want    float32
	}{
		{10, 100, 10},
		{10, 150, 15},
		{10, 80, 8},
		{11, 0, 11},
	} {
		if got := scaleFont(test.size, test.percent); got != test.want {
			t.Errorf("scaleFont(%v, %d) = %v, want %v", test.size, test.percent, got, test.want)
		}
	}

	app := New(Options{Agents: []agent.LaunchSpec{}})
	t.Cleanup(app.Shutdown)
	app.Fonts = settings.Fonts{WorkspaceList: 120, Chat: 90, Composer: 110, Git: 150}
	if got := app.workspaceFont(10); got != 12 {
		t.Errorf("workspaceFont = %v, want 12", got)
	}
	if got := app.chatFont(10); got != 9 {
		t.Errorf("chatFont = %v, want 9", got)
	}
	if got := app.composerFont(10); got != 11 {
		t.Errorf("composerFont = %v, want 11", got)
	}
	if got := app.gitFont(10); got != 15 {
		t.Errorf("gitFont = %v, want 15", got)
	}
}

func TestChatFontScaleReachesMarkdown(t *testing.T) {
	app := New(Options{Version: "test", Workspace: t.TempDir(), Agents: []agent.LaunchSpec{}})
	app.Fonts.Chat = 125
	var themeSize float32
	tt := mui.NewTester(func(c *mui.Context) {
		themeSize = c.Theme().FontSize
		app.View(c)
	}, 1060, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	tt.Frame()

	if themeSize == 0 {
		t.Fatal("view did not see a theme font size")
	}
	want := scaleFont(themeSize, 125)
	if app.MD.Theme == nil || app.MD.Theme.FontSize != want {
		t.Fatalf("markdown font = %+v, want %v", app.MD.Theme, want)
	}
}

func TestFontSettingsUIAndPersistence(t *testing.T) {
	dataDir := t.TempDir()
	workspace := t.TempDir()
	app := New(Options{
		Version:   "test",
		Workspace: workspace,
		DataDir:   dataDir,
		Agents:    []agent.LaunchSpec{},
	})
	tt := mui.NewTester(app.View, 1060, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	tt.Frame()

	if app.Fonts != settings.DefaultFonts() {
		t.Fatalf("defaults = %+v, want 100%% everywhere", app.Fonts)
	}

	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Fonts"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"Workspace list", "Conversation", "Input box", "Git panel"} {
		if !tt.HasText(want) {
			t.Errorf("font settings missing %q; texts: %q", want, tt.Texts())
		}
	}

	if err := tt.Click("Increase Conversation font size"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.Fonts.Chat != 110 {
		t.Fatalf("chat scale = %d, want 110", app.Fonts.Chat)
	}
	if err := tt.Click("Decrease Workspace list font size"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.Fonts.WorkspaceList != 90 {
		t.Fatalf("workspace scale = %d, want 90", app.Fonts.WorkspaceList)
	}

	file := loadPersistedSettings(t, dataDir)
	if file.Fonts.Chat != 110 || file.Fonts.WorkspaceList != 90 {
		t.Fatalf("persisted fonts = %+v, want chat 110 and workspace 90", file.Fonts)
	}

	restarted := New(Options{
		Version:   "test",
		Workspace: workspace,
		DataDir:   dataDir,
		Agents:    []agent.LaunchSpec{},
	})
	t.Cleanup(func() {
		restarted.Shutdown()
		restarted.Flush()
	})
	if restarted.Fonts.Chat != 110 || restarted.Fonts.WorkspaceList != 90 {
		t.Fatalf("restored fonts = %+v, want chat 110 and workspace 90", restarted.Fonts)
	}

	tt2 := mui.NewTester(restarted.View, 1060, 760)
	tt2.Frame()
	restarted.openSettings()
	restarted.SettingsSection = settingsSectionFonts
	tt2.Frame()
	if err := tt2.Click("Reset font sizes"); err != nil {
		t.Fatal(err)
	}
	tt2.Frame()
	if restarted.Fonts != settings.DefaultFonts() {
		t.Fatalf("fonts after reset = %+v, want 100%% everywhere", restarted.Fonts)
	}
	file = loadPersistedSettings(t, dataDir)
	if file.Fonts != settings.DefaultFonts() {
		t.Fatalf("persisted fonts after reset = %+v, want 100%% everywhere", file.Fonts)
	}
}
