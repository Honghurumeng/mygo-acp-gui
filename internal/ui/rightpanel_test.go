package ui

import (
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
)

// TestRightRailSwitchesRightColumn covers the icon strip at the window's
// right edge: one icon button per tab, stacked, and both reachable while the
// column itself is closed.
func TestRightRailSwitchesRightColumn(t *testing.T) {
	app := New(Options{Version: "test", Workspace: t.TempDir(), DataDir: t.TempDir(), Agents: []agent.LaunchSpec{}})
	tt := mui.NewTester(app.View, 1200, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	tt.Frame()

	if !app.GitPanelOpen || app.RightTab != rightTabGit {
		t.Fatalf("right column did not start on Git: open=%v tab=%q", app.GitPanelOpen, app.RightTab)
	}
	if !tt.HasText("Hide Git panel") || !tt.HasText("Show Tasks panel") {
		t.Fatalf("rail does not show both tabs; texts: %q", tt.Texts())
	}
	git, ok := tt.Find("Hide Git panel")
	if !ok {
		t.Fatalf("Git rail button missing; texts: %q", tt.Texts())
	}
	tasks, ok := tt.Find("Show Tasks panel")
	if !ok {
		t.Fatalf("Tasks rail button missing; texts: %q", tt.Texts())
	}
	if git.X != tasks.X {
		t.Errorf("rail buttons not in one column: git X=%v tasks X=%v", git.X, tasks.X)
	}
	if tasks.Y <= git.Y {
		t.Errorf("rail buttons not stacked: git Y=%v tasks Y=%v", git.Y, tasks.Y)
	}
	// The rail is the narrow column at the window's right edge, not the
	// conversation header the text buttons used to sit in.
	if git.X < 1100 {
		t.Errorf("rail button at X=%v, not at the right edge", git.X)
	}

	if err := tt.Click("Hide Git panel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.GitPanelOpen {
		t.Fatal("rail button did not close the right column")
	}
	for _, text := range tt.Texts() {
		if text == "Git" || text == "Tasks" {
			t.Fatalf("closed column still draws %q; texts: %q", text, tt.Texts())
		}
	}
	if !tt.HasText("Show Git panel") {
		t.Fatalf("rail lost the Git button while closed; texts: %q", tt.Texts())
	}

	if err := tt.Click("Show Tasks panel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.GitPanelOpen || app.RightTab != rightTabTasks {
		t.Fatalf("rail did not open Tasks: open=%v tab=%q", app.GitPanelOpen, app.RightTab)
	}
	if _, ok := tt.Find("Tasks"); !ok {
		t.Fatalf("Tasks panel title missing; texts: %q", tt.Texts())
	}
	if !tt.HasText("Hide Tasks panel") || !tt.HasText("Show Git panel") {
		t.Fatalf("rail did not follow the switch; texts: %q", tt.Texts())
	}
}
