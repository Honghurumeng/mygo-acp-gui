package ui

import (
	"os"
	"path/filepath"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/i18n"
)

// newLanguageApp builds an app with one injected agent, so no discovery runs,
// which records the language setting it hands to MyGo.
func newLanguageApp(t *testing.T, dataDir string, preferred []string) (*App, *mui.Tester, *[]string) {
	t.Helper()
	t.Cleanup(func() { i18n.Set(i18n.English) })
	var locales []string
	app := New(Options{
		Version:            "test",
		Workspace:          t.TempDir(),
		DataDir:            dataDir,
		PreferredLanguages: preferred,
		OnLanguage:         func(setting string) { locales = append(locales, setting) },
		Agents:             []agent.LaunchSpec{{ID: agent.PiID, Name: "pi", Version: "0.81.0"}},
	})
	tt := mui.NewTester(app.View, 1060, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	return app, tt, &locales
}

// TestLanguageFollowsTheSystemPreference checks the default: System takes the
// first of the user's languages the app is written in.
func TestLanguageFollowsTheSystemPreference(t *testing.T) {
	app, tt, locales := newLanguageApp(t, t.TempDir(), []string{"de-DE", "zh-CN"})
	tt.Frame()
	if app.Language != i18n.System {
		t.Fatalf("default language = %q, want %q", app.Language, i18n.System)
	}
	if i18n.Language() != i18n.Chinese {
		t.Fatalf("shown language = %q, want Chinese", i18n.Language())
	}
	if !tt.HasText("工作区") || !tt.HasText("发送") {
		t.Fatalf("the app is not in Chinese; texts: %q", tt.Texts())
	}
	if got := *locales; len(got) != 1 || got[0] != i18n.System {
		t.Fatalf("OnLanguage calls = %v, want [system]", got)
	}
}

// TestLanguageSwitchInSettings switches through the panel and checks that the
// texts, the setting and MyGo's own language all follow, oldest first.
func TestLanguageSwitchInSettings(t *testing.T) {
	dataDir := t.TempDir()
	app, tt, locales := newLanguageApp(t, dataDir, []string{"en-US"})
	tt.Frame()
	if i18n.Language() != i18n.English {
		t.Fatalf("shown language = %q, want English", i18n.Language())
	}

	app.openSettings()
	tt.Frame()
	if err := tt.Click("Language"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("English") || !tt.HasText("简体中文") || !tt.HasText("System") {
		t.Fatalf("language category missing; texts: %q", tt.Texts())
	}

	if err := tt.Click("简体中文"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.Language != i18n.Chinese || i18n.Language() != i18n.Chinese {
		t.Fatalf("setting %q, shown %q; want Chinese", app.Language, i18n.Language())
	}
	if !tt.HasText("语言") || !tt.HasText("外观") {
		t.Fatalf("panel did not switch to Chinese; texts: %q", tt.Texts())
	}
	if file := loadPersistedSettings(t, dataDir); file.Language != i18n.Chinese {
		t.Fatalf("persisted language = %q, want %q", file.Language, i18n.Chinese)
	}

	if err := tt.Click("English"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.Language != i18n.English || i18n.Language() != i18n.English {
		t.Fatalf("setting %q, shown %q; want English", app.Language, i18n.Language())
	}
	if !tt.HasText("Language") || tt.HasText("语言") {
		t.Fatalf("panel did not switch back to English; texts: %q", tt.Texts())
	}
	want := []string{i18n.System, i18n.Chinese, i18n.English}
	if got := *locales; len(got) != len(want) {
		t.Fatalf("OnLanguage calls = %v, want %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("OnLanguage calls = %v, want %v", got, want)
			}
		}
	}
}

// TestSystemLanguageFollowsTheDesktop checks that a change of the user's
// languages mid-run is picked up by the System choice, and that a fixed
// language ignores it.
func TestSystemLanguageFollowsTheDesktop(t *testing.T) {
	app, tt, _ := newLanguageApp(t, t.TempDir(), []string{"en-US"})
	tt.Frame()
	if i18n.Language() != i18n.English {
		t.Fatalf("shown language = %q, want English", i18n.Language())
	}
	app.PreferredLanguagesChanged([]string{"zh-CN", "en-US"})
	tt.Frame()
	if i18n.Language() != i18n.Chinese {
		t.Fatalf("shown language = %q, want Chinese after the desktop changed", i18n.Language())
	}

	app.setLanguage(i18n.English)
	app.PreferredLanguagesChanged([]string{"zh-CN"})
	if i18n.Language() != i18n.English {
		t.Fatalf("a fixed English followed the desktop to %q", i18n.Language())
	}
}

// TestSavedLanguageLoads checks that the settings file is enough to start in
// Chinese, as the next launch after a switch does.
func TestSavedLanguageLoads(t *testing.T) {
	dataDir := t.TempDir()
	settingsPath := filepath.Join(dataDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"version":1,"language":"zh-Hans"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	app, tt, _ := newLanguageApp(t, dataDir, []string{"en-US"})
	tt.Frame()
	if app.Language != i18n.Chinese || i18n.Language() != i18n.Chinese {
		t.Fatalf("setting %q, shown %q; want Chinese", app.Language, i18n.Language())
	}
	if !tt.HasText("工作区") {
		t.Fatalf("saved Chinese was not applied; texts: %q", tt.Texts())
	}
}
