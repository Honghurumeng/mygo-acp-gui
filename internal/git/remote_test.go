package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func initBareRemote(t *testing.T) string {
	t.Helper()
	if !Available() {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	runTestGit(t, dir, "init", "--bare", "-q")
	return dir
}

func cloneTestRepo(t *testing.T, origin string) string {
	t.Helper()
	parent := t.TempDir()
	runTestGit(t, parent, "clone", "-q", "-b", "master", origin, "work")
	dir := filepath.Join(parent, "work")
	runTestGit(t, dir, "config", "user.email", "test@example.com")
	runTestGit(t, dir, "config", "user.name", "Test User")
	return dir
}

func revParseTest(t *testing.T, dir, rev string) string {
	t.Helper()
	out, err := run(context.Background(), dir, nil, "rev-parse", rev)
	if err != nil {
		t.Fatalf("git rev-parse %s: %v", rev, err)
	}
	return strings.TrimSpace(string(out))
}

func TestFetchPullPushAndUnstageAll(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	origin := initBareRemote(t)
	work := t.TempDir()
	runTestGit(t, work, "init", "-q", "-b", "master")
	runTestGit(t, work, "config", "user.email", "test@example.com")
	runTestGit(t, work, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(work, "base.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, work, "add", ".")
	runTestGit(t, work, "commit", "-q", "-m", "base")
	runTestGit(t, work, "remote", "add", "origin", origin)
	runTestGit(t, work, "push", "-q", "-u", "origin", "master")

	other := cloneTestRepo(t, origin)
	if err := os.WriteFile(filepath.Join(other, "remote.txt"), []byte("remote\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, other, "add", ".")
	runTestGit(t, other, "commit", "-q", "-m", "remote work")
	runTestGit(t, other, "push", "-q", "origin", "master")
	remoteHead := revParseTest(t, other, "HEAD")

	ctx := context.Background()
	if err := Fetch(ctx, work); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := revParseTest(t, work, "refs/remotes/origin/master"); got != remoteHead {
		t.Fatalf("fetched origin/master = %s, want %s", got, remoteHead)
	}
	if _, err := os.Stat(filepath.Join(work, "remote.txt")); !os.IsNotExist(err) {
		t.Fatalf("fetch changed the work tree (err = %v)", err)
	}

	// A second fetch has nothing new but must still succeed.
	if err := Fetch(ctx, work); err != nil {
		t.Fatalf("second Fetch: %v", err)
	}

	if err := Pull(ctx, work); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if got := revParseTest(t, work, "HEAD"); got != remoteHead {
		t.Fatalf("HEAD after pull = %s, want %s", got, remoteHead)
	}
	if data, err := os.ReadFile(filepath.Join(work, "remote.txt")); err != nil || string(data) != "remote\n" {
		t.Fatalf("pulled file = %q, err = %v", data, err)
	}

	// Push publishes the local commit back to the configured upstream.
	if err := os.WriteFile(filepath.Join(work, "local.txt"), []byte("local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, work, "add", ".")
	runTestGit(t, work, "commit", "-q", "-m", "local work")
	localHead := revParseTest(t, work, "HEAD")
	if err := Push(ctx, work); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if got := revParseTest(t, origin, "master"); got != localHead {
		t.Fatalf("origin/master after push = %s, want %s", got, localHead)
	}

	// UnstageAll clears every path from the index but leaves the work tree.
	if err := os.WriteFile(filepath.Join(work, "staged.txt"), []byte("staged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, work, "add", ".")
	state, err := Load(ctx, work)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Staged()) != 1 {
		t.Fatalf("staged before UnstageAll = %+v", state.Staged())
	}
	if err := UnstageAll(ctx, work); err != nil {
		t.Fatalf("UnstageAll: %v", err)
	}
	state, err = Load(ctx, work)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Staged()) != 0 {
		t.Fatalf("staged after UnstageAll = %+v", state.Staged())
	}
	if len(state.Changes()) == 0 {
		t.Fatal("UnstageAll removed the work-tree change")
	}
}

func TestUnstageAllBeforeFirstCommit(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "first.txt"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, dir, "add", ".")
	state, err := Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Staged()) != 1 {
		t.Fatalf("staged = %+v", state.Staged())
	}
	if err := UnstageAll(context.Background(), dir); err != nil {
		t.Fatalf("UnstageAll before first commit: %v", err)
	}
	state, err = Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Staged()) != 0 || len(state.Changes()) != 1 {
		t.Fatalf("after UnstageAll staged = %+v changes = %+v", state.Staged(), state.Changes())
	}
}
