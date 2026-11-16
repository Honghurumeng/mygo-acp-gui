package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	mui "github.com/egoist/mygo/ui"
)

func TestFileViewerViewsEditsAndSaves(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "notes.md")
	if err := os.WriteFile(path, []byte("# Hello\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app, tt := newFilePanelApp(t, workspace)
	showFilePanel(t, app, tt)
	if err := tt.Click("File notes.md"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Files.ViewerOpen && !app.Files.ViewerBusy && app.Files.ViewerBody != ""
	})
	if app.Files.ViewerMode != viewerView {
		t.Fatalf("file opened in mode %v, want view", app.Files.ViewerMode)
	}
	tt.Frame()
	if !tt.HasText("Hello") {
		t.Errorf("rendered Markdown is missing; texts: %q", tt.Texts())
	}

	if err := tt.Click("Edit file"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.Files.ViewerMode != viewerEdit {
		t.Fatalf("mode = %v, want edit", app.Files.ViewerMode)
	}
	if err := tt.Click("Show file content"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.Files.ViewerMode != viewerView {
		t.Fatalf("mode = %v, want view", app.Files.ViewerMode)
	}

	app.saveFileViewer("# Edited\n")
	waitFor(t, app, tt, func() bool {
		return !app.Files.ViewerSaving && app.Files.ViewerBody == "# Edited\n"
	})
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "# Edited\n" {
		t.Fatalf("saved file = %q", body)
	}
	if !tt.HasText("Saved.") {
		t.Errorf("saved notice is missing; texts: %q", tt.Texts())
	}

	if err := tt.Click("Close"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return !app.Files.ViewerOpen && app.Files.ViewerPath == "" })
}

func TestFileViewerRejectsBinaryFile(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "blob.bin"), []byte("a\x00b"), 0o644); err != nil {
		t.Fatal(err)
	}
	app, tt := newFilePanelApp(t, workspace)
	showFilePanel(t, app, tt)
	if err := tt.Click("File blob.bin"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Files.ViewerOpen && !app.Files.ViewerBusy && app.Files.ViewerErr != ""
	})
	if !strings.Contains(app.Files.ViewerErr, "binary") {
		t.Fatalf("viewer error = %q", app.Files.ViewerErr)
	}
	tt.Frame()
	if !tt.HasText("Cannot show this file") {
		t.Errorf("binary error is missing from the card; texts: %q", tt.Texts())
	}
	if _, ok := tt.Find("Edit file"); ok {
		t.Error("binary file still offers editing")
	}
}

func TestFileViewerDragsWidth(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "notes.txt"), []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app, tt := newFilePanelApp(t, workspace)
	showFilePanel(t, app, tt)
	if err := tt.Click("File notes.txt"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Files.ViewerOpen && !app.Files.ViewerBusy && app.Files.ViewerBody != ""
	})
	tt.Frame()
	closeRect, ok := tt.Find("Close")
	if !ok {
		t.Fatalf("viewer has no Close button; texts: %q", tt.Texts())
	}
	width := app.viewerCardWidth(1200)
	cardRight := closeRect.X + closeRect.W + 12
	x := cardRight - width + 2
	y := closeRect.Y + closeRect.H/2
	tt.Press(x, y)
	tt.Move(x-8, y)
	tt.Frame()
	tt.Move(x-80, y)
	tt.Frame()
	tt.Release(x-80, y)
	tt.Frame()
	if app.ViewerWidth <= width {
		t.Fatalf("dragging the left edge left did not widen the card: width %v -> %v", width, app.ViewerWidth)
	}
}

func TestFileViewerKeyboardSave(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "hello.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app, tt := newFilePanelApp(t, workspace)
	showFilePanel(t, app, tt)
	if err := tt.Click("File hello.txt"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Files.ViewerOpen && !app.Files.ViewerBusy && app.Files.ViewerBody == "hello\n"
	})
	if err := tt.Click("Edit file"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	closeRect, ok := tt.Find("Close")
	if !ok {
		t.Fatalf("viewer has no Close button; texts: %q", tt.Texts())
	}
	// Click the first line of the editor, then type and save with the
	// platform shortcut.
	tt.ClickAt(closeRect.X-300, closeRect.Y+70)
	tt.Frame()
	tt.Type("X")
	tt.Frame()
	tt.Key(mui.Cmd, mui.KeyS)
	waitFor(t, app, tt, func() bool {
		return !app.Files.ViewerSaving && strings.Contains(app.Files.ViewerBody, "X")
	})
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "X") {
		t.Fatalf("keyboard save did not write the edit: %q", body)
	}
}
