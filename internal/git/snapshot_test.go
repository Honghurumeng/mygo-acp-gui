package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeSnapshotRestoresPreTaskState(t *testing.T) {
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
	runTestGit(t, dir, "add", "-A")
	runTestGit(t, dir, "commit", "-qm", "base")

	// Pre-task state: a staged edit, a different work-tree edit, an untracked
	// file, and an ignored file. All of it must survive a failed task.
	if err := os.WriteFile(tracked, []byte("staged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, dir, "add", "tracked.txt")
	if err := os.WriteFile(tracked, []byte("worktree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pre := filepath.Join(dir, "pre.txt")
	if err := os.WriteFile(pre, []byte("pre\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ignored := filepath.Join(dir, "ignored.log")
	if err := os.WriteFile(ignored, []byte("keep ignored\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	snapshot, err := SnapshotWorktree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Root == "" || snapshot.Tree == "" || snapshot.IndexTree == "" {
		t.Fatalf("snapshot = %+v", snapshot)
	}

	// What a task does: change tracked and pre-existing files, create a file
	// and a directory, and touch an ignored file. The ignored file is not
	// part of the snapshot and should not be restored or removed.
	if err := os.WriteFile(tracked, []byte("task\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pre, []byte("task changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "task.txt"), []byte("task\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "taskdir", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "taskdir", "nested", "x.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ignored, []byte("task ignored\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := snapshot.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(tracked)
	if err != nil || string(data) != "worktree\n" {
		t.Fatalf("tracked work-tree = %q, err = %v", data, err)
	}
	indexed, err := gitIndexContent(dir, "tracked.txt")
	if err != nil || indexed != "staged\n" {
		t.Fatalf("tracked index = %q, err = %v", indexed, err)
	}
	data, err = os.ReadFile(pre)
	if err != nil || string(data) != "pre\n" {
		t.Fatalf("pre-existing untracked = %q, err = %v", data, err)
	}
	for _, gone := range []string{"task.txt", "taskdir"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s survived the snapshot restore (err = %v)", gone, err)
		}
	}
	if data, err := os.ReadFile(ignored); err != nil || string(data) != "task ignored\n" {
		t.Errorf("ignored file = %q, err = %v; restore should leave it alone", data, err)
	}

	state, err := Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Staged()) != 1 || len(state.Changes()) != 2 {
		// tracked.txt is staged + unstaged; pre.txt is untracked; .gitignore
		// and ignored.log are not changes.
		t.Fatalf("restored state staged = %+v changes = %+v", state.Staged(), state.Changes())
	}
}

func TestWorktreeSnapshotBeforeFirstCommit(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	dir := initRepo(t)
	pre := filepath.Join(dir, "pre.txt")
	if err := os.WriteFile(pre, []byte("pre\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := SnapshotWorktree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Tree == "" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if err := os.WriteFile(filepath.Join(dir, "task.txt"), []byte("task\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(pre); err != nil || string(data) != "pre\n" {
		t.Fatalf("pre-existing file = %q, err = %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "task.txt")); !os.IsNotExist(err) {
		t.Errorf("task file survived before-first-commit restore (err = %v)", err)
	}
}

func gitIndexContent(dir, path string) (string, error) {
	cmd := exec.Command("git", "show", ":"+path)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)) + "\n", nil
}
