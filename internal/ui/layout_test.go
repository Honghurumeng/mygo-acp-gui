package ui

import (
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
)

func newLayoutApp(t *testing.T) (*App, *mui.Tester) {
	t.Helper()
	app := New(Options{Version: "test", Workspace: t.TempDir(), Agents: []agent.LaunchSpec{}})
	tt := mui.NewTester(app.View, 1200, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	return app, tt
}

func TestPaneWidthsDefaultAndClamp(t *testing.T) {
	app := &App{}
	if got := app.sidebarPaneWidth(1200); got != sidebarDefaultWidth {
		t.Errorf("default sidebar width = %v, want %v", got, sidebarDefaultWidth)
	}
	if got := app.rightPaneWidth(1200); got != rightPanelDefaultWidth {
		t.Errorf("default right panel width = %v, want %v", got, rightPanelDefaultWidth)
	}

	// A dragged width is kept while the window has room for it.
	app.SidebarWidth, app.RightPanelWidth = 380, 420
	if got := app.sidebarPaneWidth(1200); got != 380 {
		t.Errorf("dragged sidebar width = %v, want 380", got)
	}
	if got := app.rightPaneWidth(1200); got != 420 {
		t.Errorf("dragged right panel width = %v, want 420", got)
	}

	// The caps keep the conversation the widest column on a large window.
	app.SidebarWidth, app.RightPanelWidth = 1000, 1000
	if got := app.sidebarPaneWidth(1600); got != sidebarMaxWidthCap {
		t.Errorf("sidebar width on a wide window = %v, want %v", got, sidebarMaxWidthCap)
	}
	if got := app.rightPaneWidth(1600); got != rightPanelMaxWidthCap {
		t.Errorf("right panel width on a wide window = %v, want %v", got, rightPanelMaxWidthCap)
	}

	// A small window clamps to its share until the minimum wins.
	if got, want := app.sidebarPaneWidth(900), float32(315); got != want {
		t.Errorf("sidebar width in a 900 DIP window = %v, want %v", got, want)
	}
	if got, want := app.rightPaneWidth(900), float32(405); got != want {
		t.Errorf("right panel width in a 900 DIP window = %v, want %v", got, want)
	}
	if got := app.sidebarPaneWidth(400); got != sidebarMinWidth {
		t.Errorf("sidebar width in a 400 DIP window = %v, want %v", got, sidebarMinWidth)
	}
	if got := app.rightPaneWidth(400); got != rightPanelMinWidth {
		t.Errorf("right panel width in a 400 DIP window = %v, want %v", got, rightPanelMinWidth)
	}
}

func TestNormalizePaneWidths(t *testing.T) {
	for _, test := range []struct {
		width float32
		want  float32
	}{
		{0, 0},
		{100, 0},
		{380, 380},
		{1000, 460},
	} {
		if got := normalizeSidebarWidth(test.width); got != test.want {
			t.Errorf("normalizeSidebarWidth(%v) = %v, want %v", test.width, got, test.want)
		}
	}
	for _, test := range []struct {
		width float32
		want  float32
	}{
		{0, 0},
		{100, 0},
		{420, 420},
		{1000, 560},
	} {
		if got := normalizeRightPanelWidth(test.width); got != test.want {
			t.Errorf("normalizeRightPanelWidth(%v) = %v, want %v", test.width, got, test.want)
		}
	}
}

func TestSidebarDragsWidth(t *testing.T) {
	app, tt := newLayoutApp(t)
	tt.Frame()
	width := app.sidebarPaneWidth(1200)
	x, y := width+3, float32(300)
	tt.Press(x, y)
	tt.Move(x+8, y)
	tt.Frame()
	tt.Move(x+60, y)
	tt.Frame()
	tt.Release(x+60, y)
	tt.Frame()
	if app.SidebarWidth < width+50 {
		t.Fatalf("dragging the sidebar edge right did not widen it: %v -> %v", width, app.SidebarWidth)
	}
	if got := app.sidebarPaneWidth(1200); got != app.SidebarWidth {
		t.Fatalf("sidebar shows %v while the setting is %v", got, app.SidebarWidth)
	}
}

func TestRightPanelDragsWidth(t *testing.T) {
	app, tt := newLayoutApp(t)
	tt.Frame()
	if !app.GitPanelOpen {
		t.Fatal("right panel starts closed")
	}
	width := app.rightPaneWidth(1200)
	x := 1200 - rightRailWidth - width + 3
	tt.Press(x, 300)
	tt.Move(x-8, 300)
	tt.Frame()
	tt.Move(x-60, 300)
	tt.Frame()
	tt.Release(x-60, 300)
	tt.Frame()
	if app.RightPanelWidth < width+50 {
		t.Fatalf("dragging the panel edge left did not widen it: %v -> %v", width, app.RightPanelWidth)
	}
	if got := app.rightPaneWidth(1200); got != app.RightPanelWidth {
		t.Fatalf("right panel shows %v while the setting is %v", got, app.RightPanelWidth)
	}
}

func TestPaneWidthsPersist(t *testing.T) {
	dataDir := t.TempDir()
	workspace := t.TempDir()
	app := New(Options{Version: "test", Workspace: workspace, DataDir: dataDir, Agents: []agent.LaunchSpec{}})
	app.SidebarWidth = 340
	app.RightPanelWidth = 420
	app.persistSettings()
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})

	restarted := New(Options{Version: "test", Workspace: workspace, DataDir: dataDir, Agents: []agent.LaunchSpec{}})
	t.Cleanup(func() {
		restarted.Shutdown()
		restarted.Flush()
	})
	if restarted.SidebarWidth != 340 {
		t.Errorf("restored sidebar width = %v, want 340", restarted.SidebarWidth)
	}
	if restarted.RightPanelWidth != 420 {
		t.Errorf("restored right panel width = %v, want 420", restarted.RightPanelWidth)
	}
}

func TestPaneDoubleClickResetsWidth(t *testing.T) {
	app, tt := newLayoutApp(t)
	app.SidebarWidth = 400
	app.RightPanelWidth = 450
	tt.Frame()

	x, y := app.sidebarPaneWidth(1200)+3, float32(300)
	tt.ClickAt(x, y)
	tt.ClickAt(x, y)
	tt.Frame()
	if app.SidebarWidth != sidebarDefaultWidth {
		t.Errorf("double click left the sidebar at %v, want %v", app.SidebarWidth, sidebarDefaultWidth)
	}

	x = 1200 - rightRailWidth - app.rightPaneWidth(1200) + 3
	tt.ClickAt(x, y)
	tt.ClickAt(x, y)
	tt.Frame()
	if app.RightPanelWidth != rightPanelDefaultWidth {
		t.Errorf("double click left the right panel at %v, want %v", app.RightPanelWidth, rightPanelDefaultWidth)
	}
}

func TestSidebarGripFollowsSidebarOpen(t *testing.T) {
	app, tt := newLayoutApp(t)
	app.SidebarOpen = false
	tt.Frame()
	// The conversation now starts at the window's left edge; a drag there
	// must not resize a sidebar that is not shown.
	tt.Press(3, 300)
	tt.Move(80, 300)
	tt.Frame()
	tt.Release(80, 300)
	tt.Frame()
	if app.SidebarWidth != 0 {
		t.Fatalf("dragging with the sidebar closed set width %v", app.SidebarWidth)
	}
}
