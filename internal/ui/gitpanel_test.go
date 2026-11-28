package ui

import (
	"context"
	"fmt"
	"image"
	"image/color"
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

func TestGitPanelTogglesFromRail(t *testing.T) {
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
		t.Fatal("rail button did not close the Git panel")
	}
	if _, ok := tt.Find("Commit"); ok {
		t.Fatalf("Git panel still drawn after collapse; texts: %q", tt.Texts())
	}

	if err := tt.Click("Show Git panel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.GitPanelOpen {
		t.Fatal("rail button did not reopen the Git panel")
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

func TestGitHistoryDividerStaysWhenTopContentChanges(t *testing.T) {
	workspace := initGitPanelRepo(t)
	writeGitPanelFile(t, workspace, "hello.txt", "hello\n")
	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && len(app.Git.Changes) == 1
	})

	// The divider the user drags sits between the changes list and the graph.
	// The commit box and the feedback line above the list are inside the same
	// pane, so a message box that grows with a multi-line message, or a notice
	// a commit leaves behind (and a refresh clears), must not move it: only
	// the list gives way.
	divider := func() float32 {
		t.Helper()
		tt.Frame()
		info, ok := tt.AccessByRole(mui.RoleSplitter, "")
		if !ok {
			t.Fatalf("no divider; texts: %q", tt.Texts())
		}
		return info.Bounds.Y
	}
	row := func(label string) float32 {
		t.Helper()
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("no %q row; texts: %q", label, tt.Texts())
		}
		return r.Y
	}

	y0 := divider()
	staged0, fetch0 := row("Staged"), row("Fetch")

	app.Git.Message = "subject\n\nbody line one\nbody line two\nbody line three"
	if y := divider(); y != y0 {
		t.Errorf("divider moved to %v from %v when the message box grew", y, y0)
	}
	if y := row("Fetch"); y != fetch0 {
		t.Errorf("graph moved to %v from %v when the message box grew", y, fetch0)
	}
	if y := row("Staged"); y <= staged0 {
		t.Errorf("changes list did not give way to the grown message box: Staged at %v, was %v", y, staged0)
	}

	app.Git.Message = ""
	app.Git.Notice = "Committed."
	if y := divider(); y != y0 {
		t.Errorf("divider moved to %v from %v when the notice showed", y, y0)
	}
	if y := row("Fetch"); y != fetch0 {
		t.Errorf("graph moved to %v from %v when the notice showed", y, fetch0)
	}

	app.Git.Notice = ""
	if y := divider(); y != y0 {
		t.Errorf("divider moved to %v from %v when the notice cleared", y, y0)
	}
	if y := row("Staged"); y != staged0 {
		t.Errorf("changes list at %v after the notice cleared, was %v", y, staged0)
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

// The confirmation must not need a second click when a background refresh
// starts while the pointer is on it: a focus or turn-end read used to
// disable the button and drop the click, leaving the dialog open and every
// change in place.
func TestGitPanelDiscardConfirmSurvivesBackgroundRefresh(t *testing.T) {
	workspace, app, tt := discardableGitPanel(t)

	if err := tt.Click("Discard all"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	button, ok := tt.Find("Discard")
	if !ok {
		t.Fatalf("confirmation missing; texts: %q", tt.Texts())
	}
	tt.Press(button.X+button.W/2, button.Y+button.H/2)
	tt.Frame()

	// The read starts between the press and the release, as a window focus
	// or an agent turn end does. Its post is not flushed until the waits
	// below, so it is still in flight when the button is released.
	app.startGitLoad(workspace, false)
	tt.Frame()
	if !app.Git.Loading {
		t.Fatal("the background refresh did not start")
	}

	tt.Release(button.X+button.W/2, button.Y+button.H/2)
	tt.Frame()
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
}

// A click on the confirmation while a status read is already in flight runs
// the discard instead of being ignored until the read finishes.
func TestGitPanelDiscardConfirmWhileRefreshRuns(t *testing.T) {
	workspace, app, tt := discardableGitPanel(t)

	if err := tt.Click("Discard all"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	app.startGitLoad(workspace, false)
	tt.Frame()
	if !app.Git.Loading {
		t.Fatal("the background refresh did not start")
	}
	if err := tt.Click("Discard"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !app.Git.DiscardOpen && app.Git.Busy == "" && !app.Git.Loading &&
			len(app.Git.Staged) == 0 && len(app.Git.Changes) == 0
	})
}

// discardableGitPanel returns a repository with one modified tracked file,
// its Git panel read once and shown.
func discardableGitPanel(t *testing.T) (string, *App, *mui.Tester) {
	t.Helper()
	workspace := initGitPanelRepo(t)
	writeGitPanelFile(t, workspace, "hello.txt", "one\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "first")
	writeGitPanelFile(t, workspace, "hello.txt", "two\n")
	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && len(app.Git.Changes) == 1
	})
	return workspace, app, tt
}

// Leaving the Git tab and coming back marks the panel stale. That must not
// start a read while an action runs: its new generation would drop the
// action's completion and leave the panel busy forever.
func TestGitPanelTabSwitchDuringActionClearsBusy(t *testing.T) {
	workspace := initGitPanelRepo(t)
	writeGitPanelFile(t, workspace, "hello.txt", "one\n")
	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && app.Git.RepoRoot != ""
	})

	started, release := make(chan struct{}), make(chan struct{})
	app.runGitAction("Stage all", true, func(context.Context, string) error {
		close(started)
		<-release
		return nil
	})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the action did not start")
	}

	if err := tt.Click("Show Files panel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Show Git panel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.Git.Loading {
		t.Fatal("a read started while an action was running")
	}

	close(release)
	waitFor(t, app, tt, func() bool { return app.Git.Busy == "" && !app.Git.Loading })
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

// TestGitPanelShowsFetchedCommitsAsIncoming keeps a fetch that was not
// pulled apart from a pull: the fetched commits are incoming (dashed lane,
// hollow dot) with an outlined remote chip, while the branch chip keeps the
// lane's color. Pulling makes them solid and puts the branch chip on top.
func TestGitPanelShowsFetchedCommitsAsIncoming(t *testing.T) {
	origin := initGitPanelRepo(t)
	writeGitPanelFile(t, origin, "a.txt", "one\n")
	runGitPanelCommand(t, origin, "add", ".")
	runGitPanelCommand(t, origin, "commit", "-q", "-m", "base")

	workspace := filepath.Join(t.TempDir(), "clone")
	runGitPanelCommand(t, origin, "clone", "-q", origin, workspace)

	writeGitPanelFile(t, origin, "b.txt", "two\n")
	runGitPanelCommand(t, origin, "add", ".")
	runGitPanelCommand(t, origin, "commit", "-q", "-m", "remote work")
	runGitPanelCommand(t, workspace, "fetch", "-q", "origin")

	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && len(app.Git.Commits) == 2
	})
	tt.Frame()
	remote := app.Git.Commits[0]
	if remote.Message != "remote work" || !remote.Incoming || remote.Head {
		t.Fatalf("top commit = %+v, want the fetched, unpulled commit", remote)
	}
	if !tt.HasText("↓1") {
		t.Errorf("header is missing the behind badge; texts: %q", tt.Texts())
	}
	remoteChip := graphRef(remote)
	if remoteChip == "" {
		t.Fatal("the fetched commit shows no ref")
	}
	if chip, row := chipFills(t, tt, remoteChip); chip != row {
		t.Errorf("remote chip fill = %v, want an outline on the row %v", chip, row)
	}

	runGitPanelCommand(t, workspace, "pull", "-q", "--ff-only")
	app.startGitLoad(workspace, true)
	waitFor(t, app, tt, func() bool {
		return !app.Git.Loading && app.Git.Behind == 0 && len(app.Git.Commits) == 2 &&
			app.Git.Commits[0].Head && !app.Git.Commits[0].Incoming
	})
	tt.Frame()
	if tt.HasText("↓1") {
		t.Errorf("behind badge survived the pull; texts: %q", tt.Texts())
	}
	if chip, row := chipFills(t, tt, graphRef(app.Git.Commits[0])); chip == row {
		t.Errorf("current branch chip fill = %v, want the lane's tint", chip)
	}
}

// TestGitPanelMarksUnpushedCommitsViolet draws a commit the upstream does
// not have in violet, dot and lines alike, so it reads apart from the pushed
// ones at a glance; pushing it returns both to the lane's ink.
func TestGitPanelMarksUnpushedCommitsViolet(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	runGitPanelCommand(t, filepath.Dir(origin), "init", "--bare", "-q", origin)
	workspace := initGitPanelRepo(t)
	writeGitPanelFile(t, workspace, "a.txt", "one\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "base")
	runGitPanelCommand(t, workspace, "remote", "add", "origin", origin)
	runGitPanelCommand(t, workspace, "push", "-q", "-u", "origin", "HEAD")

	writeGitPanelFile(t, workspace, "b.txt", "two\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "local work")
	writeGitPanelFile(t, workspace, "c.txt", "three\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "second local work")

	app, tt := newGitPanelApp(t, workspace)
	tt.SetDark(false)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && len(app.Git.Commits) == 3 &&
			app.Git.Commits[0].Unpushed && app.Git.Commits[1].Unpushed &&
			!app.Git.Commits[2].Unpushed
	})
	tt.Move(5, 5)
	tt.Frame()
	violet := gitUnpushedInk(mui.LightTheme())
	img := tt.Image()
	if n := countColor(img, violet); n == 0 {
		t.Fatalf("the unpushed commit's dot is not violet (%v)", violet)
	}

	// The line between the two unpushed commits is violet too: find the
	// dot's column on the upper row, then sample the middle of the gap.
	upper, ok := tt.Find("local work")
	if !ok {
		t.Fatalf("the unpushed commit row is missing; texts: %q", tt.Texts())
	}
	lower, ok := tt.Find("base")
	if !ok {
		t.Fatalf("the pushed commit row is missing; texts: %q", tt.Texts())
	}
	rowY := int(upper.Y + upper.H/2)
	dotX := -1
	for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
		if got := img.RGBAAt(x, rowY); got.R == violet.R && got.G == violet.G && got.B == violet.B {
			dotX = x
			break
		}
	}
	if dotX < 0 {
		t.Fatalf("no violet dot on the unpushed row at y %d", rowY)
	}
	lineY := (rowY + int(lower.Y+lower.H/2)) / 2
	violetLine := false
	for x := dotX - 2; x <= dotX+2; x++ {
		if purplePixel(img.RGBAAt(x, lineY)) {
			violetLine = true
		}
	}
	if !violetLine {
		t.Errorf("the line between unpushed commits is not violet at (%d, %d)", dotX, lineY)
	}

	runGitPanelCommand(t, workspace, "push", "-q", "origin", "HEAD")
	app.startGitLoad(workspace, true)
	waitFor(t, app, tt, func() bool {
		return !app.Git.Loading && len(app.Git.Commits) == 3 && !app.Git.Commits[0].Unpushed
	})
	tt.Frame()
	if n := countColor(tt.Image(), violet); n != 0 {
		t.Errorf("the pushed commit still has a violet dot (%d pixels)", n)
	}
}

// purplePixel reports whether c leans violet, which tells the unpushed ink
// apart from the lane's blue even where alpha thins it.
func purplePixel(c color.RGBA) bool {
	return c.R > c.G && c.B > c.G
}

// countColor counts the frame's pixels drawn in c.
func countColor(img *image.RGBA, c mui.Color) int {
	count := 0
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			got := img.RGBAAt(x, y)
			if got.R == c.R && got.G == c.G && got.B == c.B {
				count++
			}
		}
	}
	return count
}

// chipFills samples a ref chip's inside and the row behind it.
func chipFills(t *testing.T, tt *mui.Tester, ref string) (chip, row color.RGBA) {
	t.Helper()
	rect, ok := tt.Find(ref)
	if !ok {
		t.Fatalf("no chip shows %q; texts: %q", ref, tt.Texts())
	}
	img := tt.Image()
	x := int(rect.X + 3)
	return img.RGBAAt(x, int(rect.Y+3)), img.RGBAAt(x, int(rect.Y-3))
}

// TestGitListsShareScrollBarColumn keeps the changes list's and the history
// list's overlay scroll bars in the panel's right gutter, in one column: both
// scroll boxes run to the panel's right edge and both lists stop their rows
// the same gutter short of it.
func TestGitListsShareScrollBarColumn(t *testing.T) {
	workspace := initGitPanelRepo(t)
	for i := 1; i <= 40; i++ {
		writeGitPanelFile(t, workspace, "history.txt", strings.Repeat("line\n", i))
		runGitPanelCommand(t, workspace, "add", ".")
		runGitPanelCommand(t, workspace, "commit", "-q", "-m", fmt.Sprintf("history %d", i))
	}
	for i := 1; i <= 30; i++ {
		writeGitPanelFile(t, workspace, fmt.Sprintf("changed%d.txt", i), "change\n")
	}

	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && len(app.Git.Changes) == 30 && len(app.Git.Commits) > 1
	})

	// bar returns the columns the overlay scroll bar of the list under (x, y)
	// paints in: the frame with the pointer away from the panel and the frame
	// with it on the list differ along that bar, and nothing changes right of
	// it, so the rightmost run of changed columns is the bar.
	bar := func(x, y float32) (int, int) {
		t.Helper()
		tt.Move(5, 5)
		tt.Frame()
		base := *tt.Image()
		base.Pix = append([]byte(nil), base.Pix...)
		tt.Move(x, y)
		tt.Frame()
		frame := tt.Image()
		start, end := -1, -1
		for px := frame.Bounds().Dx() - 1; px >= 0; px-- {
			changed := false
			for py := frame.Bounds().Dy() - 1; py >= 0 && !changed; py-- {
				changed = frame.RGBAAt(px, py) != base.RGBAAt(px, py)
			}
			switch {
			case changed:
				if end < 0 {
					end = px
				}
				start = px
			case end >= 0:
				return start, end
			}
		}
		return start, end
	}
	center := func(r mui.Rect) (float32, float32) { return r.X + r.W/2, r.Y + r.H/2 }

	file, ok := tt.Find("changed1.txt")
	if !ok {
		t.Fatalf("no changed file row; texts: %q", tt.Texts())
	}
	commit, ok := tt.Find(app.Git.Commits[1].Message)
	if !ok {
		t.Fatalf("no history row; texts: %q", tt.Texts())
	}
	changesStart, changesEnd := bar(center(file))
	historyStart, historyEnd := bar(center(commit))
	if changesStart != historyStart || changesEnd != historyEnd {
		t.Fatalf("scroll bars at columns %d-%d (changes) and %d-%d (history) do not line up",
			changesStart, changesEnd, historyStart, historyEnd)
	}
	if width := changesEnd - changesStart + 1; width < 4 || width > 10 {
		t.Fatalf("changed columns %d-%d are not a scroll bar", changesStart, changesEnd)
	}
	// The rows stop a gutter short of the panel's right edge; the bar runs in
	// that gutter rather than over the rows.
	rowRight := int(commit.X + commit.W)
	if historyStart < rowRight || historyEnd >= rowRight+gitListGutter {
		t.Fatalf("scroll bar at columns %d-%d is not in the %d DIP gutter right of the rows at column %d",
			historyStart, historyEnd, gitListGutter, rowRight)
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

	tt.Move(5, 5)
	tt.Frame()
	commitRowFill := func() color.RGBA {
		t.Helper()
		rect, ok := tt.Find("Commit diff of hello.txt")
		if !ok {
			t.Fatalf("commit file row is missing; texts: %q", tt.Texts())
		}
		img := tt.Image()
		return img.RGBAAt(int(rect.X+2), int(rect.Y+rect.H/2))
	}
	baseFill := commitRowFill()

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
	tt.Move(5, 5)
	tt.Frame()
	if selectedFill := commitRowFill(); selectedFill == baseFill {
		t.Errorf("previewed commit file row keeps the unselected fill %v", baseFill)
	}
	if err := tt.Click("Close"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return !app.Git.PreviewOpen })
	tt.Move(5, 5)
	tt.Frame()
	if got := commitRowFill(); got != baseFill {
		t.Errorf("closed commit file row fill = %v, want the unselected %v", got, baseFill)
	}

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

// TestGitDetailLabelsFitTheirColumn keeps the card's label column as wide as
// its widest label at every Git font size: a fixed 38 DIP split "Changes"
// onto two lines once the font grew.
func TestGitDetailLabelsFitTheirColumn(t *testing.T) {
	app := New(Options{Agents: []agent.LaunchSpec{}})
	t.Cleanup(app.Shutdown)
	for _, scale := range []int{minFontScale, 100, maxFontScale} {
		app.Fonts.Git = scale
		var column, label float32
		tt := mui.NewTester(func(c *mui.Context) {
			column = app.gitDetailLabelColumn(c)
			label, _ = c.MeasureText(0, mui.Span{Text: "Changes", Size: app.gitFont(8)})
			mui.Text(c, "x")
		}, 200, 100)
		tt.Frame()
		if column <= label {
			t.Errorf("label column %v at %d%% is not wider than the Changes label %v", column, scale, label)
		}
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

func runGitPanelOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
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

func TestGitPreviewSelectionMatchesSide(t *testing.T) {
	app := &App{Workspace: "/workspace"}
	app.Git.Workspace = "/workspace"
	app.Git.PreviewOpen = true
	app.Git.PreviewPath = "hello.txt"
	if !app.gitPreviewSelected("hello.txt", "", false) {
		t.Error("unstaged row does not match its preview")
	}
	if app.gitPreviewSelected("hello.txt", "", true) {
		t.Error("staged row matches an unstaged preview")
	}

	app.Git.PreviewStaged = true
	if !app.gitPreviewSelected("hello.txt", "", true) {
		t.Error("staged row does not match its preview")
	}
	if app.gitPreviewSelected("hello.txt", "", false) {
		t.Error("unstaged row matches a staged preview")
	}

	app.Git.PreviewCommit = "abc123"
	app.Git.PreviewStaged = false
	if !app.gitPreviewSelected("hello.txt", "abc123", false) {
		t.Error("commit file row does not match its preview")
	}
	if app.gitPreviewSelected("hello.txt", "", false) ||
		app.gitPreviewSelected("hello.txt", "def456", false) {
		t.Error("another side matches a commit preview")
	}

	app.Git.Workspace = "/elsewhere"
	if app.gitPreviewSelected("hello.txt", "abc123", false) {
		t.Error("a preview from another workspace still matches")
	}
}

func TestGitPanelMarksPreviewedFileRow(t *testing.T) {
	workspace := initGitPanelRepo(t)
	writeGitPanelFile(t, workspace, "hello.txt", "one\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "first")
	writeGitPanelFile(t, workspace, "hello.txt", "two\n")

	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && len(app.Git.Changes) == 1
	})
	rowFill := func() color.RGBA {
		t.Helper()
		rect, ok := tt.Find("Diff of hello.txt")
		if !ok {
			t.Fatalf("change row is missing; texts: %q", tt.Texts())
		}
		img := tt.Image()
		return img.RGBAAt(int(rect.X+1), int(rect.Y+rect.H/2))
	}
	tt.Move(5, 5)
	tt.Frame()
	base := rowFill()

	if err := tt.Click("Diff of hello.txt"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Git.PreviewOpen && !app.Git.PreviewBusy && app.Git.PreviewNew != ""
	})
	tt.Move(5, 5)
	tt.Frame()
	if selected := rowFill(); selected == base {
		t.Errorf("previewed row keeps the unselected fill %v", base)
	}

	if err := tt.Click("Close"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return !app.Git.PreviewOpen && app.Git.PreviewPath == "" })
	tt.Move(5, 5)
	tt.Frame()
	if got := rowFill(); got != base {
		t.Errorf("closed row fill = %v, want the unselected %v", got, base)
	}
}

func TestGitPanelEditsWorkingTreeFile(t *testing.T) {
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
	if !app.Git.PreviewCanEdit {
		t.Fatal("an unstaged working-tree file should be editable")
	}

	if err := tt.Click("Show file content"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.Git.PreviewMode != viewerView || !tt.HasText("File · hello.txt") {
		t.Fatalf("view mode = %v; texts: %q", app.Git.PreviewMode, tt.Texts())
	}
	if err := tt.Click("Edit file"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.Git.PreviewMode != viewerEdit || !tt.HasText("Edit · hello.txt") {
		t.Fatalf("edit mode = %v; texts: %q", app.Git.PreviewMode, tt.Texts())
	}

	app.saveGitPreview("three\n")
	waitFor(t, app, tt, func() bool {
		return !app.Git.PreviewSaving && app.Git.PreviewNew == "three\n" && !app.Git.Loading
	})
	body, err := os.ReadFile(filepath.Join(workspace, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "three\n" {
		t.Fatalf("saved file = %q", body)
	}
	if app.Git.PreviewNotice != "Saved." {
		t.Errorf("preview notice = %q", app.Git.PreviewNotice)
	}

	if err := tt.Click("Close"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return !app.Git.PreviewOpen })

	// The index side is read-only: no Edit button, and the state refuses.
	if err := tt.Click("Stage all"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return len(app.Git.Staged) == 1 && len(app.Git.Changes) == 0 && app.Git.Busy == "" && !app.Git.Loading
	})
	if err := tt.Click("Diff of hello.txt"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Git.PreviewOpen && app.Git.PreviewStaged && !app.Git.PreviewBusy
	})
	if app.Git.PreviewCanEdit {
		t.Error("the staged side should not be editable")
	}
	if _, ok := tt.Find("Edit file"); ok {
		t.Error("the staged diff still offers Edit")
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
		t.Errorf("binary error is missing from the card; texts: %q", tt.Texts())
	}
	if _, ok := tt.Find("Edit file"); ok {
		t.Error("binary diff still offers editing")
	}
	app.saveGitPreview("")
	body, err := os.ReadFile(filepath.Join(workspace, "blob.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "head\x00tail\n" {
		t.Errorf("a failed preview let Save overwrite the file: %q", body)
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

func TestViewerCardLayoutRespondsToWidth(t *testing.T) {
	app := &App{}
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
		got := app.viewerCardWidth(c.window)
		if diff := got - c.width; diff > 0.5 || diff < -0.5 {
			t.Errorf("card width for a %v DIP window = %v, want %v", c.window, got, c.width)
		}
		if split := viewerSplit(got); split != c.split {
			t.Errorf("card %v DIPs wide splits = %v, want %v", got, split, c.split)
		}
	}

	app.ViewerWidth = 1100
	if got, want := app.viewerCardWidth(1240), app.viewerMaxWidth(1240); got != want {
		t.Errorf("dragged card width = %v, want the clamped %v", got, want)
	}
	app.ViewerWidth = 120
	if got := app.viewerCardWidth(1600); got != viewerMinWidth {
		t.Errorf("tiny dragged width = %v, want %v", got, viewerMinWidth)
	}
}
