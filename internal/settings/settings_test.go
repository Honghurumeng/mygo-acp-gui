package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/reed/acp-gui/internal/codexprovider"
)

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	file, err := Open(path).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if file.Version != CurrentVersion {
		t.Errorf("version = %d, want %d", file.Version, CurrentVersion)
	}
	if file.LastAgent != "" || file.LastWorkspace != "" {
		t.Errorf("missing file returned data: %+v", file)
	}
	if !file.Notifications {
		t.Error("notifications should default to on")
	}
	if !file.DockBadge {
		t.Error("the Dock badge should default to on")
	}
	if file.Fonts != DefaultFonts() {
		t.Errorf("fonts = %+v, want %+v", file.Fonts, DefaultFonts())
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "settings.json")
	store := Open(path)
	workspace := t.TempDir()
	want := File{
		Version:             CurrentVersion,
		LastAgent:           "command-code",
		LastWorkspace:       workspace,
		Workspaces:          []string{workspace},
		RemovedWorkspaces:   []string{workspace},
		CollapsedWorkspaces: []string{workspace},
		Paths:               Paths{Shell: "/bin/zsh", Node: "/usr/local/bin/node"},
		Appearance:          "dark",
		Theme:               "solarized",
		Fonts:               Fonts{WorkspaceList: 110, Chat: 125, Composer: 90, Git: 140},
		ViewerWidth:         720,
		Notifications:       true,
		DockBadge:           true,
		CodexProvider: codexprovider.Config{
			Enabled:            true,
			Name:               "DeepSeek",
			BaseURL:            "https://api.example.com/v1",
			APIKey:             "sk-test",
			Models:             []string{"deepseek-chat", "deepseek-reasoner"},
			ContextWindow:      262_144,
			AutoCompactPercent: 75,
			ReasoningEfforts:   []string{"low", "medium", "high"},
			TitleModel:         "deepseek-chat",
		},
	}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".settings-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) > 0 {
		t.Errorf("temporary files left behind: %v", leftovers)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.LastAgent != want.LastAgent || got.LastWorkspace != want.LastWorkspace {
		t.Errorf("round trip = %+v", got)
	}
	if len(got.Workspaces) != 1 || got.Workspaces[0] != workspace {
		t.Errorf("workspaces = %+v", got.Workspaces)
	}
	if len(got.RemovedWorkspaces) != 1 || got.RemovedWorkspaces[0] != workspace {
		t.Errorf("removedWorkspaces = %+v", got.RemovedWorkspaces)
	}
	if len(got.CollapsedWorkspaces) != 1 || got.CollapsedWorkspaces[0] != workspace {
		t.Errorf("collapsedWorkspaces = %+v", got.CollapsedWorkspaces)
	}
	if got.Paths != want.Paths {
		t.Errorf("paths = %+v, want %+v", got.Paths, want.Paths)
	}
	if got.Appearance != want.Appearance || got.Theme != want.Theme {
		t.Errorf("appearance/theme = %q/%q, want %q/%q", got.Appearance, got.Theme, want.Appearance, want.Theme)
	}
	if !got.Notifications {
		t.Error("notifications were not restored")
	}
	if !got.DockBadge {
		t.Error("the Dock badge was not restored")
	}
	if got.Fonts != want.Fonts {
		t.Errorf("fonts = %+v, want %+v", got.Fonts, want.Fonts)
	}
	if got.ViewerWidth != want.ViewerWidth {
		t.Errorf("viewerWidth = %v, want %v", got.ViewerWidth, want.ViewerWidth)
	}
	if !reflect.DeepEqual(got.CodexProvider, want.CodexProvider) {
		t.Errorf("codexProvider = %+v, want %+v", got.CodexProvider, want.CodexProvider)
	}
}

func TestCodexProviderInvalidEntryIsDisabledWithWarning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	document, err := json.Marshal(map[string]any{
		"version": CurrentVersion,
		"codexProvider": map[string]any{
			"enabled": true,
			"baseURL": "api.example.com",
			"models":  []string{"deepseek-chat"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, document, 0o600); err != nil {
		t.Fatal(err)
	}

	file, err := Open(path).Load()
	if err == nil || !strings.Contains(err.Error(), "codexProvider") {
		t.Fatalf("Load error = %v, want a codexProvider warning", err)
	}
	if file.CodexProvider.Enabled {
		t.Fatal("invalid provider stayed enabled")
	}
	if file.CodexProvider.BaseURL != "api.example.com" {
		t.Errorf("provider fields were lost: %+v", file.CodexProvider)
	}
}

func TestFontsDefaultForOlderFilesAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	store := Open(path)
	if err := os.WriteFile(path, []byte(`{"version": 1, "lastAgent": "pi"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if file.Fonts != DefaultFonts() {
		t.Fatalf("a file without fonts should default to 100%%: %+v", file.Fonts)
	}

	file.Fonts = Fonts{WorkspaceList: 120, Chat: 130, Composer: 80, Git: 150}
	if err := store.Save(file); err != nil {
		t.Fatal(err)
	}
	file, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if file.Fonts != (Fonts{WorkspaceList: 120, Chat: 130, Composer: 80, Git: 150}) {
		t.Fatalf("fonts did not round trip: %+v", file.Fonts)
	}
}

func TestNotificationsDefaultOnAndPersistOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	store := Open(path)
	if err := os.WriteFile(path, []byte(`{"version": 1, "lastAgent": "pi"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !file.Notifications {
		t.Fatal("a file without notifications should default to on")
	}

	file.Notifications = false
	if err := store.Save(file); err != nil {
		t.Fatal(err)
	}
	file, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if file.Notifications {
		t.Fatal("an explicit off setting was not persisted")
	}
}

func TestDockBadgeDefaultOnAndPersistOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	store := Open(path)
	if err := os.WriteFile(path, []byte(`{"version": 1, "lastAgent": "pi"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !file.DockBadge {
		t.Fatal("a file without dockBadge should default to on")
	}

	file.DockBadge = false
	if err := store.Save(file); err != nil {
		t.Fatal(err)
	}
	file, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if file.DockBadge {
		t.Fatal("an explicit off setting was not persisted")
	}
}

func TestLoadCorruptFileReturnsDefaultsAndError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := Open(path).Load()
	if err == nil {
		t.Fatal("corrupt file did not produce an error")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("error = %v", err)
	}
	if file.Version != CurrentVersion || file.LastWorkspace != "" {
		t.Errorf("defaults not returned: %+v", file)
	}
}

func TestLoadIgnoresLegacyMCPField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	workspace := t.TempDir()
	document, err := json.Marshal(map[string]any{
		"version":       CurrentVersion,
		"lastAgent":     "pi",
		"lastWorkspace": workspace,
		"mcpServers":    map[string]any{"name": "not-an-array"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, document, 0o600); err != nil {
		t.Fatal(err)
	}

	file, err := Open(path).Load()
	if err != nil {
		t.Fatalf("legacy mcpServers should be ignored: %v", err)
	}
	if file.LastAgent != "pi" || file.LastWorkspace != workspace {
		t.Errorf("good fields lost: %+v", file)
	}
}

func TestLoadDropsRelativePaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	document, err := json.Marshal(map[string]any{
		"version": CurrentVersion,
		"paths": map[string]string{
			"shell": "/bin/zsh",
			"node":  "bin/node",
			"pi":    "/usr/local/bin/pi",
			"codex": "/usr/local/bin/codex",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, document, 0o600); err != nil {
		t.Fatal(err)
	}

	file, err := Open(path).Load()
	if err == nil || !strings.Contains(err.Error(), "relative node path") {
		t.Fatalf("error = %v", err)
	}
	if file.Paths.Node != "" || file.Paths.Pi != "/usr/local/bin/pi" ||
		file.Paths.Codex != "/usr/local/bin/codex" || file.Paths.Shell != "/bin/zsh" {
		t.Errorf("paths = %+v", file.Paths)
	}
}

func TestLoadNewerVersionWarns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"version": 99, "lastAgent": "pi"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := Open(path).Load()
	if err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("error = %v", err)
	}
	if file.LastAgent != "pi" {
		t.Errorf("lastAgent = %q", file.LastAgent)
	}
}

func TestEmptyStoreIsNoOp(t *testing.T) {
	var nilStore *Store
	if err := nilStore.Save(File{LastAgent: "pi"}); err != nil {
		t.Errorf("nil Save: %v", err)
	}
	if file, err := nilStore.Load(); err != nil || file.Version != CurrentVersion {
		t.Errorf("nil Load = %+v, %v", file, err)
	}
	if path := Open("").Path(); path != "" {
		t.Errorf("path = %q", path)
	}
	if err := Open("").Save(File{LastAgent: "pi"}); err != nil {
		t.Errorf("empty Save: %v", err)
	}
}

func TestLoadSanitizesWorkspaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	workspace := t.TempDir()
	document, err := json.Marshal(map[string]any{
		"version":           CurrentVersion,
		"workspaces":        []string{workspace, "relative/dir", workspace, ""},
		"removedWorkspaces": []string{workspace, "relative/dir", workspace},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, document, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := Open(path).Load()
	if err == nil || !strings.Contains(err.Error(), "relative workspace") {
		t.Fatalf("error = %v", err)
	}
	if len(file.Workspaces) != 1 || file.Workspaces[0] != workspace {
		t.Errorf("workspaces = %+v", file.Workspaces)
	}
	if len(file.RemovedWorkspaces) != 1 || file.RemovedWorkspaces[0] != workspace {
		t.Errorf("removedWorkspaces = %+v", file.RemovedWorkspaces)
	}
}

func TestLastSelectionsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	store := Open(path)
	want := map[string]Selection{
		"pi": {
			ConfigOptions: map[string]string{"model": "anthropic/claude", "thought_level": "high"},
			Mode:          "plan",
		},
	}
	if err := store.Save(File{LastSelections: want}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.LastSelections) != 1 {
		t.Fatalf("lastSelections = %+v", got.LastSelections)
	}
	pi := got.LastSelections["pi"]
	if pi.Mode != "plan" || pi.ConfigOptions["model"] != "anthropic/claude" || pi.ConfigOptions["thought_level"] != "high" {
		t.Errorf("lastSelections[pi] = %+v", pi)
	}
}

func TestLastSelectionsDropEmptyEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	document := `{"version": 1, "lastSelections": {
		" pi ": {"configOptions": {"model": " demo/large ", "": "x", "thought_level": " "}, "mode": " plan "},
		"": {"mode": "plan"},
		"other": {"configOptions": {}}
	}}`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := Open(path).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(file.LastSelections) != 1 {
		t.Fatalf("lastSelections = %+v", file.LastSelections)
	}
	pi := file.LastSelections["pi"]
	if pi.Mode != "plan" || len(pi.ConfigOptions) != 1 || pi.ConfigOptions["model"] != "demo/large" {
		t.Errorf("lastSelections[pi] = %+v", pi)
	}
}

func TestInvalidLastSelectionsKeepOtherFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"version": 1, "lastAgent": "pi", "lastSelections": {"pi": "bad"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := Open(path).Load()
	if err == nil || !strings.Contains(err.Error(), "lastSelections") {
		t.Fatalf("error = %v", err)
	}
	if file.LastAgent != "pi" {
		t.Errorf("lastAgent = %q, want pi", file.LastAgent)
	}
	if file.LastSelections != nil {
		t.Errorf("lastSelections = %+v, want nil", file.LastSelections)
	}
}
