package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileViewerRendersMermaid(t *testing.T) {
	workspace := t.TempDir()
	source := "```mermaid\ngraph TD\n  A[Start] --> B{OK?}\n  B -->|yes| C([Done])\n```\n"
	if err := os.WriteFile(filepath.Join(workspace, "diagram.md"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	app, tt := newFilePanelApp(t, workspace)
	showFilePanel(t, app, tt)
	if err := tt.Click("File diagram.md"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Files.ViewerOpen && !app.Files.ViewerBusy && app.Files.ViewerBody != ""
	})
	tt.Frame()

	if _, ok := tt.Find("Mermaid diagram"); !ok {
		t.Errorf("diagram is missing; texts: %q", tt.Texts())
	}
	if tt.HasText("graph TD") {
		t.Errorf("Mermaid source is still drawn as code; texts: %q", tt.Texts())
	}
}

func TestFileViewerFallsBackToMermaidSource(t *testing.T) {
	workspace := t.TempDir()
	source := "```mermaid\nnotADiagram\n  A --> B\n```\n"
	if err := os.WriteFile(filepath.Join(workspace, "broken.md"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	app, tt := newFilePanelApp(t, workspace)
	showFilePanel(t, app, tt)
	if err := tt.Click("File broken.md"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Files.ViewerOpen && !app.Files.ViewerBusy && app.Files.ViewerBody != ""
	})
	tt.Frame()

	if !tt.HasText("notADiagram") {
		t.Errorf("source is not shown when the diagram cannot be drawn; texts: %q", tt.Texts())
	}
	if _, ok := tt.Find("Mermaid diagram"); ok {
		t.Error("an unsupported diagram must not show an empty image")
	}
}
