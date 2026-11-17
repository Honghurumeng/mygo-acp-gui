package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGitPanelCreatesBranchFromCommit drives the graph's context menu: the
// branch must start at the right-clicked commit and leave the checked-out
// branch and the work tree alone.
func TestGitPanelCreatesBranchFromCommit(t *testing.T) {
	workspace := initGitPanelRepo(t)
	writeGitPanelFile(t, workspace, "first.txt", "first\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "initial commit")
	first := gitPanelOutput(t, workspace, "rev-parse", "HEAD")
	writeGitPanelFile(t, workspace, "second.txt", "second\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "second commit")
	currentBranch := gitPanelOutput(t, workspace, "symbolic-ref", "--short", "HEAD")

	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && len(app.Git.Commits) == 2
	})

	if err := tt.RightClick("initial commit"); err != nil {
		t.Fatal(err)
	}
	if menu := tt.Menu(); len(menu) != 1 || menu[0] != "Create branch here…" {
		t.Fatalf("commit context menu = %v", menu)
	}
	if err := tt.ChooseMenuItem("Create branch here…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.Git.BranchCreateOpen || app.Git.BranchCreateBase != first {
		t.Fatalf("branch dialog = open %v base %q, want %q", app.Git.BranchCreateOpen, app.Git.BranchCreateBase, first)
	}
	if !tt.HasText("Create branch") {
		t.Fatalf("branch dialog title missing; texts: %q", tt.Texts())
	}

	app.Git.BranchCreateName = "feature/from-commit"
	tt.Frame()
	if err := tt.Click("Create"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !app.Git.BranchCreateOpen && !app.Git.Loading && app.Git.Busy == "" &&
			app.Git.Notice == "Branch feature/from-commit created."
	})
	if got := gitPanelOutput(t, workspace, "rev-parse", "feature/from-commit"); got != first {
		t.Fatalf("feature/from-commit = %s, want the right-clicked commit %s", got, first)
	}
	if got := gitPanelOutput(t, workspace, "symbolic-ref", "--short", "HEAD"); got != currentBranch {
		t.Fatalf("checkout moved to %q; the branch dialog must not switch", got)
	}
	if !tt.HasText("feature/from") {
		t.Errorf("new branch ref missing from the graph; texts: %q", tt.Texts())
	}
}

// TestComposerSwitchesBranchBeforeSession makes the branch chip a picker in
// a draft and checks that the checkout changes the branch the chip reports.
func TestComposerSwitchesBranchBeforeSession(t *testing.T) {
	workspace := initGitPanelRepo(t)
	writeGitPanelFile(t, workspace, "base.txt", "base\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "base")
	runGitPanelCommand(t, workspace, "branch", "feature/composer")
	initial := gitPanelOutput(t, workspace, "symbolic-ref", "--short", "HEAD")

	app, tt := newGitPanelApp(t, workspace)
	chat := current(app)
	tt.Frame()
	if err := tt.Click("Git branch: " + initial); err != nil {
		t.Fatalf("open branch chip: %v", err)
	}
	waitFor(t, app, tt, func() bool {
		return chat.BranchPicker.Open && chat.BranchPicker.Busy == "" && len(chat.BranchPicker.Branches) >= 2
	})
	if err := tt.Click("Switch to branch feature/composer"); err != nil {
		t.Fatalf("switch branch: %v", err)
	}
	waitFor(t, app, tt, func() bool {
		return !chat.BranchPicker.Open && !chat.BranchPicker.Switching &&
			gitPanelOutput(t, workspace, "symbolic-ref", "--short", "HEAD") == "feature/composer"
	})
	if got := chat.gitBranch(); got != "feature/composer" {
		t.Fatalf("composer branch = %q, want feature/composer", got)
	}
	if !tt.HasText("Git branch: feature/composer") {
		t.Errorf("composer chip did not follow the checkout; texts: %q", tt.Texts())
	}
}

// TestGitPanelSwitchesBranchFromHeader covers the same picker anchored to
// the panel's branch line, and the status refresh that follows a checkout.
func TestGitPanelSwitchesBranchFromHeader(t *testing.T) {
	workspace := initGitPanelRepo(t)
	writeGitPanelFile(t, workspace, "base.txt", "base\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "base")
	runGitPanelCommand(t, workspace, "branch", "feature/panel")

	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && app.Git.RepoRoot != ""
	})
	if err := tt.Click("Switch Git branch"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Git.BranchPicker.Open && app.Git.BranchPicker.Busy == "" && len(app.Git.BranchPicker.Branches) >= 2
	})
	if err := tt.Click("Switch to branch feature/panel"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !app.Git.BranchPicker.Open && !app.Git.Loading && app.Git.Busy == "" &&
			app.Git.Branch == "feature/panel"
	})
	if got := gitPanelOutput(t, workspace, "symbolic-ref", "--short", "HEAD"); got != "feature/panel" {
		t.Fatalf("checked-out branch = %q, want feature/panel", got)
	}
	if !strings.Contains(strings.Join(tt.Texts(), " "), "feature/panel") {
		t.Errorf("panel does not show the new branch; texts: %q", tt.Texts())
	}
}

// TestGitPanelBranchCreateErrorMessage keeps a rejected name in the dialog.
func TestGitPanelBranchCreateErrorMessage(t *testing.T) {
	workspace := initGitPanelRepo(t)
	writeGitPanelFile(t, workspace, "base.txt", "base\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "base")

	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && len(app.Git.Commits) == 1
	})
	if err := tt.RightClick("base"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Create branch here…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	app.Git.BranchCreateName = "not a branch"
	tt.Frame()
	if err := tt.Click("Create"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Git.Busy == "" && app.Git.BranchCreateErr != ""
	})
	if !app.Git.BranchCreateOpen {
		t.Fatal("dialog closed on a rejected branch name")
	}
	if _, err := os.Stat(filepath.Join(workspace, ".git", "refs", "heads", "not a branch")); err == nil {
		t.Fatal("invalid branch was created")
	}
}

// TestGitPanelPushSetsUpstreamForNewBranch verifies the panel's Push button
// publishes a branch the graph created: it has no upstream yet, so the first
// push must set one instead of failing.
func TestGitPanelPushSetsUpstreamForNewBranch(t *testing.T) {
	workspace, origin := initGitPanelRemote(t)
	runGitPanelCommand(t, workspace, "switch", "-c", "feature/push")
	writeGitPanelFile(t, workspace, "feature.txt", "feature\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "feature work")
	head := gitPanelOutput(t, workspace, "rev-parse", "HEAD")

	app, tt := newGitPanelApp(t, workspace)
	waitFor(t, app, tt, func() bool {
		return app.Git.Checked && !app.Git.Loading && app.Git.Branch == "feature/push"
	})
	if err := tt.Click("Push"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !app.Git.Loading && app.Git.Busy == "" && app.Git.Notice == "Pushed."
	})
	if got := gitPanelOutput(t, origin, "rev-parse", "refs/heads/feature/push"); got != head {
		t.Fatalf("origin feature/push = %s, want %s", got, head)
	}
	if got := gitPanelOutput(t, workspace, "rev-parse", "--abbrev-ref", "@{upstream}"); got != "origin/feature/push" {
		t.Fatalf("upstream = %q, want origin/feature/push", got)
	}
}

// TestComposerCarriesLocalChangesAcrossBranch reproduces git's refusal to
// switch over local changes: the picker keeps its branch list, the fallback
// stashes, switches and restores, and a conflicting restore keeps the stash
// with a warning instead of losing the change.
func TestComposerCarriesLocalChangesAcrossBranch(t *testing.T) {
	workspace := initGitPanelRepo(t)
	writeGitPanelFile(t, workspace, "base.txt", "main\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "main base")
	initial := gitPanelOutput(t, workspace, "symbolic-ref", "--short", "HEAD")
	runGitPanelCommand(t, workspace, "switch", "-q", "-c", "feature/carry")
	writeGitPanelFile(t, workspace, "base.txt", "feature\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "feature base")
	runGitPanelCommand(t, workspace, "switch", "-q", initial)
	writeGitPanelFile(t, workspace, "base.txt", "local\n")

	app, tt := newGitPanelApp(t, workspace)
	chat := current(app)
	tt.Frame()
	if err := tt.Click("Git branch: " + initial); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return chat.BranchPicker.Open && chat.BranchPicker.Busy == "" && len(chat.BranchPicker.Branches) >= 2
	})
	if err := tt.Click("Switch to branch feature/carry"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return chat.BranchPicker.Err != "" && chat.BranchPicker.SwitchFailed == "feature/carry"
	})
	if _, ok := tt.Find("Switch to branch feature/carry"); !ok {
		t.Fatalf("failed switch hid the branch list; texts: %q", tt.Texts())
	}
	if !tt.HasText("Could not switch to feature/carry") {
		t.Errorf("switch summary missing; texts: %q", tt.Texts())
	}
	if !tt.HasText("Aborting") {
		t.Errorf("full git error missing; texts: %q", tt.Texts())
	}
	if !tt.HasText("git stash pop --index") {
		t.Errorf("stash & switch steps missing; texts: %q", tt.Texts())
	}
	if _, ok := tt.Find("Stash & switch"); !ok {
		t.Fatalf("carry changes action missing; texts: %q", tt.Texts())
	}

	if err := tt.Click("Stash & switch"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !chat.BranchPicker.Switching && chat.BranchPicker.Warning != "" &&
			gitPanelOutput(t, workspace, "symbolic-ref", "--short", "HEAD") == "feature/carry"
	})
	if !chat.BranchPicker.Open {
		t.Fatalf("conflicting restore closed the warning; state = %+v", chat.BranchPicker)
	}
	if !tt.HasText("local changes could not be restored") {
		t.Errorf("restore warning missing; texts: %q", tt.Texts())
	}
	if !tt.HasText("git stash drop stash@{0}") {
		t.Errorf("warning is truncated before its stash advice; texts: %q", tt.Texts())
	}
	if stashes := gitPanelOutput(t, workspace, "stash", "list"); !strings.Contains(stashes, "stash@{0}") {
		t.Fatalf("changes were not kept in a stash: %q", stashes)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "base.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "<<<<<<<") {
		t.Fatalf("conflict was not left for the user: %q", data)
	}
}
