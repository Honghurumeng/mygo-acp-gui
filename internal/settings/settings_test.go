package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if file.LastAgent != "" || file.LastWorkspace != "" || file.MCPServers != nil {
		t.Errorf("missing file returned data: %+v", file)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "settings.json")
	store := Open(path)
	workspace := t.TempDir()
	want := File{
		Version:       CurrentVersion,
		LastAgent:     "command-code",
		LastWorkspace: workspace,
		MCPServers: []any{
			map[string]any{"name": "demo", "command": "true"},
		},
		Paths: Paths{Shell: "/bin/zsh", Node: "/usr/local/bin/node"},
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
	if len(got.MCPServers) != 1 {
		t.Fatalf("mcp servers = %+v", got.MCPServers)
	}
	if server, ok := got.MCPServers[0].(map[string]any); !ok || server["name"] != "demo" {
		t.Errorf("mcp server = %#v", got.MCPServers[0])
	}
	if got.Paths != want.Paths {
		t.Errorf("paths = %+v, want %+v", got.Paths, want.Paths)
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

func TestLoadKeepsGoodFieldsWhenMCPIsBad(t *testing.T) {
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
	if err == nil || !strings.Contains(err.Error(), "mcpServers") {
		t.Fatalf("error = %v", err)
	}
	if file.LastAgent != "pi" || file.LastWorkspace != workspace {
		t.Errorf("good fields lost: %+v", file)
	}
	if file.MCPServers != nil {
		t.Errorf("bad MCP list kept: %+v", file.MCPServers)
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
	if file.Paths.Node != "" || file.Paths.Pi != "/usr/local/bin/pi" || file.Paths.Shell != "/bin/zsh" {
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
