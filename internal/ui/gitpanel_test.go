package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/git"
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
		return app.Git.Message == "" && gitPanelHasCommit(app, "first from panel")
	})
	out, err := exec.Command("git", "-C", workspace, "log", "-1", "--pretty=format:%s").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "first from panel" {
		t.Fatalf("commit subject = %q", out)
	}
	tt.Frame()
	if !tt.HasText("first from panel") {
		t.Errorf("graph does not show the new subject; texts: %q", tt.Texts())
	}
	if len(app.Git.Commits) != 1 || app.Git.Commits[0].Lane != 0 {
		t.Errorf("commits = %+v", app.Git.Commits)
	}
}

func TestGitGraphShowsOneShortRefAndHash(t *testing.T) {
	commit := git.LogCommit{
		Hash: "0123456789abcdef",
		Refs: []string{"feature/a-very-long-name", "v1.0.0", "origin/main"},
	}
	if got := graphRef(commit); len([]rune(got)) != 13 || !strings.HasSuffix(got, "…") {
		t.Errorf("ref = %q, want 12 runes and an ellipsis", got)
	}
	if got := graphRef(git.LogCommit{}); got != "" {
		t.Errorf("ref of a commit without refs = %q", got)
	}
	if got := shortCommitHash(commit.Hash); got != "0123456" {
		t.Errorf("hash = %q", got)
	}
	if shortGraphText("发布分支", 12) != "发布分支" {
		t.Error("shortGraphText split a short non-Latin name")
	}
}

func TestGitPanelExpandsCommitFiles(t *testing.T) {
	workspace := initGitPanelRepo(t)
	writeGitPanelFile(t, workspace, "hello.txt", "one\n")
	writeGitPanelFile(t, workspace, "docs/readme.md", "readme\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "first")

	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && len(app.Git.Commits) == 1
	})
	tt.Frame()
	if err := tt.Click("first"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Git.CommitOpen != "" && !app.Git.CommitFilesBusy
	})
	if len(app.Git.CommitFiles) != 2 {
		t.Fatalf("commit files = %+v", app.Git.CommitFiles)
	}
	tt.Frame()
	for _, want := range []string{"hello.txt", "readme.md", "docs"} {
		if !tt.HasText(want) {
			t.Errorf("expanded list is missing %q; texts: %q", want, tt.Texts())
		}
	}

	if err := tt.Click("Commit diff of hello.txt"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Git.PreviewOpen && !app.Git.PreviewBusy && app.Git.PreviewPath == "hello.txt"
	})
	if app.Git.PreviewCommit == "" {
		t.Fatalf("commit preview has no commit: %+v", app.Git)
	}
	if app.Git.PreviewOld != "" || app.Git.PreviewNew != "one\n" {
		t.Fatalf("commit preview sides = %q → %q", app.Git.PreviewOld, app.Git.PreviewNew)
	}
	tt.Frame()
	if !tt.HasText("Commit " + shortCommitHash(app.Git.PreviewCommit) + " · hello.txt") {
		t.Errorf("commit diff title missing; texts: %q", tt.Texts())
	}
	if err := tt.Click("Close"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return !app.Git.PreviewOpen })

	if err := tt.Click("first"); err != nil {
		t.Fatal(err)
	}
	if app.Git.CommitOpen != "" || app.Git.CommitFiles != nil {
		t.Errorf("collapse left commit open = %q, files = %+v", app.Git.CommitOpen, app.Git.CommitFiles)
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

func TestGitPanelPreviewsDiff(t *testing.T) {
	workspace := initGitPanelRepo(t)
	writeGitPanelFile(t, workspace, "hello.txt", "one\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "first")
	writeGitPanelFile(t, workspace, "hello.txt", "two\n")

	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && len(app.Git.Changes) == 1
	})
	if err := tt.Click("Diff of hello.txt"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Git.PreviewOpen && !app.Git.PreviewBusy && app.Git.PreviewNew != ""
	})
	if app.Git.PreviewOld != "one\n" || app.Git.PreviewNew != "two\n" {
		t.Fatalf("preview sides = %q → %q", app.Git.PreviewOld, app.Git.PreviewNew)
	}
	if app.Git.PreviewStaged {
		t.Error("unstaged change opened as staged")
	}
	tt.Frame()
	for _, want := range []string{"Diff · hello.txt", "Split", "Unified"} {
		if !tt.HasText(want) {
			t.Errorf("overlay is missing %q; texts: %q", want, tt.Texts())
		}
	}

	if err := tt.Click("Close"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return !app.Git.PreviewOpen && app.Git.PreviewPath == "" })

	// The staged side compares HEAD with the index.
	if err := tt.Click("Stage all"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return len(app.Git.Staged) == 1 && len(app.Git.Changes) == 0 && app.Git.Busy == ""
	})
	if err := tt.Click("Diff of hello.txt"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Git.PreviewOpen && app.Git.PreviewStaged && !app.Git.PreviewBusy && app.Git.PreviewNew != ""
	})
	if app.Git.PreviewOld != "one\n" || app.Git.PreviewNew != "two\n" {
		t.Fatalf("staged preview sides = %q → %q", app.Git.PreviewOld, app.Git.PreviewNew)
	}
	tt.Frame()
	if !tt.HasText("Staged diff · hello.txt") {
		t.Errorf("staged title missing; texts: %q", tt.Texts())
	}
}

func TestGitPanelDiffRejectsBinary(t *testing.T) {
	workspace := initGitPanelRepo(t)
	writeGitPanelFile(t, workspace, "blob.bin", "head\x00tail\n")

	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && len(app.Git.Changes) == 1
	})
	if err := tt.Click("Diff of blob.bin"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Git.PreviewOpen && !app.Git.PreviewBusy && app.Git.PreviewErr != ""
	})
	if !strings.Contains(app.Git.PreviewErr, "binary") {
		t.Fatalf("preview error = %q", app.Git.PreviewErr)
	}
	tt.Frame()
	if !tt.HasText("Cannot show this diff") {
		t.Errorf("binary error is missing from the overlay; texts: %q", tt.Texts())
	}
}

func gitPanelHasCommit(app *App, want string) bool {
	for _, c := range app.Git.Commits {
		if strings.Contains(c.Message, want) {
			return true
		}
	}
	return false
}

func writeGitPanelFile(t *testing.T, dir, name, text string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
