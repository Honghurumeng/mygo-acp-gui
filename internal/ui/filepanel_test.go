package ui

import (
	"os"
	"path/filepath"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
)

func newFilePanelApp(t *testing.T, workspace string) (*App, *mui.Tester) {
	t.Helper()
	app := New(Options{Version: "test", Workspace: workspace, Agents: []agent.LaunchSpec{}})
	tt := mui.NewTester(app.View, 1200, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	return app, tt
}

func showFilePanel(t *testing.T, app *App, tt *mui.Tester) {
	t.Helper()
	tt.Frame()
	if err := tt.Click("Show Files panel"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.RightTab == rightTabFiles && app.Files.Checked && !app.Files.Loading && app.Files.Loaded[""]
	})
}

func TestFilePanelShowsTreeAndGitStatus(t *testing.T) {
	workspace := initGitPanelRepo(t)
	writeGitPanelFile(t, workspace, "alpha.txt", "one\n")
	writeGitPanelFile(t, workspace, "docs/readme.md", "hello\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "first")
	writeGitPanelFile(t, workspace, "alpha.txt", "two\n")
	writeGitPanelFile(t, workspace, "untracked.txt", "new\n")
	writeGitPanelFile(t, workspace, "staged.txt", "added\n")
	runGitPanelCommand(t, workspace, "add", "staged.txt")

	app, tt := newFilePanelApp(t, workspace)
	showFilePanel(t, app, tt)

	if file := app.Files.Status["alpha.txt"]; file.Worktree != 'M' {
		t.Errorf("alpha status = %+v, want unstaged M", file)
	}
	if file := app.Files.Status["untracked.txt"]; file.Index != '?' {
		t.Errorf("untracked status = %+v, want ?", file)
	}
	if file := app.Files.Status["staged.txt"]; file.Index != 'A' {
		t.Errorf("staged status = %+v, want A", file)
	}
	for _, name := range []string{"alpha.txt", "docs", "untracked.txt", "staged.txt"} {
		if !tt.HasText(name) {
			t.Errorf("%q missing from the tree; texts: %q", name, tt.Texts())
		}
	}
	if got := len(app.Files.Status); got != 3 {
		t.Errorf("status map has %d entries: %+v", got, app.Files.Status)
	}

	if err := tt.Click("docs"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Files.Loaded["docs"] })
	if !tt.HasText("readme.md") {
		t.Errorf("expanded folder did not list its file; texts: %q", tt.Texts())
	}

	if !tt.HasText("New file") || !tt.HasText("New folder") || !tt.HasText("Refresh files") {
		t.Errorf("header actions missing; texts: %q", tt.Texts())
	}
}

func TestFilePanelCreatesRenamesAndDeletes(t *testing.T) {
	workspace := t.TempDir()
	app, tt := newFilePanelApp(t, workspace)
	showFilePanel(t, app, tt)

	// New file.
	if err := tt.Click("New file"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.Files.CreateOpen {
		t.Fatal("New file did not open the create dialog")
	}
	app.Files.CreateName = "notes.txt"
	tt.Frame()
	if err := tt.Click("Create"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		_, err := os.Stat(filepath.Join(workspace, "notes.txt"))
		return err == nil && app.Files.Busy == "" && app.Files.Loaded[""]
	})
	if app.Files.Notice != "File created." {
		t.Errorf("notice = %q", app.Files.Notice)
	}
	if !tt.HasText("notes.txt") {
		t.Errorf("created file missing from tree; texts: %q", tt.Texts())
	}

	// New folder expands and shows its empty body.
	if err := tt.Click("New folder"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	app.Files.CreateName = "docs"
	tt.Frame()
	if err := tt.Click("Create"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		_, err := os.Stat(filepath.Join(workspace, "docs"))
		return err == nil && app.Files.Busy == "" && app.Files.Loaded["docs"]
	})
	if !app.Files.Expanded["docs"] {
		t.Error("new folder did not open")
	}
	if !tt.HasText("Empty") {
		t.Errorf("empty folder hint missing; texts: %q", tt.Texts())
	}

	// Rename through the row's context menu.
	if err := tt.RightClick("notes.txt"); err != nil {
		t.Fatal(err)
	}
	if got := tt.Menu(); len(got) != 3 || got[0] != "Reveal in folder" || got[1] != "Rename" || got[2] != "Delete" {
		t.Fatalf("context menu = %v", got)
	}
	if err := tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	app.Files.RenameName = "journal.txt"
	tt.Frame()
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		_, oldErr := os.Stat(filepath.Join(workspace, "notes.txt"))
		_, newErr := os.Stat(filepath.Join(workspace, "journal.txt"))
		return oldErr != nil && newErr == nil && app.Files.Busy == "" && app.Files.Loaded[""]
	})
	if app.Files.Notice != "Renamed." {
		t.Errorf("notice = %q", app.Files.Notice)
	}

	// Delete asks first, then removes the file.
	if err := tt.RightClick("journal.txt"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Delete"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.Files.DeleteOpen {
		t.Fatal("Delete did not open the confirmation")
	}
	if err := tt.Click("Delete file"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		_, err := os.Stat(filepath.Join(workspace, "journal.txt"))
		return err != nil && app.Files.Busy == "" && app.Files.Loaded[""]
	})
	if app.Files.Notice != "Deleted." {
		t.Errorf("notice = %q", app.Files.Notice)
	}
	if tt.HasText("journal.txt") {
		t.Errorf("deleted file is still drawn; texts: %q", tt.Texts())
	}
}

func TestFilePanelRevealsSelection(t *testing.T) {
	workspace := t.TempDir()
	writeGitPanelFile(t, workspace, "docs/readme.md", "hello\n")
	app, tt := newFilePanelApp(t, workspace)
	var revealed []string
	app.revealItem = func(path string) { revealed = append(revealed, path) }
	showFilePanel(t, app, tt)

	if err := tt.RightClick("docs"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Reveal in folder"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(revealed) != 1 || revealed[0] != filepath.Join(workspace, "docs") {
		t.Fatalf("revealed = %v", revealed)
	}
}

func TestFilePanelDragsIntoFolder(t *testing.T) {
	workspace := t.TempDir()
	writeGitPanelFile(t, workspace, "a.txt", "hello\n")
	if err := os.Mkdir(filepath.Join(workspace, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	app, tt := newFilePanelApp(t, workspace)
	showFilePanel(t, app, tt)

	source, ok := tt.Find("a.txt")
	if !ok {
		t.Fatalf("source row missing; texts: %q", tt.Texts())
	}
	target, ok := tt.Find("docs")
	if !ok {
		t.Fatalf("target row missing; texts: %q", tt.Texts())
	}
	tt.Press(source.X, source.Y)
	tt.Move(source.X+10, source.Y+4)
	tt.Move(target.X, target.Y)
	tt.Release(target.X, target.Y)

	waitFor(t, app, tt, func() bool {
		_, err := os.Stat(filepath.Join(workspace, "docs", "a.txt"))
		return err == nil && app.Files.Busy == "" && app.Files.Loaded["docs"]
	})
	if app.Files.Notice != "Moved." {
		t.Errorf("notice = %q", app.Files.Notice)
	}
	if _, err := os.Stat(filepath.Join(workspace, "a.txt")); !os.IsNotExist(err) {
		t.Errorf("source survived the move: %v", err)
	}
	if !app.Files.Expanded["docs"] {
		t.Error("drop target did not open")
	}
}

func TestFilePanelFollowsWorkspace(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeGitPanelFile(t, first, "first.txt", "1\n")
	writeGitPanelFile(t, second, "second.txt", "2\n")
	app, tt := newFilePanelApp(t, first)
	showFilePanel(t, app, tt)
	if !tt.HasText("first.txt") {
		t.Fatalf("first tree missing; texts: %q", tt.Texts())
	}

	app.newChatTabIn(second)
	waitFor(t, app, tt, func() bool {
		return app.Files.Workspace == second && app.Files.Checked && app.Files.Loaded[""]
	})
	if tt.HasText("first.txt") || !tt.HasText("second.txt") {
		t.Fatalf("tree did not follow the chat; texts: %q", tt.Texts())
	}
}

func TestFilePanelMapsRepositoryStatusIntoNestedWorkspace(t *testing.T) {
	repo := initGitPanelRepo(t)
	workspace := filepath.Join(repo, "app")
	writeGitPanelFile(t, repo, "app/one.txt", "one\n")
	writeGitPanelFile(t, repo, "other.txt", "other\n")
	runGitPanelCommand(t, repo, "add", ".")
	runGitPanelCommand(t, repo, "commit", "-q", "-m", "first")
	writeGitPanelFile(t, repo, "app/one.txt", "two\n")
	writeGitPanelFile(t, repo, "other.txt", "changed\n")

	app, tt := newFilePanelApp(t, workspace)
	showFilePanel(t, app, tt)

	if file, ok := app.Files.Status["one.txt"]; !ok || file.Worktree != 'M' {
		t.Fatalf("nested status = %+v, full map %+v", file, app.Files.Status)
	}
	if _, ok := app.Files.Status["other.txt"]; ok {
		t.Fatalf("status outside the workspace leaked in: %+v", app.Files.Status)
	}
	if app.Files.RepoRoot == "" || !filepath.IsAbs(app.Files.RepoRoot) {
		t.Fatalf("repo root = %q", app.Files.RepoRoot)
	}
}

func TestFilePanelListsWithoutGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	workspace := t.TempDir()
	writeGitPanelFile(t, workspace, "readme.md", "hello\n")
	app, tt := newFilePanelApp(t, workspace)
	showFilePanel(t, app, tt)

	if !tt.HasText("readme.md") {
		t.Fatalf("tree needs no Git to list files; texts: %q", tt.Texts())
	}
	if app.Files.RepoRoot != "" || app.Files.StatusError != "" {
		t.Fatalf("missing Git should be silent: %+v", app.Files)
	}
}

func TestFilePanelIndentsNestedFolders(t *testing.T) {
	workspace := t.TempDir()
	writeGitPanelFile(t, workspace, "a.txt", "hello\n")
	writeGitPanelFile(t, workspace, "docs/readme.md", "hello\n")
	writeGitPanelFile(t, workspace, "docs/inner/deep.txt", "hello\n")
	app, tt := newFilePanelApp(t, workspace)
	showFilePanel(t, app, tt)

	if err := tt.Click("docs"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Files.Loaded["docs"] })
	if err := tt.Click("inner"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Files.Loaded["docs/inner"] })

	x := func(name string) float32 {
		t.Helper()
		rect, ok := tt.Find(name)
		if !ok {
			t.Fatalf("%q missing from the tree; texts: %q", name, tt.Texts())
		}
		return rect.X
	}
	root, child, grandchild := x("a.txt"), x("readme.md"), x("deep.txt")
	if child <= root {
		t.Errorf("child row x=%g does not sit right of its folder x=%g", child, root)
	}
	if grandchild <= child {
		t.Errorf("grandchild row x=%g does not sit right of its folder x=%g", grandchild, child)
	}
	if got := child - root; got != grandchild-child {
		t.Errorf("indent per level = %g then %g, want equal steps", got, grandchild-child)
	}
}

func TestFilePanelMarksIgnoredEntries(t *testing.T) {
	workspace := initGitPanelRepo(t)
	writeGitPanelFile(t, workspace, ".gitignore", "dist/\n*.log\n")
	writeGitPanelFile(t, workspace, "dist/out.bin", "x\n")
	writeGitPanelFile(t, workspace, "notes.log", "x\n")
	writeGitPanelFile(t, workspace, "keep.md", "x\n")

	app, tt := newFilePanelApp(t, workspace)
	showFilePanel(t, app, tt)

	if !app.Files.Ignored["dist"] || !app.Files.Ignored["notes.log"] {
		t.Fatalf("ignored set = %+v", app.Files.Ignored)
	}
	if app.Files.Ignored["keep.md"] || app.Files.Ignored[".gitignore"] {
		t.Errorf("kept paths marked ignored: %+v", app.Files.Ignored)
	}
	if !app.fileIgnored("dist/out.bin") {
		t.Error("a child of an ignored folder should count as ignored")
	}
	if app.fileIgnored("keep.md") {
		t.Error("a committed path must not count as ignored")
	}
	if !tt.HasText("notes.log") || !tt.HasText("dist") {
		t.Errorf("ignored entries left the tree; texts: %q", tt.Texts())
	}
	if !tt.HasText("ignored") {
		t.Errorf("ignored entries carry no label; texts: %q", tt.Texts())
	}

	if err := tt.Click("dist"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Files.Loaded["dist"] })
	if !tt.HasText("out.bin") || !app.fileIgnored("dist/out.bin") {
		t.Errorf("expanded ignored folder lost its marker; texts: %q", tt.Texts())
	}
}

func TestFilePanelMarksWorkspaceInsideIgnoredFolder(t *testing.T) {
	repo := initGitPanelRepo(t)
	writeGitPanelFile(t, repo, ".gitignore", "build/\n")
	workspace := filepath.Join(repo, "build", "app")
	writeGitPanelFile(t, repo, "build/app/one.txt", "x\n")

	app, tt := newFilePanelApp(t, workspace)
	showFilePanel(t, app, tt)

	if !app.Files.IgnoredAll {
		t.Fatalf("workspace under an ignored folder should be all ignored: %+v", app.Files)
	}
	if !app.fileIgnored("one.txt") {
		t.Error("path in an ignored workspace should count as ignored")
	}
}
