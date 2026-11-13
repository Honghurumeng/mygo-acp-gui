package modelconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/reed/acp-gui/internal/acp"
)

func sampleOptions(level string) []acp.ConfigOption {
	return []acp.ConfigOption{
		{
			ID: "model", Name: "Model", Category: "model", Type: "select", CurrentValue: "demo/small",
			Options: []acp.ConfigOptionValue{
				{Value: "demo/small", Name: "demo/small"},
				{Value: "demo/large", Name: "demo/large"},
			},
		},
		{
			ID: "thought_level", Name: "Thinking", Category: "thought_level", Type: "select", CurrentValue: level,
			Options: []acp.ConfigOptionValue{{Value: "low"}, {Value: level}},
		},
	}
}

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model-config.json")
	store := Open(path)
	file := File{
		Version: CurrentVersion,
		Agents: map[string]map[string]Model{
			"fake": {
				"demo/small":   {ConfigOptions: sampleOptions("low"), ProbedAt: "2026-01-01T00:00:00Z"},
				"demo/large":   {ConfigOptions: sampleOptions("high")},
				"demo/missing": {},
			},
			"empty": {},
		},
	}
	if err := store.Save(file); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want 600", got)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Version != CurrentVersion {
		t.Errorf("version = %d", loaded.Version)
	}
	if len(loaded.Agents) != 1 {
		t.Fatalf("agents = %+v", loaded.Agents)
	}
	fake, ok := loaded.Agents["fake"]
	if !ok {
		t.Fatal("fake agent missing")
	}
	// Entries without options and empty agent buckets are dropped.
	if len(fake) != 2 {
		t.Fatalf("models = %+v", fake)
	}
	if got := fake["demo/small"]; got.ProbedAt != "2026-01-01T00:00:00Z" ||
		len(got.ConfigOptions) != 2 || got.ConfigOptions[1].CurrentValue != "low" {
		t.Errorf("demo/small = %+v", got)
	}
}

func TestStoreMissingAndCorruptFiles(t *testing.T) {
	dir := t.TempDir()
	missing := Open(filepath.Join(dir, "missing.json"))
	file, err := missing.Load()
	if err != nil {
		t.Fatalf("missing load: %v", err)
	}
	if len(file.Agents) != 0 {
		t.Errorf("missing file agents = %+v", file.Agents)
	}

	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	corrupt, err := Open(path).Load()
	if err == nil {
		t.Fatal("corrupt file loaded without error")
	}
	if len(corrupt.Agents) != 0 {
		t.Errorf("corrupt file agents = %+v", corrupt.Agents)
	}
}

func TestNullStoreIsNoOp(t *testing.T) {
	var store *Store
	if err := store.Save(File{}); err != nil {
		t.Errorf("nil save: %v", err)
	}
	file, err := store.Load()
	if err != nil || file.Agents == nil {
		t.Errorf("nil load = %+v %v", file, err)
	}
	if got := Open("").Path(); got != "" {
		t.Errorf("empty path = %q", got)
	}
}
