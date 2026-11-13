package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadStatusGraphAndActions(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(state.Root, dir) {
		t.Errorf("root = %q, want %q", state.Root, dir)
	}
	if state.Branch == "" || state.HasCommits {
		t.Errorf("branch = %q, hasCommits = %v", state.Branch, state.HasCommits)
	}
	if len(state.Changes()) != 1 || state.Changes()[0].Path != "new.txt" {
		t.Fatalf("changes = %+v", state.Changes())
	}

	if err := StageAll(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	state, err = Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Staged(); len(got) != 1 || got[0].Path != "new.txt" {
		t.Fatalf("staged = %+v", got)
	}
	if len(state.Changes()) != 0 {
		t.Fatalf("changes after stage = %+v", state.Changes())
	}

	if err := Commit(context.Background(), dir, "first commit"); err != nil {
		t.Fatal(err)
	}
	state, err = Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !state.HasCommits || !hasCommitMessage(state.Commits, "first commit") {
		t.Fatalf("commits = %+v, hasCommits = %v", state.Commits, state.HasCommits)
	}

	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err = Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Changes()) != 1 || state.Changes()[0].WorktreeCode() != "M" {
		t.Fatalf("modified changes = %+v", state.Changes())
	}
	if err := Stage(context.Background(), dir, "new.txt"); err != nil {
		t.Fatal(err)
	}
	if err := Unstage(context.Background(), dir, "new.txt"); err != nil {
		t.Fatal(err)
	}
	state, err = Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Staged()) != 0 {
		t.Fatalf("staged after unstage = %+v", state.Staged())
	}

	if err := UndoLastCommit(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	state, err = Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if state.HasCommits {
		t.Fatal("undo last commit left a commit behind")
	}
	if len(state.Staged()) == 0 {
		t.Fatal("soft undo did not keep the changes staged")
	}
}

func TestDiscardAllClearsWorkTree(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tracked := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, dir, "add", ".")
	runTestGit(t, dir, "commit", "-qm", "first")

	// A staged edit on top of an unstaged one, an untracked file, an untracked
	// folder, and an ignored file that must survive.
	if err := os.WriteFile(tracked, []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, dir, "add", "tracked.txt")
	if err := os.WriteFile(tracked, []byte("three\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(dir, "scratch", "nested")
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "x.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.log"), []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := DiscardAll(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	state, err := Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Staged()) != 0 || len(state.Changes()) != 0 {
		t.Fatalf("after discard staged = %+v, changes = %+v", state.Staged(), state.Changes())
	}
	data, err := os.ReadFile(tracked)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "one\n" {
		t.Errorf("tracked file = %q, want the committed contents", data)
	}
	for _, gone := range []string{"new.txt", "scratch"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s survived the discard (err = %v)", gone, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "ignored.log")); err != nil {
		t.Errorf("ignored file was removed: %v", err)
	}
}

func TestDiscardAllBeforeFirstCommit(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "first.txt"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := StageAll(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if err := DiscardAll(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	state, err := Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Staged()) != 0 || len(state.Changes()) != 0 {
		t.Fatalf("after discard staged = %+v, changes = %+v", state.Staged(), state.Changes())
	}
	if _, err := os.Stat(filepath.Join(dir, "first.txt")); !os.IsNotExist(err) {
		t.Errorf("staged file survived the discard (err = %v)", err)
	}
}

func TestLoadNestedWorkspaceAndRename(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	dir := initRepo(t)
	nested := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, dir, "add", "old.txt")
	runTestGit(t, dir, "commit", "-qm", "first")
	runTestGit(t, dir, "mv", "old.txt", "new.txt")

	state, err := Load(context.Background(), nested)
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(state.Root, dir) {
		t.Fatalf("nested root = %q, want %q", state.Root, dir)
	}
	staged := state.Staged()
	if len(staged) != 1 || staged[0].Path != "new.txt" || staged[0].OrigPath != "old.txt" {
		t.Fatalf("rename status = %+v", staged)
	}
}

func TestInitAndNotRepository(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	if _, err := Load(context.Background(), dir); !errors.Is(err, ErrNotRepository) {
		t.Fatalf("Load outside repo = %v", err)
	}
	if err := Init(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	state, err := Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(state.Root, dir) || state.HasCommits {
		t.Fatalf("initialized state = %+v", state)
	}
}

func TestParseBranchHeader(t *testing.T) {
	for _, tc := range []struct {
		header string
		branch string
		ahead  int
		behind int
	}{
		{"main", "main", 0, 0},
		{"main...origin/main [ahead 1, behind 2]", "main", 1, 2},
		{"feature/x...origin/feature/x [gone]", "feature/x", 0, 0},
	} {
		result := statusResult{hasCommits: true}
		parseBranch(tc.header, &result)
		if result.branch != tc.branch || result.ahead != tc.ahead || result.behind != tc.behind {
			t.Errorf("%q = branch %q ahead %d behind %d", tc.header, result.branch, result.ahead, result.behind)
		}
	}
	result := statusResult{hasCommits: true}
	parseBranch("No commits yet on main", &result)
	if result.branch != "main" || result.hasCommits {
		t.Errorf("unborn = %+v", result)
	}
}

func hasCommitMessage(commits []LogCommit, want string) bool {
	for _, c := range commits {
		if strings.Contains(c.Message, want) {
			return true
		}
	}
	return false
}

func samePath(a, b string) bool {
	canonical := func(path string) string {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			return resolved
		}
		return filepath.Clean(path)
	}
	return canonical(a) == canonical(b)
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runTestGit(t, dir, "init", "-q", "-b", "master")
	runTestGit(t, dir, "config", "user.email", "test@example.com")
	runTestGit(t, dir, "config", "user.name", "Test User")
	return dir
}

func runTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
