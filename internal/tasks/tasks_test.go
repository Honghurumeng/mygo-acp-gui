package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := Open(filepath.Join(dir, "tasks.json"))
	file := File{Version: CurrentVersion, Tasks: []Task{
		{ID: "b", Workspace: "/tmp/ws", Title: "Second", Status: StatusPending},
		{ID: "a", Workspace: "/tmp/ws", Title: "First", Status: StatusComplete, SummaryPath: "tasks/a-first.md"},
		{ID: "c", Workspace: "/tmp/ws", Title: "Failed", Status: StatusComplete, Result: ResultFailure, SummaryPath: "tasks/c-failed.md"},
	}}
	if err := store.Save(file); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Tasks) != 3 || loaded.Tasks[0].ID != "b" || loaded.Tasks[1].ID != "a" {
		t.Fatalf("loaded tasks = %+v", loaded.Tasks)
	}
	if loaded.Tasks[1].Status != StatusComplete || loaded.Tasks[1].Result != ResultPass {
		t.Fatalf("status/result = %q/%q, want complete/pass for an old row", loaded.Tasks[1].Status, loaded.Tasks[1].Result)
	}
	if loaded.Tasks[2].Result != ResultFailure {
		t.Fatalf("failure result = %q", loaded.Tasks[2].Result)
	}
	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("tasks.json mode = %v", info.Mode().Perm())
	}
}

func TestStoreLoadToleratesMissingAndNormalizesRows(t *testing.T) {
	dir := t.TempDir()
	store := Open(filepath.Join(dir, "tasks.json"))
	file, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 0 {
		t.Fatalf("missing file loaded tasks: %+v", file.Tasks)
	}
	data := `{"version":1,"tasks":[
		{"id":"keep","workspace":"/tmp/ws","title":"Keep","status":"future"},
		{"workspace":"/tmp/ws","title":"No id"},
		{"id":"keep","workspace":"/tmp/ws","title":"Duplicate"}
	]}`
	if err := os.WriteFile(store.Path(), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 1 || file.Tasks[0].ID != "keep" {
		t.Fatalf("tasks = %+v", file.Tasks)
	}
	if file.Tasks[0].Status != StatusPending {
		t.Fatalf("unknown status became %q", file.Tasks[0].Status)
	}
}

func TestWriteAndReadSummary(t *testing.T) {
	workspace := t.TempDir()
	task := Task{ID: "abc", Title: "Fix the parser / add tests", Workspace: workspace}
	rel, err := WriteSummary(workspace, task, "# Done\n")
	if err != nil {
		t.Fatal(err)
	}
	if rel != "tasks/abc-fix-the-parser-add-tests.md" {
		t.Fatalf("relative path = %q", rel)
	}
	content, err := ReadSummary(workspace, rel)
	if err != nil {
		t.Fatal(err)
	}
	if content != "# Done\n" {
		t.Fatalf("content = %q", content)
	}
	path := ResolveSummary(workspace, rel)
	if !strings.HasPrefix(path, workspace) {
		t.Fatalf("resolved path = %q", path)
	}
	if ResolveSummary(workspace, "../outside.md") != "" {
		t.Fatal("path outside tasks/ was accepted")
	}
	if ResolveSummary(workspace, "/etc/passwd") != "" {
		t.Fatal("absolute summary path was accepted")
	}
}

func TestSlugKeepsUnicodeLetters(t *testing.T) {
	if got := slug("实现 Task 面板"); got != "实现-task-面板" {
		t.Fatalf("slug = %q", got)
	}
	if got := slug("///"); got != "task" {
		t.Fatalf("empty slug = %q", got)
	}
}
