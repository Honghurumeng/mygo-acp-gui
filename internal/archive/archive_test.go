package archive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store := Open(path)
	file := File{
		Version: CurrentVersion,
		Sessions: []Session{
			{
				AgentID:   "pi",
				SessionID: "s1",
				Cwd:       "/work/a",
				Title:     "Agent title",
				ShowName:  "My title",
				CreatedAt: "2026-01-01T00:00:00Z",
				UpdatedAt: "2026-01-02T00:00:00Z",
				Show:      true,
			},
			{AgentID: "command-code", SessionID: "s2", Cwd: "/work/b", Show: false},
		},
	}
	if err := store.Save(file); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("archive mode = %v, want 0600", info.Mode().Perm())
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != CurrentVersion || len(loaded.Sessions) != 2 {
		t.Fatalf("loaded = %+v", loaded)
	}
	if got := loaded.Sessions[0]; got.ShowName != "My title" || !got.Show || got.AgentID != "pi" {
		t.Fatalf("first session = %+v", got)
	}
	if got := loaded.Sessions[1]; got.Show || got.AgentID != "command-code" {
		t.Fatalf("second session = %+v", got)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	file, err := Open(filepath.Join(t.TempDir(), "missing.json")).Load()
	if err != nil {
		t.Fatal(err)
	}
	if file.Version != CurrentVersion || len(file.Sessions) != 0 {
		t.Fatalf("file = %+v", file)
	}
}

func TestLoadDefaultsMissingShowToVisible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	data := `{"version":1,"sessions":[{"agentId":"pi","sessionId":"s1","title":"Old"}]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := Open(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Sessions) != 1 || !file.Sessions[0].Show {
		t.Fatalf("file = %+v", file)
	}
}

func TestLoadDropsInvalidAndDuplicateRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	data := `{
		"version": 99,
		"sessions": [
			{"agentId":"pi","sessionId":"s1"},
			{"agentId":"pi","sessionId":"s1"},
			{"agentId":"","sessionId":"orphan"},
			{"agentId":"pi","sessionId":""}
		]
	}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := Open(path).Load()
	if err == nil {
		t.Fatal("expected warnings for newer version and bad rows")
	}
	if !strings.Contains(err.Error(), "version 99") || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("warnings = %v", err)
	}
	if len(file.Sessions) != 1 {
		t.Fatalf("sessions = %+v", file.Sessions)
	}
}

func TestLoadCorruptJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path).Load(); err == nil {
		t.Fatal("corrupt archive was accepted")
	}
}

func TestNilStoreIsNoop(t *testing.T) {
	var store *Store
	if err := store.Save(File{}); err != nil {
		t.Fatal(err)
	}
	file, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Sessions) != 0 {
		t.Fatalf("file = %+v", file)
	}
}
