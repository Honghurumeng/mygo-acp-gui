package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseNumstat(t *testing.T) {
	out := []byte("3\t1\tsrc/main.go\x00" +
		"-\t-\tassets/logo.png\x00" +
		"2\t4\t\x00old/name.go\x00new/name.go\x00" +
		"0\t0\tmode-only.sh\x00")
	added, removed := parseNumstat(out)
	if added != 5 || removed != 5 {
		t.Fatalf("added, removed = %d, %d; want 5, 5", added, removed)
	}
	if stat := parseNumstatStat(out); stat.Files != 4 || stat.Added != 5 || stat.Removed != 5 {
		t.Fatalf("stat = %+v; want 4 files, 5 added, 5 removed", stat)
	}
	if added, removed := parseNumstat(nil); added != 0 || removed != 0 {
		t.Errorf("empty out = %d, %d", added, removed)
	}
}

func TestLoadCountsDiffStat(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	dir := initRepo(t)
	writeTestFile(t, dir, "tracked.txt", "one\ntwo\n")
	writeTestFile(t, dir, "renamed.txt", "a\nb\nc\n")
	runTestGit(t, dir, "add", "-A")
	runTestGit(t, dir, "commit", "-q", "-m", "first")

	// One modified tracked file, one staged change, one rename and one
	// untracked file: the stat covers all of them against HEAD.
	writeTestFile(t, dir, "tracked.txt", "one\nchanged\nthree\n")
	writeTestFile(t, dir, "staged.txt", "new\n")
	runTestGit(t, dir, "add", "staged.txt")
	runTestGit(t, dir, "mv", "renamed.txt", "moved.txt")
	writeTestFile(t, dir, "untracked.txt", "x\ny\n")

	state, err := Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	stat := state.Stat
	if stat.Files != len(state.Files) || stat.Files != 4 {
		t.Fatalf("stat = %+v, files = %d", stat, len(state.Files))
	}
	// tracked.txt: +2 -1, staged.txt: +1, moved.txt rename: +0 -0,
	// untracked.txt: +2.
	if stat.Added != 5 || stat.Removed != 1 {
		t.Errorf("stat = %+v; want 5 added, 1 removed", stat)
	}
}

func TestLoadCountsStatBeforeFirstCommit(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	dir := initRepo(t)
	writeTestFile(t, dir, "staged.txt", "a\nb\n")
	runTestGit(t, dir, "add", "staged.txt")
	writeTestFile(t, dir, "untracked.txt", "c")

	state, err := Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if state.HasCommits {
		t.Fatalf("hasCommits = true before the first commit")
	}
	if stat := state.Stat; stat.Files != 2 || stat.Added != 3 || stat.Removed != 0 {
		t.Errorf("stat = %+v; want 2 files, 3 added", state.Stat)
	}
}

func TestLoadSkipsBinaryAndHugeUntrackedStat(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "binary.bin"), []byte("a\x00b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	huge := make([]byte, MaxPreview+1)
	for i := range huge {
		huge[i] = '\n'
	}
	if err := os.WriteFile(filepath.Join(dir, "huge.txt"), huge, 0o600); err != nil {
		t.Fatal(err)
	}

	state, err := Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if stat := state.Stat; stat.Files != 2 || stat.Added != 0 || stat.Removed != 0 {
		t.Errorf("stat = %+v; want 2 files and no line counts", stat)
	}
}

func TestLoadEmptyWorkTreeHasNoStat(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	dir := initRepo(t)
	writeTestFile(t, dir, "tracked.txt", "one\n")
	runTestGit(t, dir, "add", "-A")
	runTestGit(t, dir, "commit", "-q", "-m", "first")

	state, err := Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if stat := state.Stat; stat != (DiffStat{}) {
		t.Errorf("clean work tree stat = %+v", stat)
	}
}

func writeTestFile(t *testing.T, dir, name, text string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
