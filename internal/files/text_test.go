package files

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadAndSaveText(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes", "todo.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	text, err := ReadText(root, "notes/todo.md")
	if err != nil {
		t.Fatalf("ReadText: %v", err)
	}
	if text != "old\n" {
		t.Fatalf("text = %q", text)
	}

	if err := SaveText(root, "notes/todo.md", "new\nlines\n"); err != nil {
		t.Fatalf("SaveText: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "new\nlines\n" {
		t.Fatalf("saved body = %q", body)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %o, want 640", info.Mode().Perm())
	}
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".acp-gui-edit-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) > 0 {
		t.Errorf("temporary files left behind: %v", leftovers)
	}
}

func TestTextRejectsBinaryFoldersAndEscapes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "blob.bin"), []byte("a\x00b"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte(strings.Repeat("x", MaxTextSize+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlinked := true
	if err := os.Symlink(filepath.Join(root, "large.txt"), filepath.Join(root, "link.txt")); err != nil {
		symlinked = false
		t.Logf("symlink not available: %v", err)
	}

	if _, err := ReadText(root, "blob.bin"); !errors.Is(err, ErrBinary) {
		t.Errorf("binary read error = %v", err)
	}
	if _, err := ReadText(root, "large.txt"); !errors.Is(err, ErrTextTooLarge) {
		t.Errorf("large read error = %v", err)
	}
	if _, err := ReadText(root, "dir"); err == nil {
		t.Error("reading a folder should fail")
	}
	if symlinked {
		if _, err := ReadText(root, "link.txt"); !errors.Is(err, ErrSymlink) {
			t.Errorf("symlink read error = %v", err)
		}
	}
	if _, err := ReadText(root, "../outside.txt"); !errors.Is(err, ErrOutsideWorkspace) {
		t.Errorf("escape read error = %v", err)
	}
	if err := SaveText(root, "missing.txt", "text"); err == nil {
		t.Error("saving a missing file should fail")
	}
	if _, err := os.Stat(filepath.Join(root, "missing.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Error("SaveText recreated a missing file")
	}
	if err := SaveText(root, "blob.bin", "a\x00b"); !errors.Is(err, ErrBinary) {
		t.Errorf("binary save error = %v", err)
	}
}
