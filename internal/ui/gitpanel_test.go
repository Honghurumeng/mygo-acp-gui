package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if _, ok := tt.Find("Commit"); !ok {
		t.Fatalf("commit box missing before collapse; texts: %q", tt.Texts())
	}

	if err := tt.Click("Hide Git panel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.GitPanelOpen {
		t.Fatal("header button did not close the Git panel")
	}
	if _, ok := tt.Find("Commit"); ok {
		t.Fatalf("Git panel still drawn after collapse; texts: %q", tt.Texts())
	}

	if err := tt.Click("Show Git panel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.GitPanelOpen {
		t.Fatal("header button did not reopen the Git panel")
	}
	if _, ok := tt.Find("Commit"); !ok {
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

func TestGitPanelRefreshesOnFocus(t *testing.T) {
	workspace := initGitPanelRepo(t)
	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && app.Git.RepoRoot != ""
	})

	writeGitPanelFile(t, workspace, "focused.txt", "changed elsewhere\n")
	app.refreshGitOnFocus()
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && len(app.Git.Changes) == 1
	})
	if !tt.HasText("focused.txt") {
		t.Errorf("focused refresh did not show the new change; texts: %q", tt.Texts())
	}
}

func TestGitPanelHeaderShowsDiffStat(t *testing.T) {
	workspace := initGitPanelRepo(t)
	writeGitPanelFile(t, workspace, "a.txt", "one\ntwo\n")
	writeGitPanelFile(t, workspace, "b.txt", "three\n")
	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && app.Git.Stat.Files == 2
	})
	if stat := app.Git.Stat; stat.Added != 3 || stat.Removed != 0 {
		t.Fatalf("stat = %+v; want 3 added", stat)
	}
	if !tt.HasText("2 files") || !tt.HasText("+3") {
		t.Errorf("header summary missing; texts: %q", tt.Texts())
	}

	if err := tt.Click("Stage all"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return len(app.Git.Staged) == 2 && app.Git.Busy == ""
	})
	app.Git.Message = "first"
	tt.Frame()
	if err := tt.Click("Commit"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Git.Busy == "" && app.Git.Stat == (git.DiffStat{})
	})
	if tt.HasText("2 files") {
		t.Errorf("summary survived the commit; texts: %q", tt.Texts())
	}

	writeGitPanelFile(t, workspace, "b.txt", "four\nfive\n")
	app.refreshGitOnFocus()
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && app.Git.Stat.Files == 1
	})
	if stat := app.Git.Stat; stat.Added != 2 || stat.Removed != 1 {
		t.Fatalf("stat = %+v; want 2 added, 1 removed", stat)
	}
	if !tt.HasText("1 file") || !tt.HasText("+2") || !tt.HasText("−1") {
		t.Errorf("updated header summary missing; texts: %q", tt.Texts())
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

func TestGitPanelDiscardsAllChangesAfterConfirm(t *testing.T) {
	workspace := initGitPanelRepo(t)
	writeGitPanelFile(t, workspace, "hello.txt", "one\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "first")
	writeGitPanelFile(t, workspace, "hello.txt", "two\n")
	writeGitPanelFile(t, workspace, "new.txt", "new\n")

	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && len(app.Git.Changes) == 2
	})
	discard, ok := tt.Find("Discard all")
	stage, staged := tt.Find("Stage all")
	if !ok || !staged {
		t.Fatalf("section actions missing; texts: %q", tt.Texts())
	}
	if discard.X >= stage.X {
		t.Fatalf("Discard all at %v is not left of Stage all at %v", discard, stage)
	}

	// The button asks first, and cancelling keeps every change.
	if err := tt.Click("Discard all"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.Git.DiscardOpen || !tt.HasText("Discard all changes?") {
		t.Fatalf("confirmation missing; open = %v, texts = %q", app.Git.DiscardOpen, tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.Git.DiscardOpen || len(app.Git.Changes) != 2 {
		t.Fatalf("cancel kept the dialog or discarded changes: open = %v, changes = %d", app.Git.DiscardOpen, len(app.Git.Changes))
	}

	if err := tt.Click("Discard all"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Discard"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !app.Git.DiscardOpen && app.Git.Busy == "" && !app.Git.Loading &&
			len(app.Git.Staged) == 0 && len(app.Git.Changes) == 0
	})
	data, err := os.ReadFile(filepath.Join(workspace, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "one\n" {
		t.Errorf("tracked file = %q, want the committed contents", data)
	}
	if _, err := os.Stat(filepath.Join(workspace, "new.txt")); !os.IsNotExist(err) {
		t.Errorf("untracked file survived the discard (err = %v)", err)
	}
}

func TestGitGraphShowsOneShortRef(t *testing.T) {
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

func TestGitPanelHoverShowsCommitDetailsAndCopiesHash(t *testing.T) {
	workspace := initGitPanelRepo(t)
	writeGitPanelFile(t, workspace, "hello.txt", "hello\nworld\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "hover details")

	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && len(app.Git.Commits) == 1
	})
	tt.Frame()
	commit := app.Git.Commits[0]
	row, ok := tt.Find("hover details")
	if !ok {
		t.Fatalf("commit row missing; texts: %q", tt.Texts())
	}
	if tt.HasText(shortCommitHash(commit.Hash)) {
		t.Errorf("the graph row still shows the short hash; texts: %q", tt.Texts())
	}

	// The card waits for a short rest, so passing over the graph does not
	// cover it in popovers.
	tt.Move(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	if tt.HasText(commit.Hash) {
		t.Fatal("the details card showed before the pointer rested")
	}
	tt.Advance(gitGraphHoverDelay + time.Millisecond)
	waitFor(t, app, tt, func() bool {
		return app.Git.CommitStatHash == commit.Hash && !app.Git.CommitStatBusy
	})
	for _, want := range []string{commit.Hash, "Test User", commit.Time.Local().Format("2006-01-02 15:04"), "1 file", "+2"} {
		if !tt.HasText(want) {
			t.Errorf("details card is missing %q; texts: %q", want, tt.Texts())
		}
	}
	files, filesOK := tt.Find("1 file")
	added, addedOK := tt.Find("+2")
	if !filesOK || !addedOK || files.Y != added.Y {
		t.Errorf("the changed files and lines are not on one row: files at %v, lines at %v", files, added)
	}
	if tt.HasText("Parents") {
		t.Errorf("details card still lists parents; texts: %q", tt.Texts())
	}
	if hash, ok := tt.Find(commit.Hash); !ok || hash.X+hash.W >= row.X {
		t.Errorf("details card at %v is not left of the graph row at %v", hash, row)
	}

	if err := tt.Click("Copy commit hash"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := tt.Clipboard(); got != commit.Hash {
		t.Errorf("clipboard = %q, want the full hash %q", got, commit.Hash)
	}

	// Clicking the row it describes dismisses the card and expands the
	// commit as a plain click does.
	if err := tt.Click("hover details"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.Git.CommitHover != "" || app.Git.CommitHoverOpen {
		t.Errorf("card survived the row click: hover = %q, open = %v", app.Git.CommitHover, app.Git.CommitHoverOpen)
	}
	waitFor(t, app, tt, func() bool { return app.Git.CommitOpen == commit.Hash && !app.Git.CommitFilesBusy })

	// Leaving the row and card closes it after the grace period.
	tt.Move(5, 5)
	tt.Frame()
	tt.Advance(gitGraphHoverGrace + time.Millisecond)
	tt.Frame()
	if app.Git.CommitHover != "" || app.Git.CommitHoverOpen {
		t.Errorf("card stayed: hover = %q, open = %v", app.Git.CommitHover, app.Git.CommitHoverOpen)
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
	if !tt.HasText("Diff · hello.txt") {
		t.Errorf("diff card title is missing; texts: %q", tt.Texts())
	}
	// The card floats over the conversation with its right edge on the Git
	// panel's left edge: its close button stays left of the panel's list.
	closeRect, ok := tt.Find("Close")
	if !ok {
		t.Fatalf("diff card has no Close button; texts: %q", tt.Texts())
	}
	panelRect, ok := tt.Find("No staged changes")
	if !ok {
		t.Fatalf("Git panel list is missing; texts: %q", tt.Texts())
	}
	if closeRect.X+closeRect.W > panelRect.X {
		t.Errorf("diff card covers the Git panel: card right edge %.0f, panel left edge %.0f",
			closeRect.X+closeRect.W, panelRect.X)
	}
	if _, ok := tt.Find("Diff of hello.txt"); !ok {
		t.Errorf("Git panel file row is hidden while the diff card is open; texts: %q", tt.Texts())
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

	// Pressing outside the card closes it, as an overlay's backdrop did.
	tt.ClickAt(600, 736)
	waitFor(t, app, tt, func() bool { return !app.Git.PreviewOpen && app.Git.PreviewPath == "" })
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
		t.Errorf("binary error is missing from the card; texts: %q", tt.Texts())
	}
}

func TestGitPanelRefreshesAfterTurn(t *testing.T) {
	workspace := initGitPanelRepo(t)
	app, tt := newFakeAppIn(t, workspace)
	connect(t, app, tt)
	tt.Frame()
	waitFor(t, app, tt, func() bool {
		return app.Git.Workspace == workspace && app.Git.Checked && !app.Git.Loading
	})
	if app.Git.RepoRoot == "" {
		t.Fatalf("panel did not read the repository: %+v", app.Git)
	}

	// The fake agent does not touch the disk; write what it would have
	// written, then finish a turn and expect the panel to pick it up.
	writeGitPanelFile(t, workspace, "agent.txt", "written by the agent\n")
	current(app).Input = "hello"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !current(app).Streaming && !app.Git.Loading && len(app.Git.Changes) == 1
	})
	if app.Git.Changes[0].Path != "agent.txt" {
		t.Errorf("changes = %+v", app.Git.Changes)
	}
	tt.Frame()
	if !tt.HasText("agent.txt") {
		t.Errorf("turn-end refresh did not show the new file; texts: %q", tt.Texts())
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

func TestGitDiffCardLayoutRespondsToWidth(t *testing.T) {
	cases := []struct {
		window float32
		width  float32
		split  bool
	}{
		{900, 480, false},
		{1240, 582.8, false},
		{1600, 806, true},
		{1920, 900, true},
	}
	for _, c := range cases {
		got := gitDiffCardWidth(c.window)
		if diff := got - c.width; diff > 0.5 || diff < -0.5 {
			t.Errorf("card width for a %v DIP window = %v, want %v", c.window, got, c.width)
		}
		if split := gitDiffSplit(got); split != c.split {
			t.Errorf("card %v DIPs wide splits = %v, want %v", got, split, c.split)
		}
	}
}
