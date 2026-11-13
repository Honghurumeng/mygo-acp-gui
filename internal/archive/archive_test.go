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
				UserTurns: []UserTurn{
					{Number: 1, SentAt: "2026-01-01T09:41:00Z", DurationMs: 83000},
					{Number: 2, SentAt: "2026-01-02T10:15:00Z"},
				},
				Show: true,
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
	if turns := loaded.Sessions[0].UserTurns; len(turns) != 2 || turns[0].Number != 1 ||
		turns[0].SentAt != "2026-01-01T09:41:00Z" || turns[0].DurationMs != 83000 ||
		turns[1].Number != 2 {
		t.Fatalf("user turns = %+v", turns)
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

func TestLoadCleansUserTurns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	data := `{"version":1,"sessions":[{"agentId":"pi","sessionId":"s1","userTurns":[
		{"number":1,"sentAt":" 2026-01-01T09:41:00Z ","durationMs":83000},
		{"number":0,"sentAt":"2026-01-01T09:42:00Z"},
		{"number":-2,"sentAt":"2026-01-01T09:43:00Z"},
		{"number":2},
		{"number":1,"sentAt":"2026-01-01T09:44:00Z"},
		{"number":3,"sentAt":"2026-01-01T09:45:00Z","durationMs":-5}
	]}]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := Open(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Sessions) != 1 {
		t.Fatalf("sessions = %+v", file.Sessions)
	}
	turns := file.Sessions[0].UserTurns
	if len(turns) != 2 {
		t.Fatalf("user turns = %+v, want 2 valid entries", turns)
	}
	if turns[0] != (UserTurn{Number: 1, SentAt: "2026-01-01T09:41:00Z", DurationMs: 83000}) ||
		turns[1] != (UserTurn{Number: 3, SentAt: "2026-01-01T09:45:00Z"}) {
		t.Errorf("user turns = %+v", turns)
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
