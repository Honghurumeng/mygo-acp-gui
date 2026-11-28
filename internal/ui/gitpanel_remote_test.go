package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitPanelOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out := runGitPanelOutput(t, dir, args...)
	return strings.TrimSpace(out)
}

func initGitPanelRemote(t *testing.T) (workspace, origin string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	origin = t.TempDir()
	runGitPanelCommand(t, origin, "init", "--bare", "-q")

	workspace = t.TempDir()
	runGitPanelCommand(t, workspace, "init", "-q", "-b", "master")
	runGitPanelCommand(t, workspace, "config", "user.email", "test@example.com")
	runGitPanelCommand(t, workspace, "config", "user.name", "Test User")
	writeGitPanelFile(t, workspace, "base.txt", "base\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "base")
	runGitPanelCommand(t, workspace, "remote", "add", "origin", origin)
	runGitPanelCommand(t, workspace, "push", "-q", "-u", "origin", "master")

	parent := t.TempDir()
	runGitPanelCommand(t, parent, "clone", "-q", "-b", "master", origin, "other")
	other := filepath.Join(parent, "other")
	runGitPanelCommand(t, other, "config", "user.email", "test@example.com")
	runGitPanelCommand(t, other, "config", "user.name", "Test User")
	writeGitPanelFile(t, other, "remote.txt", "remote\n")
	runGitPanelCommand(t, other, "add", ".")
	runGitPanelCommand(t, other, "commit", "-q", "-m", "remote work")
	runGitPanelCommand(t, other, "push", "-q", "origin", "master")
	return workspace, origin
}

func TestGitPanelRemoteButtons(t *testing.T) {
	workspace, origin := initGitPanelRemote(t)
	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && app.Git.RepoRoot != "" && app.Git.Busy == ""
	})

	if err := tt.Click("Fetch"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !app.Git.Loading && app.Git.Busy == "" && app.Git.Notice == "Fetched." && app.Git.Behind == 1
	})
	if !tt.HasText("↓1") {
		t.Errorf("fetched commit is not shown as behind; texts: %q", tt.Texts())
	}
	if _, err := os.Stat(filepath.Join(workspace, "remote.txt")); !os.IsNotExist(err) {
		t.Errorf("fetch changed the work tree (err = %v)", err)
	}

	if err := tt.Click("Pull"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !app.Git.Loading && app.Git.Busy == "" && app.Git.Notice == "Pulled." && app.Git.Behind == 0
	})
	if data, err := os.ReadFile(filepath.Join(workspace, "remote.txt")); err != nil || string(data) != "remote\n" {
		t.Fatalf("pulled file = %q, err = %v", data, err)
	}

	writeGitPanelFile(t, workspace, "local.txt", "local\n")
	if err := tt.Click("Refresh"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !app.Git.Loading && len(app.Git.Changes) >= 1
	})
	if err := tt.Click("Stage all"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !app.Git.Loading && app.Git.Busy == "" && len(app.Git.Staged) == 1
	})
	app.Git.Message = "local work"
	tt.Frame()
	if err := tt.Click("Commit"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !app.Git.Loading && app.Git.Busy == "" && app.Git.Notice == "Committed." && app.Git.Message == ""
	})
	localHead := gitPanelOutput(t, workspace, "rev-parse", "HEAD")

	if err := tt.Click("Push"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !app.Git.Loading && app.Git.Busy == "" && app.Git.Notice == "Pushed."
	})
	if got := gitPanelOutput(t, origin, "rev-parse", "master"); got != localHead {
		t.Fatalf("origin/master = %s, want pushed %s", got, localHead)
	}

	writeGitPanelFile(t, workspace, "unstaged.txt", "change\n")
	if err := tt.Click("Refresh"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !app.Git.Loading && len(app.Git.Changes) >= 1
	})
	if err := tt.Click("Stage all"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !app.Git.Loading && app.Git.Busy == "" && len(app.Git.Staged) == 1
	})
	if err := tt.Click("Unstage all"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !app.Git.Loading && app.Git.Busy == "" && len(app.Git.Staged) == 0
	})
	if len(app.Git.Changes) == 0 {
		t.Fatal("Unstage all removed the work-tree change")
	}
}

func TestGitPanelUndoLastCommit(t *testing.T) {
	workspace := initGitPanelRepo(t)
	writeGitPanelFile(t, workspace, "base.txt", "base\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "base")
	base := gitPanelOutput(t, workspace, "rev-parse", "HEAD")

	writeGitPanelFile(t, workspace, "next.txt", "next\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "next")

	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && app.Git.Busy == "" && app.Git.HasCommits
	})
	if err := tt.Click("Undo"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !app.Git.Loading && app.Git.Busy == "" &&
			app.Git.Notice == "Last commit undone; changes stay staged."
	})
	if got := gitPanelOutput(t, workspace, "rev-parse", "HEAD"); got != base {
		t.Fatalf("HEAD after undo = %s, want %s", got, base)
	}
	waitFor(t, app, tt, func() bool { return !app.Git.Loading && len(app.Git.Staged) == 1 })
	if app.Git.Staged[0].Path != "next.txt" {
		t.Errorf("staged after undo = %+v", app.Git.Staged)
	}
	if app.Git.Message != "next" {
		t.Errorf("message after undo = %q, want %q", app.Git.Message, "next")
	}
}
