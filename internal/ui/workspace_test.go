package ui

import (
	"path/filepath"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/settings"
)

func TestRemoveWorkspaceIsSoftAndCanBeRestored(t *testing.T) {
	app, tt := newFakeApp(t)
	workspace := app.Workspace
	label := filepath.Base(workspace)
	tt.Frame()

	if err := tt.Click("Workspace actions for " + label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Remove workspace"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.workspaceRemoved(workspace) && len(app.shownWorkspaces()) == 0
	})

	// Soft delete: the group is hidden but the path and its sessions stay.
	known := false
	for _, item := range app.Workspaces {
		if item == workspace {
			known = true
			break
		}
	}
	if !known {
		t.Fatalf("workspace dropped from the list: %+v", app.Workspaces)
	}
	dataDir := filepath.Dir(app.archive.Path())
	file := loadPersistedSettings(t, dataDir)
	if len(file.RemovedWorkspaces) != 1 || file.RemovedWorkspaces[0] != workspace {
		t.Fatalf("removedWorkspaces = %+v", file.RemovedWorkspaces)
	}
	if len(app.savedSessionsFor(workspace)) != 1 {
		t.Errorf("saved sessions were deleted with the workspace: %+v", app.SavedSessions)
	}
	if !tt.HasText("Show removed (1)") {
		t.Errorf("removed filter missing; texts: %q", tt.Texts())
	}

	// The record survives a restart, so a reload cannot bring the group back.
	restarted := New(Options{Version: "test", Workspace: workspace, DataDir: dataDir, Agents: app.Agents})
	restarted.MD.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
	tt2 := mui.NewTester(restarted.View, 1060, 760)
	t.Cleanup(func() {
		restarted.Shutdown()
		restarted.Flush()
	})
	tt2.Frame()
	if !restarted.workspaceRemoved(workspace) {
		t.Error("removed workspace was restored by restart")
	}
	if len(restarted.shownWorkspaces()) != 0 {
		t.Errorf("shown workspaces after restart = %+v", restarted.shownWorkspaces())
	}

	// Show removed -> Restore brings the group back.
	if err := tt.Click("Show removed (1)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("[removed] " + label) {
		t.Fatalf("removed group not shown; texts: %q", tt.Texts())
	}
	if err := tt.Click("Workspace actions for " + label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Restore workspace"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !app.workspaceRemoved(workspace) && len(app.shownWorkspaces()) == 1
	})
	file = loadPersistedSettings(t, dataDir)
	if len(file.RemovedWorkspaces) != 0 {
		t.Errorf("removedWorkspaces after restore = %+v", file.RemovedWorkspaces)
	}
}

func TestRemovedWorkspaceRecordReloads(t *testing.T) {
	dataDir := t.TempDir()
	workspace := t.TempDir()
	store := settings.Open(filepath.Join(dataDir, "settings.json"))
	if err := store.Save(settings.File{
		Version:           settings.CurrentVersion,
		Workspaces:        []string{workspace},
		RemovedWorkspaces: []string{workspace},
	}); err != nil {
		t.Fatal(err)
	}
	app := New(Options{Version: "test", Workspace: workspace, DataDir: dataDir, Agents: []agent.LaunchSpec{}})
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	if !app.workspaceRemoved(workspace) {
		t.Fatalf("removed workspace not loaded: %+v", app.RemovedWorkspaces)
	}
	if len(app.shownWorkspaces()) != 0 {
		t.Errorf("shown workspaces = %+v", app.shownWorkspaces())
	}
	app.restoreWorkspace(workspace)
	if app.workspaceRemoved(workspace) || len(app.shownWorkspaces()) != 1 {
		t.Errorf("restore failed: removed=%v shown=%+v", app.workspaceRemoved(workspace), app.shownWorkspaces())
	}
}
