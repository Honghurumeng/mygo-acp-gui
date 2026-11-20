package ui

import (
	"os"
	"path/filepath"
	"testing"

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
