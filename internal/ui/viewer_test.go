package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
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

func TestFileViewerMarksItsTreeRow(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "notes.txt"), []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app, tt := newFilePanelApp(t, workspace)
	showFilePanel(t, app, tt)

	rowFill := func() color.RGBA {
		t.Helper()
		rect, ok := tt.Find("File notes.txt")
		if !ok {
			t.Fatalf("file row is missing; texts: %q", tt.Texts())
		}
		img := tt.Image()
		return img.RGBAAt(int(rect.X+rect.W-4), int(rect.Y+rect.H/2))
	}
	tt.Move(5, 5)
	tt.Frame()
	base := rowFill()
	if info, ok := tt.Access("File notes.txt"); !ok || info.Selected {
		t.Fatalf("unopened row is selected: %+v, found %v", info, ok)
	}

	if err := tt.Click("File notes.txt"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Files.ViewerOpen && !app.Files.ViewerBusy && app.Files.ViewerBody != ""
	})
	tt.Move(5, 5)
	tt.Frame()
	if info, ok := tt.Access("File notes.txt"); !ok || !info.Selected {
		t.Fatalf("viewed row is not selected: %+v, found %v", info, ok)
	}
	if selected := rowFill(); selected == base {
		t.Errorf("viewed row keeps the unselected fill %v", base)
	}

	if err := tt.Click("Close"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return !app.Files.ViewerOpen && app.Files.ViewerPath == "" })
	tt.Move(5, 5)
	tt.Frame()
	if info, ok := tt.Access("File notes.txt"); !ok || info.Selected {
		t.Errorf("closed row is still selected: %+v, found %v", info, ok)
	}
	if got := rowFill(); got != base {
		t.Errorf("closed row fill = %v, want the unselected %v", got, base)
	}
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

// writeViewerPNG writes a solid picture for the viewer to show.
func writeViewerPNG(t *testing.T, path string, width, height int, fill color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, fill)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// viewerImagePixel reads the middle of the picture the card draws.
func viewerImagePixel(t *testing.T, tt *mui.Tester) color.RGBA {
	t.Helper()
	rect, ok := tt.Find("Image")
	if !ok {
		t.Fatalf("viewer image is missing; texts: %q", tt.Texts())
	}
	return tt.Image().RGBAAt(int(rect.X+rect.W/2), int(rect.Y+rect.H/2))
}

// A bitmap opens as its picture, without the editor: an image is not text to
// save, and a click opens it enlarged like a pasted attachment.
func TestFileViewerShowsBitmapImage(t *testing.T) {
	workspace := t.TempDir()
	fill := color.RGBA{210, 40, 50, 255}
	writeViewerPNG(t, filepath.Join(workspace, "shot.png"), 80, 50, fill)

	app, tt := newFilePanelApp(t, workspace)
	showFilePanel(t, app, tt)
	if err := tt.Click("File shot.png"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Files.ViewerOpen && !app.Files.ViewerBusy && app.Files.ViewerImage != nil
	})
	if app.Files.ViewerBody != "" || app.Files.ViewerEditable {
		t.Fatalf("bitmap body = %q, editable = %v", app.Files.ViewerBody, app.Files.ViewerEditable)
	}
	tt.Frame()
	if _, ok := tt.Find("Edit file"); ok {
		t.Error("a bitmap still offers editing")
	}
	if got := viewerImagePixel(t, tt); got.R < 150 || got.G > 90 || got.B > 100 {
		t.Errorf("viewer drew %v, want the picture's red", got)
	}

	if err := tt.Click("Image"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.ImageOpen || app.ImagePreview == nil || app.DiagramPreview != nil {
		t.Fatal("clicking the picture did not open its enlarged preview")
	}
	name, data, ok := app.previewFile()
	if !ok || name != "shot.png" || !bytes.HasPrefix(data, []byte("\x89PNG")) {
		t.Fatalf("preview download = %q, %d bytes, ok %v", name, len(data), ok)
	}
	tt.Key(0, mui.KeyEscape)
	tt.Frame()
	if app.ImageOpen {
		t.Fatal("Escape did not close the preview")
	}
	if !app.Files.ViewerOpen {
		t.Fatal("closing the preview closed the viewer")
	}
}

// An SVG opens as its picture, not its source; Edit shows the text, and a save
// draws the saved document again.
func TestFileViewerShowsSVGAsPictureAndEditsSource(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "logo.svg")
	green := `<svg xmlns="http://www.w3.org/2000/svg" width="120" height="60">` +
		`<rect width="120" height="60" fill="#2e7d32"/></svg>`
	if err := os.WriteFile(path, []byte(green), 0o644); err != nil {
		t.Fatal(err)
	}

	app, tt := newFilePanelApp(t, workspace)
	showFilePanel(t, app, tt)
	if err := tt.Click("File logo.svg"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Files.ViewerOpen && !app.Files.ViewerBusy && app.Files.ViewerImage != nil
	})
	if !app.Files.ViewerEditable || !strings.Contains(app.Files.ViewerBody, "<svg") {
		t.Fatalf("SVG source is not editable: body %q", app.Files.ViewerBody)
	}
	tt.Frame()
	if tt.HasText("<svg") {
		t.Errorf("View draws the SVG source instead of the picture; texts: %q", tt.Texts())
	}
	if got := viewerImagePixel(t, tt); got.G < 80 || got.R > 90 {
		t.Errorf("viewer drew %v, want the SVG's green", got)
	}

	if err := tt.Click("Edit file"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.Files.ViewerMode != viewerEdit {
		t.Fatalf("mode = %v, want edit", app.Files.ViewerMode)
	}
	if _, ok := tt.Find("Image"); ok {
		t.Error("Edit still draws the picture instead of the source")
	}

	red := `<svg xmlns="http://www.w3.org/2000/svg" width="120" height="60">` +
		`<rect width="120" height="60" fill="#c62828"/></svg>`
	app.saveFileViewer(red)
	waitFor(t, app, tt, func() bool {
		return !app.Files.ViewerSaving && app.Files.ViewerBody == red
	})
	if err := tt.Click("Show file content"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := viewerImagePixel(t, tt); got.R < 150 || got.G > 90 {
		t.Errorf("viewer drew %v, want the saved red document", got)
	}
	if err := tt.Click("Image"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.ImageOpen || app.DiagramPreview == nil {
		t.Fatal("clicking the SVG did not open its enlarged preview")
	}
	name, data, ok := app.previewFile()
	if !ok || name != "logo.svg" || !bytes.Contains(data, []byte("#c62828")) {
		t.Fatalf("SVG preview download = %q, %d bytes, ok %v", name, len(data), ok)
	}
	tt.Key(0, mui.KeyEscape)
	tt.Frame()
	if app.ImageOpen {
		t.Fatal("Escape did not close the SVG preview")
	}
}

// MyGo not drawing a document leaves its source, where the user can fix it.
func TestFileViewerFallsBackToBrokenSVGSource(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "broken.svg"), []byte("<svg xmlns=\"http://www.w3.org/2000/svg\">"), 0o644); err != nil {
		t.Fatal(err)
	}
	app, tt := newFilePanelApp(t, workspace)
	showFilePanel(t, app, tt)
	if err := tt.Click("File broken.svg"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Files.ViewerOpen && !app.Files.ViewerBusy && app.Files.ViewerBody != ""
	})
	if app.Files.ViewerImage != nil || !app.Files.ViewerEditable {
		t.Fatalf("broken SVG image = %v, editable = %v", app.Files.ViewerImage, app.Files.ViewerEditable)
	}
	tt.Frame()
	if _, ok := tt.Find("Image"); ok {
		t.Error("a document MyGo cannot draw still shows an empty picture")
	}
}

// A document too large to edit is still shown as a picture, without an editor
// that could not save it.
func TestFileViewerShowsLargeSVGWithoutEditing(t *testing.T) {
	workspace := t.TempDir()
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="80" height="40">` +
		strings.Repeat("<!-- padding -->", 45000) +
		`<rect width="80" height="40" fill="#1565c0"/></svg>`
	if len(svg) <= 512<<10 {
		t.Fatalf("test SVG is only %d bytes", len(svg))
	}
	if err := os.WriteFile(filepath.Join(workspace, "large.svg"), []byte(svg), 0o644); err != nil {
		t.Fatal(err)
	}
	app, tt := newFilePanelApp(t, workspace)
	showFilePanel(t, app, tt)
	if err := tt.Click("File large.svg"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Files.ViewerOpen && !app.Files.ViewerBusy && app.Files.ViewerImage != nil
	})
	if app.Files.ViewerEditable || app.Files.ViewerBody != "" {
		t.Fatalf("large SVG editable = %v, body = %q", app.Files.ViewerEditable, app.Files.ViewerBody)
	}
	tt.Frame()
	if _, ok := tt.Find("Edit file"); ok {
		t.Error("a document beyond the editor's limit still offers editing")
	}
	if got := viewerImagePixel(t, tt); got.B < 150 || got.R > 80 {
		t.Errorf("viewer drew %v, want the large SVG's blue", got)
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
