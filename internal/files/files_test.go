package files

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadDirSortsFoldersFirstAndSkipsGit(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "beta.txt"), "b")
	mustWrite(t, filepath.Join(root, "Alpha.txt"), "a")
	mustWrite(t, filepath.Join(root, "docs", "readme.md"), "r")
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	entries, err := ReadDir(root, "")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, entry := range entries {
		got = append(got, entry.Path)
	}
	want := []string{"docs", "Alpha.txt", "beta.txt"}
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entries = %v, want %v", got, want)
		}
	}
	if !entries[0].Dir || entries[1].Dir {
		t.Fatalf("kind = %+v", entries)
	}

	nested, err := ReadDir(root, "docs")
	if err != nil {
		t.Fatal(err)
	}
	if len(nested) != 1 || nested[0].Path != "docs/readme.md" {
		t.Fatalf("nested = %+v", nested)
	}
}

func TestCreateRenameMoveAndRemove(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}

	file, err := Create(root, "", "notes.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if file != "notes.txt" {
		t.Fatalf("created %q", file)
	}
	if _, err := os.Stat(filepath.Join(root, "notes.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(root, "", "notes.txt", false); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate create error = %v", err)
	}

	folder, err := Create(root, "src", "nested", true)
	if err != nil {
		t.Fatal(err)
	}
	if folder != "src/nested" {
		t.Fatalf("created folder %q", folder)
	}

	renamed, err := Rename(root, "notes.txt", "journal.txt")
	if err != nil {
		t.Fatal(err)
	}
	if renamed != "journal.txt" {
		t.Fatalf("renamed to %q", renamed)
	}

	moved, err := Move(root, "journal.txt", "src/nested")
	if err != nil {
		t.Fatal(err)
	}
	if moved != "src/nested/journal.txt" {
		t.Fatalf("moved to %q", moved)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "nested", "journal.txt")); err != nil {
		t.Fatal(err)
	}

	if _, err := Move(root, "src", "src/nested"); !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("move into itself error = %v", err)
	}
	if _, err := Move(root, "src/nested/journal.txt", "src/nested"); err != nil {
		t.Fatalf("move into the same folder: %v", err)
	}

	if err := Remove(root, "src/nested/journal.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "nested", "journal.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file survived remove: %v", err)
	}
	if err := Remove(root, "src"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "src")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("folder survived remove: %v", err)
	}
}

func TestNamesAndPathsStayInsideWorkspace(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"", "..", ".", "a/b", `a\b`, ".git", ".GIT"} {
		if _, err := CleanName(name); err == nil {
			t.Errorf("CleanName(%q) accepted", name)
		}
	}
	if name, err := CleanName("  spaced.txt  "); err != nil || name != "spaced.txt" {
		t.Fatalf("CleanName trimmed = %q, %v", name, err)
	}
	if _, err := Resolve(root, "../outside"); !errors.Is(err, ErrOutsideWorkspace) {
		t.Fatalf("escape error = %v", err)
	}
	if _, err := Rename(root, "", "x"); !errors.Is(err, ErrWorkspaceRoot) {
		t.Fatalf("rename root error = %v", err)
	}
	if err := Remove(root, ""); !errors.Is(err, ErrWorkspaceRoot) {
		t.Fatalf("remove root error = %v", err)
	}
	if _, err := Move(root, "", "src"); !errors.Is(err, ErrWorkspaceRoot) {
		t.Fatalf("move root error = %v", err)
	}
}

func TestSymlinkFolderIsListedAsFile(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	entries, err := ReadDir(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Dir {
		t.Fatalf("symlink entry = %+v", entries)
	}
}

func mustWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
