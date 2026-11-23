package files

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadImageAcceptsBinaryAndRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	picture := []byte("\x89PNG\r\n\x1a\n\x00\x00binary")
	if err := os.WriteFile(filepath.Join(root, "shot.png"), picture, 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := ReadImage(root, "shot.png")
	if err != nil {
		t.Fatalf("ReadImage: %v", err)
	}
	if !bytes.Equal(data, picture) {
		t.Fatalf("image bytes = %q", data)
	}

	if err := os.WriteFile(filepath.Join(root, "large.png"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(filepath.Join(root, "large.png"), MaxImageSize+1); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlinked := true
	if err := os.Symlink(filepath.Join(root, "shot.png"), filepath.Join(root, "link.png")); err != nil {
		symlinked = false
		t.Logf("symlink not available: %v", err)
	}

	if _, err := ReadImage(root, "large.png"); !errors.Is(err, ErrImageTooLarge) {
		t.Errorf("large image error = %v", err)
	}
	if _, err := ReadImage(root, "dir"); err == nil {
		t.Error("reading a folder should fail")
	}
	if symlinked {
		if _, err := ReadImage(root, "link.png"); !errors.Is(err, ErrSymlink) {
			t.Errorf("symlink image error = %v", err)
		}
	}
	if _, err := ReadImage(root, "../outside.png"); !errors.Is(err, ErrOutsideWorkspace) {
		t.Errorf("escape image error = %v", err)
	}
}
