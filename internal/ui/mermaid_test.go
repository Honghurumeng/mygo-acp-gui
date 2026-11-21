package ui

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mui "github.com/egoist/mygo/ui"
)

func openMermaidFile(t *testing.T, name, fence string) (*App, *mui.Tester) {
	t.Helper()
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, name), []byte(fence), 0o644); err != nil {
		t.Fatal(err)
	}
	app, tt := newFilePanelApp(t, workspace)
	showFilePanel(t, app, tt)
	if err := tt.Click("File " + name); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Files.ViewerOpen && !app.Files.ViewerBusy && app.Files.ViewerBody != ""
	})
	tt.Frame()
	return app, tt
}

func TestFileViewerRendersMermaid(t *testing.T) {
	_, tt := openMermaidFile(t, "diagram.md",
		"```mermaid\ngraph TD\n  A[Start] --> B{OK?}\n  B -->|yes| C([Done])\n```\n")

	if _, ok := tt.Find("Mermaid diagram"); !ok {
		t.Errorf("diagram is missing; texts: %q", tt.Texts())
	}
	if tt.HasText("graph TD") {
		t.Errorf("Mermaid source is still drawn as code; texts: %q", tt.Texts())
	}
}

func TestFileViewerRendersMermaidLegacyEdgeLabel(t *testing.T) {
	_, tt := openMermaidFile(t, "flow.md",
		"```mermaid\nflowchart LR\n  subgraph Host[Host]\n    A[Views<br/>app/views.py] -- 更新/写入 --> B[(DB)]\n  end\n```\n")

	if _, ok := tt.Find("Mermaid diagram"); !ok {
		t.Errorf("diagram is missing; texts: %q", tt.Texts())
	}
	if tt.HasText("flowchart LR") || tt.HasText("更新/写入") {
		t.Errorf("Mermaid source is still drawn as code; texts: %q", tt.Texts())
	}
}

func TestFileViewerRendersMermaidClassDiagram(t *testing.T) {
	_, tt := openMermaidFile(t, "class.md",
		"```mermaid\nclassDiagram\n  class Animal {\n    +String name\n    +makeSound()\n  }\n  Animal <|-- Duck\n```\n")

	if _, ok := tt.Find("Mermaid diagram"); !ok {
		t.Errorf("diagram is missing; texts: %q", tt.Texts())
	}
	if tt.HasText("classDiagram") {
		t.Errorf("Mermaid source is still drawn as code; texts: %q", tt.Texts())
	}
}

func TestFileViewerFallsBackToMermaidSource(t *testing.T) {
	_, tt := openMermaidFile(t, "broken.md",
		"```mermaid\nnotADiagram\n  A --> B\n```\n")

	if !tt.HasText("notADiagram") {
		t.Errorf("source is not shown when the diagram cannot be drawn; texts: %q", tt.Texts())
	}
	if _, ok := tt.Find("Mermaid diagram"); ok {
		t.Error("an unsupported diagram must not show an empty image")
	}
}

// A Mermaid diagram opens enlarged like an attachment: the wheel zooms in on
// the point under the pointer, the percentage resets it, the SVG stays
// downloadable, and Escape closes it.
func TestMermaidDiagramOpensThePreview(t *testing.T) {
	app, tt := openMermaidFile(t, "diagram.md",
		"```mermaid\ngraph TD\n  A[Start] --> B{OK?}\n  B -->|yes| C([Done])\n```\n")
	if _, ok := tt.Find("Mermaid diagram"); !ok {
		t.Fatalf("diagram is missing; texts: %q", tt.Texts())
	}
	if err := tt.Click("Mermaid diagram"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.ImageOpen || app.DiagramPreview == nil {
		t.Fatal("clicking the diagram did not open the preview")
	}
	if app.ImagePreview != nil {
		t.Error("the diagram's preview still shows an attachment")
	}
	pic, ok := tt.Find("Image preview")
	if !ok {
		t.Fatalf("enlarged diagram is missing; texts: %q", tt.Texts())
	}
	tt.Wheel(pic.X+pic.W/2, pic.Y+pic.H/2, 0, -40)
	tt.Frame()
	if app.ImageView.Zoom <= 1 {
		t.Fatalf("the wheel did not zoom in: %v", app.ImageView.Zoom)
	}
	if !tt.HasText("110%") {
		t.Errorf("toolbar does not show the zoom; texts: %q", tt.Texts())
	}
	// A notch redraws the SVG at the nearest step, not at every zoom it
	// shows, which is what made zooming stutter; once the zoom rests, the
	// picture is drawn again at the zoom shown.
	if app.ImageView.Sharp != 1 {
		t.Errorf("the wheel redrew the picture at %v for a zoom of %v", app.ImageView.Sharp, app.ImageView.Zoom)
	}
	time.Sleep(previewSharpDelay + 20*time.Millisecond)
	tt.Frame()
	if math.Abs(float64(app.ImageView.Sharp-app.ImageView.Zoom)) > 1e-4 {
		t.Errorf("the picture did not sharpen at rest: %v at %v", app.ImageView.Sharp, app.ImageView.Zoom)
	}
	// Dragging pans the picture once it is larger than its box.
	pic, _ = tt.Find("Image preview")
	before := app.ImageView
	tt.Press(pic.X+pic.W/2, pic.Y+pic.H/2)
	tt.Move(pic.X+pic.W/2-30, pic.Y+pic.H/2+10)
	tt.Frame()
	tt.Release(pic.X+pic.W/2-30, pic.Y+pic.H/2+10)
	tt.Frame()
	if app.ImageView.X >= before.X {
		t.Errorf("dragging did not pan the picture: %v -> %v", before.X, app.ImageView.X)
	}
	if err := tt.Click("110%"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.ImageView.Zoom != 1 {
		t.Errorf("the percentage did not reset the zoom: %v", app.ImageView.Zoom)
	}
	name, data, ok := app.previewFile()
	if !ok {
		t.Fatal("the preview has nothing to download")
	}
	if name != "diagram.svg" || !strings.Contains(string(data), "<svg") {
		t.Errorf("download source = %q with %d bytes", name, len(data))
	}
	tt.Key(0, mui.KeyEscape)
	tt.Frame()
	if app.ImageOpen || app.DiagramPreview != nil {
		t.Fatal("Escape did not close the preview")
	}
}

// The preview's modal keeps presses from the floating card under it: the card
// neither closes nor acts while the picture shows, and comes back whole once
// the preview does.
func TestPreviewKeepsPressesFromTheViewer(t *testing.T) {
	app, tt := openMermaidFile(t, "diagram.md",
		"```mermaid\ngraph TD\n  A[Start] --> B{OK?}\n```\n")
	if err := tt.Click("Mermaid diagram"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.ImageOpen || !app.Files.ViewerOpen {
		t.Fatalf("preview open = %v, viewer open = %v", app.ImageOpen, app.Files.ViewerOpen)
	}
	if _, ok := tt.Find("Show file content"); ok {
		t.Error("the viewer card still shows under the preview")
	}
	if err := tt.Click("Zoom in"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.ImageView.Zoom <= 1 {
		t.Errorf("the preview's button did not zoom: %v", app.ImageView.Zoom)
	}
	if !app.Files.ViewerOpen {
		t.Fatal("a press in the preview closed the viewer card")
	}
	tt.Key(0, mui.KeyEscape)
	tt.Frame()
	if app.ImageOpen {
		t.Fatal("Escape did not close the preview")
	}
	if !app.Files.ViewerOpen {
		t.Fatal("the viewer card did not come back")
	}
	if _, ok := tt.Find("Show file content"); !ok {
		t.Errorf("the viewer card did not come back; texts: %q", tt.Texts())
	}
}
