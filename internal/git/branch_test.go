package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func branchNames(branches []Branch) []string {
	names := make([]string, len(branches))
	for i, branch := range branches {
		names[i] = branch.Name
	}
	return names
}

func findBranch(branches []Branch, name string) (Branch, bool) {
	for _, branch := range branches {
		if branch.Name == name {
			return branch, true
		}
	}
	return Branch{}, false
}

func TestBranchesCreateAndSwitch(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	dir := initRepo(t)
	ctx := context.Background()

	// Before the first commit HEAD still names an unborn branch.
	branches, err := Branches(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 || branches[0].Name != "master" || !branches[0].Current {
		t.Fatalf("unborn branches = %+v", branches)
	}

	if err := os.WriteFile(filepath.Join(dir, "one.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, dir, "add", ".")
	runTestGit(t, dir, "commit", "-qm", "one")
	one := revParseTest(t, dir, "HEAD")

	if err := os.WriteFile(filepath.Join(dir, "two.txt"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, dir, "add", ".")
	runTestGit(t, dir, "commit", "-qm", "two")
	two := revParseTest(t, dir, "HEAD")

	if err := CreateBranch(ctx, dir, "feature/topic", one); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	branches, err = Branches(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := branchNames(branches); len(got) != 2 || got[0] != "master" || got[1] != "feature/topic" {
		t.Fatalf("branches = %v", got)
	}
	if !branches[0].Current {
		t.Fatalf("master is not marked current: %+v", branches[0])
	}

	if err := SwitchBranch(ctx, dir, "feature/topic"); err != nil {
		t.Fatalf("SwitchBranch: %v", err)
	}
	if got := revParseTest(t, dir, "HEAD"); got != one {
		t.Fatalf("feature HEAD = %s, want %s", got, one)
	}
	branches, err = Branches(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	feature, ok := findBranch(branches, "feature/topic")
	if !ok || !feature.Current {
		t.Fatalf("feature branch is not current: %+v", branches)
	}
	if master, ok := findBranch(branches, "master"); !ok || master.Current {
		t.Fatalf("master still current after switch: %+v", branches)
	}

	if err := SwitchBranch(ctx, dir, "master"); err != nil {
		t.Fatalf("switch back: %v", err)
	}
	if got := revParseTest(t, dir, "HEAD"); got != two {
		t.Fatalf("master HEAD = %s, want %s", got, two)
	}

	if err := CreateBranch(ctx, dir, "bad name", one); err == nil {
		t.Fatal("CreateBranch accepted a name with a space")
	}
	if err := CreateBranch(ctx, dir, "-nope", one); err == nil {
		t.Fatal("CreateBranch accepted an option-like name")
	}
	if err := CreateBranch(ctx, dir, "master", one); err == nil {
		t.Fatal("CreateBranch replaced an existing branch")
	}
	if err := SwitchBranch(ctx, dir, "missing"); err == nil {
		t.Fatal("SwitchBranch accepted a missing branch")
	}
}

func TestPushBranchSetsUpstream(t *testing.T) {
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

	ctx := context.Background()
	if err := CreateBranch(ctx, work, "feature/new", "HEAD"); err != nil {
		t.Fatal(err)
	}
	if err := SwitchBranch(ctx, work, "feature/new"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "feature.txt"), []byte("feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, work, "add", ".")
	runTestGit(t, work, "commit", "-q", "-m", "feature work")
	head := revParseTest(t, work, "HEAD")

	if err := PushBranch(ctx, work); err != nil {
		t.Fatalf("PushBranch without upstream: %v", err)
	}
	if got := revParseTest(t, origin, "refs/heads/feature/new"); got != head {
		t.Fatalf("origin feature/new = %s, want %s", got, head)
	}
	upstream, err := run(ctx, work, nil, "rev-parse", "--abbrev-ref", "@{upstream}")
	if err != nil {
		t.Fatalf("read upstream: %v", err)
	}
	if got := strings.TrimSpace(string(upstream)); got != "origin/feature/new" {
		t.Fatalf("upstream = %q, want origin/feature/new", got)
	}
	branches, err := Branches(ctx, work)
	if err != nil {
		t.Fatal(err)
	}
	feature, ok := findBranch(branches, "feature/new")
	if !ok || feature.Upstream != "origin/feature/new" {
		t.Fatalf("feature upstream in Branches = %+v", feature)
	}

	// A second push takes the configured upstream path.
	if err := os.WriteFile(filepath.Join(work, "second.txt"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, work, "add", ".")
	runTestGit(t, work, "commit", "-q", "-m", "second feature work")
	second := revParseTest(t, work, "HEAD")
	if err := PushBranch(ctx, work); err != nil {
		t.Fatalf("PushBranch with upstream: %v", err)
	}
	if got := revParseTest(t, origin, "refs/heads/feature/new"); got != second {
		t.Fatalf("origin feature/new after second push = %s, want %s", got, second)
	}
}

func TestPushBranchWithoutRemote(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "first.txt"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, dir, "add", ".")
	runTestGit(t, dir, "commit", "-q", "-m", "first")
	err := PushBranch(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "no remote") {
		t.Fatalf("PushBranch without a remote = %v", err)
	}
}

func TestPullBranchUsesMatchingRemote(t *testing.T) {
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
	runTestGit(t, other, "switch", "-q", "-c", "feature/pull")
	if err := os.WriteFile(filepath.Join(other, "remote.txt"), []byte("remote\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, other, "add", ".")
	runTestGit(t, other, "commit", "-q", "-m", "remote work")
	remoteHead := revParseTest(t, other, "HEAD")
	runTestGit(t, other, "push", "-q", "-u", "origin", "feature/pull")

	ctx := context.Background()
	if err := Fetch(ctx, work); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, work, "branch", "feature/pull", "master")
	runTestGit(t, work, "switch", "-q", "feature/pull")
	if err := PullBranch(ctx, work); err != nil {
		t.Fatalf("PullBranch without upstream: %v", err)
	}
	if got := revParseTest(t, work, "HEAD"); got != remoteHead {
		t.Fatalf("HEAD after pull = %s, want %s", got, remoteHead)
	}
	if data, err := os.ReadFile(filepath.Join(work, "remote.txt")); err != nil || string(data) != "remote\n" {
		t.Fatalf("pulled file = %q, err = %v", data, err)
	}
	upstream, err := run(ctx, work, nil, "rev-parse", "--abbrev-ref", "@{upstream}")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(upstream)); got != "origin/feature/pull" {
		t.Fatalf("upstream = %q, want origin/feature/pull", got)
	}
}

func TestSwitchBranchCarryingChanges(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	dir := initRepo(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "local.txt"), []byte("committed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, dir, "add", ".")
	runTestGit(t, dir, "commit", "-qm", "base")
	runTestGit(t, dir, "switch", "-q", "-c", "feature")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, dir, "commit", "-qam", "feature")
	runTestGit(t, dir, "switch", "-q", "master")

	// A clean file change unrelated to the branch difference travels with
	// the checkout instead of blocking it.
	if err := os.WriteFile(filepath.Join(dir, "local.txt"), []byte("local edit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := SwitchBranchCarryingChanges(ctx, dir, "feature")
	if err != nil {
		t.Fatalf("SwitchBranchCarryingChanges: %v", err)
	}
	if result.RestoreConflict || result.StashRef != "" {
		t.Fatalf("clean carry result = %+v", result)
	}
	if got := currentBranch(ctx, dir); got != "feature" {
		t.Fatalf("branch = %q, want feature", got)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "local.txt")); err != nil || string(data) != "local edit\n" {
		t.Fatalf("carried file = %q, err = %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "base.txt")); err != nil || string(data) != "feature\n" {
		t.Fatalf("feature file = %q, err = %v", data, err)
	}
	if out, err := run(ctx, dir, nil, "stash", "list"); err != nil || len(strings.TrimSpace(string(out))) != 0 {
		t.Fatalf("stash after clean carry = %q, err = %v", out, err)
	}

	// A modification of the file the branches disagree about cannot be
	// reapplied cleanly: the switch happens, the stash stays.
	runTestGit(t, dir, "switch", "-q", "master")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("local base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = SwitchBranchCarryingChanges(ctx, dir, "feature")
	if err != nil {
		t.Fatalf("conflicting SwitchBranchCarryingChanges: %v", err)
	}
	if !result.RestoreConflict || result.StashRef != "stash@{0}" {
		t.Fatalf("conflicting carry result = %+v", result)
	}
	if got := currentBranch(ctx, dir); got != "feature" {
		t.Fatalf("branch after conflicting carry = %q, want feature", got)
	}
	if out, err := run(ctx, dir, nil, "stash", "list"); err != nil || !strings.Contains(string(out), "stash@{0}") {
		t.Fatalf("stash after conflicting carry = %q, err = %v", out, err)
	}
}
