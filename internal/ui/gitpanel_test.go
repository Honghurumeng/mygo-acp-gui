package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
)

func TestGitPanelPromptsForRepositoryAndInitializes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	workspace := t.TempDir()
	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool { return app.Git.Checked && !app.Git.Loading })

	if app.Git.NoRepo != true {
		t.Fatalf("non-repository state = %+v", app.Git)
	}
	if !tt.HasText("Not a Git repository") {
		t.Fatalf("missing repository prompt; texts: %q", tt.Texts())
	}
	if err := tt.Click("Initialize Git repository"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Git.RepoRoot != "" && !app.Git.NoRepo && !app.Git.Loading
	})
	if _, err := os.Stat(filepath.Join(workspace, ".git")); err != nil {
		t.Fatalf("git init did not create .git: %v", err)
	}
	if !tt.HasText("No commits yet.") {
		t.Errorf("empty repository graph hint missing; texts: %q", tt.Texts())
	}
}

func TestGitPanelPromptsWhenGitIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	app, tt := newGitPanelApp(t, t.TempDir())
	waitFor(t, app, tt, func() bool { return app.Git.Checked && !app.Git.Loading })

	if app.Git.Available {
		t.Fatalf("git reported available with an empty PATH: %+v", app.Git)
	}
	if !tt.HasText("Git is not installed.") {
		t.Fatalf("missing git install prompt; texts: %q", tt.Texts())
	}
}

func TestGitPanelTogglesFromHeader(t *testing.T) {
	workspace := initGitPanelRepo(t)
	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && app.Git.RepoRoot != ""
	})
	if !app.GitPanelOpen {
		t.Fatal("Git panel did not start open")
	}
	if !tt.HasText("Stage changes to commit") {
		t.Fatalf("commit box missing before collapse; texts: %q", tt.Texts())
	}

	if err := tt.Click("Hide Git panel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.GitPanelOpen {
		t.Fatal("header button did not close the Git panel")
	}
	if tt.HasText("Stage changes to commit") {
		t.Fatalf("Git panel still drawn after collapse; texts: %q", tt.Texts())
	}

	if err := tt.Click("Show Git panel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.GitPanelOpen {
		t.Fatal("header button did not reopen the Git panel")
	}
	if !tt.HasText("Stage changes to commit") {
		t.Fatalf("commit box missing after reopen; texts: %q", tt.Texts())
	}
}

func TestGitPanelFollowsActiveWorkspace(t *testing.T) {
	repo := initGitPanelRepo(t)
	other := t.TempDir()
	app, tt := newGitPanelApp(t, repo)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && app.Git.RepoRoot != ""
	})

	app.newChatTabIn(other)
	tt.Frame()
	waitFor(t, app, tt, func() bool {
		return app.Git.Workspace == other && app.Git.Checked && !app.Git.Loading
	})
	if app.Git.RepoRoot != "" || !app.Git.NoRepo {
		t.Fatalf("panel kept the previous repository: %+v", app.Git)
	}
	if !tt.HasText("Not a Git repository") {
		t.Errorf("new workspace prompt missing; texts: %q", tt.Texts())
	}
}

func TestGitPanelStagesAndCommits(t *testing.T) {
	workspace := initGitPanelRepo(t)
	if err := os.WriteFile(filepath.Join(workspace, "hello.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && len(app.Git.Changes) == 1
	})
	if !tt.HasText("hello.txt") {
		t.Fatalf("changed file missing; texts: %q", tt.Texts())
	}

	if err := tt.Click("Stage all"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return len(app.Git.Staged) == 1 && len(app.Git.Changes) == 0 && app.Git.Busy == ""
	})

	app.Git.Message = "first from panel"
	tt.Frame()
	if err := tt.Click("Commit"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Git.Message == "" && strings.Contains(app.Git.Graph, "first from panel")
	})
	out, err := exec.Command("git", "-C", workspace, "log", "-1", "--pretty=format:%s").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "first from panel" {
		t.Fatalf("commit subject = %q", out)
	}
}

func newGitPanelApp(t *testing.T, workspace string) (*App, *mui.Tester) {
	t.Helper()
	app := New(Options{Version: "test", Workspace: workspace, Agents: []agent.LaunchSpec{}})
	tt := mui.NewTester(app.View, 1200, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	return app, tt
}

func initGitPanelRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	runGitPanelCommand(t, dir, "init", "-q")
	runGitPanelCommand(t, dir, "config", "user.email", "test@example.com")
	runGitPanelCommand(t, dir, "config", "user.name", "Test User")
	return dir
}

func runGitPanelCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
