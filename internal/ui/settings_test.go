package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
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

func TestCodexSettingsPanelAndOverride(t *testing.T) {
	dataDir := t.TempDir()
	app, tt := newSettingsApp(t, dataDir, t.TempDir())
	tt.Frame()
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Codex"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"Codex provider", "Use custom provider"} {
		if !tt.HasText(want) {
			t.Fatalf("Codex settings missing %q; texts: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("Toggle custom Codex provider"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.CodexSettings.Override {
		t.Fatal("Codex override toggle did not turn on")
	}
	if file := loadPersistedSettings(t, dataDir); !file.Codex.Override {
		t.Fatal("Codex override was not persisted")
	}
	for _, want := range []string{"Base URL", "Model", "Context window (tokens)", "Reasoning levels", "API key environment variable", "API key (optional)", "Apply Codex settings"} {
		if !tt.HasText(want) {
			t.Fatalf("Codex provider fields missing %q; texts: %q", want, tt.Texts())
		}
	}
}

func TestApplyCodexSettingsBuildsLaunchEnv(t *testing.T) {
	base := agent.LaunchSpec{
		ID:      agent.CodexID,
		Name:    "Codex",
		Command: "/bin/echo",
		Env:     []string{"PATH=/usr/bin:/bin"},
		Source:  agent.SourceVendored,
	}
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
		Agents:    []agent.LaunchSpec{base},
	})
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	app.CodexSettings = settings.Codex{
		Override: true,
		Provider: settings.CodexProvider{
			ID:        "demo",
			Name:      "Demo",
			BaseURL:   "https://example.com",
			APIKeyEnv: "DEMO_API_KEY",
			APIKey:    "sk-literal-secret",
			Model:     "demo-model",
		},
	}
	app.applyCodexSettings()
	if app.CodexError != "" {
		t.Fatalf("applyCodexSettings: %s", app.CodexError)
	}
	spec, ok := app.currentCodexSpec()
	if !ok {
		t.Fatal("Codex spec missing")
	}
	raw := agent.EnvValue(spec.Env, "CODEX_CONFIG")
	if raw == "" {
		t.Fatal("CODEX_CONFIG was not added")
	}
	if !strings.Contains(raw, `"demo-model"`) || !strings.Contains(raw, `"demo"`) {
		t.Errorf("CODEX_CONFIG = %s", raw)
	}
	if !strings.Contains(raw, `"base_url":"https://example.com/v1"`) {
		t.Errorf("CODEX_CONFIG base URL was not normalized: %s", raw)
	}
	if !strings.Contains(raw, `"model_catalog_url":"http://127.0.0.1:`) {
		t.Errorf("CODEX_CONFIG is missing the generated catalog URL: %s", raw)
	}
	catalogURL := app.codexCatalogURL()
	if catalogURL == "" {
		t.Fatal("the local Codex catalog server did not start")
	}
	if catalogURL != providerCatalogURL(t, raw) {
		t.Errorf("catalog URL = %q, CODEX_CONFIG has %q", catalogURL, providerCatalogURL(t, raw))
	}
	catalog := readCatalog(t, catalogURL)
	if !strings.Contains(catalog, `"slug":"demo-model"`) || !strings.Contains(catalog, `"effort":"medium"`) {
		t.Errorf("catalog = %s", catalog)
	}

	// An explicit catalog URL is an expert override: do not replace it with
	// the generated one.
	app.CodexSettings.Provider.ModelCatalogURL = "https://example.com/models"
	app.applyCodexSettings()
	spec, _ = app.currentCodexSpec()
	if raw = agent.EnvValue(spec.Env, "CODEX_CONFIG"); !strings.Contains(raw, `"model_catalog_url":"https://example.com/models"`) {
		t.Errorf("explicit catalog URL was replaced: %s", raw)
	}
	if strings.Contains(raw, "sk-literal-secret") {
		t.Fatalf("literal key leaked into CODEX_CONFIG: %s", raw)
	}
	if got := agent.EnvValue(spec.Env, "DEMO_API_KEY"); got != "sk-literal-secret" {
		t.Errorf("DEMO_API_KEY = %q, want literal key", got)
	}
	if len(app.baseAgents) != 1 || agent.EnvValue(app.baseAgents[0].Env, "CODEX_CONFIG") != "" {
		t.Error("base Codex spec was mutated")
	}

	app.CodexSettings.Override = false
	app.applyCodexSettings()
	spec, _ = app.currentCodexSpec()
	if agent.EnvValue(spec.Env, "CODEX_CONFIG") != "" {
		t.Error("disabling the override left CODEX_CONFIG behind")
	}

	app.CodexSettings = settings.Codex{Override: true, Provider: settings.CodexProvider{
		ID:      "bad id",
		BaseURL: "not a url",
		Model:   "demo",
	}}
	app.applyCodexSettings()
	if app.CodexError == "" {
		t.Error("invalid Codex settings were accepted")
	}
}

func TestApplyCodexTextSettingsRejectsBadContextWindow(t *testing.T) {
	app := New(Options{Version: "test", Workspace: t.TempDir()})
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	app.CodexSettings = settings.Codex{Override: true, Provider: settings.CodexProvider{
		BaseURL:         "https://example.com",
		Model:           "demo",
		ContextWindow:   200000,
		ReasoningLevels: []string{"medium"},
	}}
	app.CodexContextText = "not-a-number"
	app.CodexReasoningText = "low, max"
	if app.applyCodexTextSettings() {
		t.Fatal("invalid context window was accepted")
	}
	if !strings.Contains(app.CodexError, "context window") {
		t.Errorf("CodexError = %q", app.CodexError)
	}
	if len(app.CodexSettings.Provider.ReasoningLevels) != 1 {
		t.Error("reasoning levels were changed by a failed parse")
	}

	app.CodexContextText = "131072"
	if !app.applyCodexTextSettings() {
		t.Fatalf("applyCodexTextSettings: %s", app.CodexError)
	}
	if app.CodexSettings.Provider.ContextWindow != 131072 {
		t.Errorf("context window = %d", app.CodexSettings.Provider.ContextWindow)
	}
	if strings.Join(app.CodexSettings.Provider.ReasoningLevels, ",") != "low,max" {
		t.Errorf("reasoning levels = %v", app.CodexSettings.Provider.ReasoningLevels)
	}
	app.applyCodexSettings()
	if app.CodexError != "" {
		t.Fatalf("applyCodexSettings: %s", app.CodexError)
	}

	app.CodexReasoningText = "low, turbo"
	if !app.applyCodexTextSettings() {
		t.Fatalf("applyCodexTextSettings: %s", app.CodexError)
	}
	app.applyCodexSettings()
	if app.CodexError == "" || !strings.Contains(app.CodexError, "unknown reasoning level") {
		t.Fatalf("CodexError = %q, want an unknown-level error", app.CodexError)
	}
}

func providerCatalogURL(t *testing.T, raw string) string {
	t.Helper()
	var config struct {
		Providers map[string]map[string]any `json:"model_providers"`
		Provider  string                    `json:"model_provider"`
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatalf("decode CODEX_CONFIG: %v", err)
	}
	provider := config.Providers[config.Provider]
	url, _ := provider["model_catalog_url"].(string)
	return url
}

func readCatalog(t *testing.T, url string) string {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET catalog: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET catalog status = %d: %s", response.StatusCode, body)
	}
	return string(body)
}
