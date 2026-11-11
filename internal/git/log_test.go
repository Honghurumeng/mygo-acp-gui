package git

import (
	"context"
	"strings"
	"testing"
)

// commitLine builds one line of the unit-separated log.
func commitLine(hash, parents, author, subject, refs string) string {
	return strings.Join([]string{hash, parents, author, subject, refs}, "\x1f")
}

// fakeHash is a 40-character object id ending in n.
func fakeHash(n string) string {
	return strings.Repeat("a", 40-len(n)) + n
}

func TestParseCommitsSkipsNoiseAndFillsSubject(t *testing.T) {
	out := []byte(strings.Join([]string{
		"warning: something on stderr",
		commitLine(fakeHash("1"), fakeHash("2"), "Ana", "first", "HEAD -> main, tag: v1, origin/main"),
		"",
		commitLine(fakeHash("2"), "", "Kai", "", ""),
		commitLine("not-a-hash", "", "Ana", "ignored", ""),
	}, "\n"))

	commits := parseCommits(out)
	if len(commits) != 2 {
		t.Fatalf("commits = %+v", commits)
	}
	if commits[0].Message != "first" || commits[0].Author != "Ana" {
		t.Errorf("first = %+v", commits[0])
	}
	if got := commits[0].Refs; len(got) != 3 || got[0] != "main" || got[1] != "v1" || got[2] != "origin/main" {
		t.Errorf("refs = %q", got)
	}
	if commits[0].Parents[0] != fakeHash("2") {
		t.Errorf("parents = %q", commits[0].Parents)
	}
	if commits[1].Message != "(no subject)" {
		t.Errorf("empty subject = %q", commits[1].Message)
	}
}

func TestParseRefsDropsDuplicatesAndTargets(t *testing.T) {
	got := parseRefs("HEAD -> main, origin/HEAD -> origin/main, origin/main, main, tag: v1, v1")
	want := []string{"main", "origin/main", "v1"}
	if len(got) != len(want) {
		t.Fatalf("refs = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("refs = %q, want %q", got, want)
		}
	}
}

func TestAssignLanesFollowsMergesAndSortsParentsLast(t *testing.T) {
	a, b, c, d := fakeHash("a"), fakeHash("b"), fakeHash("c"), fakeHash("d")
	commits := []LogCommit{
		{Hash: a, Parents: []string{b, c}},
		{Hash: b, Parents: []string{d}},
		{Hash: c, Parents: []string{d}},
		{Hash: d},
	}
	assignLanes(commits)

	want := []int{0, 0, 1, 0}
	for i, lane := range want {
		if commits[i].Lane != lane {
			t.Fatalf("lane of %s = %d, want %d", commits[i].Hash, commits[i].Lane, lane)
		}
	}
	assertGraphOrder(t, commits)
}

func TestAssignLanesStaysInsideTheBand(t *testing.T) {
	var commits []LogCommit
	for i := 0; i < GraphLanes*2; i++ {
		commits = append(commits, LogCommit{Hash: fakeHash(string(rune('a' + i)))})
	}
	assignLanes(commits)
	for _, c := range commits {
		if c.Lane < 0 || c.Lane >= GraphLanes {
			t.Fatalf("lane of %s = %d", c.Hash, c.Lane)
		}
	}
}

// assertGraphOrder checks what a graph component requires of its input.
func assertGraphOrder(t *testing.T, commits []LogCommit) {
	t.Helper()
	row := map[string]int{}
	for i, c := range commits {
		row[c.Hash] = i
	}
	for i, c := range commits {
		for _, parent := range c.Parents {
			j, ok := row[parent]
			if ok && j <= i {
				t.Fatalf("%s is listed before its parent %s", c.Hash, parent)
			}
		}
	}
}

func TestReadCommitsOrdersMergesAndRefs(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	dir := initRepo(t)
	writeFile(t, dir, "base.txt", "base\n")
	runTestGit(t, dir, "add", ".")
	runTestGit(t, dir, "commit", "-q", "-m", "base")
	runTestGit(t, dir, "tag", "v1")

	runTestGit(t, dir, "checkout", "-q", "-b", "feature")
	writeFile(t, dir, "feature.txt", "feature\n")
	runTestGit(t, dir, "add", ".")
	runTestGit(t, dir, "commit", "-q", "-m", "feature work")

	runTestGit(t, dir, "checkout", "-q", "master")
	writeFile(t, dir, "main.txt", "main\n")
	runTestGit(t, dir, "add", ".")
	runTestGit(t, dir, "commit", "-q", "-m", "main work")
	runTestGit(t, dir, "merge", "-q", "--no-ff", "-m", "merge feature", "feature")

	commits, err := readCommits(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 4 {
		t.Fatalf("commits = %+v", commits)
	}
	if commits[0].Message != "merge feature" || len(commits[0].Parents) != 2 {
		t.Fatalf("head = %+v", commits[0])
	}
	if !hasRef(commits[0].Refs, "master") {
		t.Errorf("head refs = %q", commits[0].Refs)
	}
	for _, c := range commits {
		if c.Hash == "" || c.Message == "" || c.Lane < 0 || c.Lane >= GraphLanes {
			t.Fatalf("commit = %+v", c)
		}
	}
	assertGraphOrder(t, commits)
}

func hasRef(refs []string, want string) bool {
	for _, ref := range refs {
		if ref == want {
			return true
		}
	}
	return false
}

func TestCommitFilesListsChangesAgainstFirstParent(t *testing.T) {
	ctx := context.Background()
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a\n")
	writeFile(t, dir, "keep.txt", "k\n")
	runTestGit(t, dir, "add", ".")
	runTestGit(t, dir, "commit", "-q", "-m", "base")
	base := headHash(t, dir)

	writeFile(t, dir, "b.txt", "b\n")
	runTestGit(t, dir, "add", ".")
	runTestGit(t, dir, "commit", "-q", "-m", "add b")
	second := headHash(t, dir)

	runTestGit(t, dir, "mv", "a.txt", "renamed.txt")
	runTestGit(t, dir, "commit", "-q", "-m", "rename")
	renamed := headHash(t, dir)

	files, err := CommitFiles(ctx, dir, second)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "b.txt" || files[0].Status != 'A' {
		t.Fatalf("second commit = %+v", files)
	}

	files, err = CommitFiles(ctx, dir, renamed)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Status != 'R' || files[0].Path != "renamed.txt" || files[0].OrigPath != "a.txt" {
		t.Fatalf("rename commit = %+v", files)
	}

	files, err = CommitFiles(ctx, dir, base)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]byte{}
	for _, f := range files {
		got[f.Path] = f.Status
	}
	if len(got) != 2 || got["a.txt"] != 'A' || got["keep.txt"] != 'A' {
		t.Fatalf("root commit = %+v", files)
	}

	if _, err := CommitFiles(ctx, dir, "not-a-hash"); err == nil {
		t.Error("CommitFiles accepted a short hash")
	}
}

// headHash is the full hash of HEAD, for CommitFiles.
func headHash(t *testing.T, dir string) string {
	t.Helper()
	out, err := run(context.Background(), dir, nil, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestCommitDiffReadsBothSides(t *testing.T) {
	ctx := context.Background()
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "one\n")
	runTestGit(t, dir, "add", ".")
	runTestGit(t, dir, "commit", "-q", "-m", "base")

	writeFile(t, dir, "a.txt", "two\n")
	runTestGit(t, dir, "add", ".")
	runTestGit(t, dir, "commit", "-q", "-m", "change")
	changed := headHash(t, dir)
	files, err := CommitFiles(ctx, dir, changed)
	if err != nil {
		t.Fatal(err)
	}
	old, new, err := CommitDiff(ctx, dir, changed, files[0])
	if err != nil {
		t.Fatal(err)
	}
	if old != "one\n" || new != "two\n" {
		t.Fatalf("modified file sides = %q → %q", old, new)
	}

	runTestGit(t, dir, "mv", "a.txt", "b.txt")
	runTestGit(t, dir, "commit", "-q", "-m", "rename")
	renamed := headHash(t, dir)
	files, err = CommitFiles(ctx, dir, renamed)
	if err != nil {
		t.Fatal(err)
	}
	old, new, err = CommitDiff(ctx, dir, renamed, files[0])
	if err != nil {
		t.Fatal(err)
	}
	if old != "two\n" || new != "two\n" {
		t.Fatalf("renamed file sides = %q → %q", old, new)
	}

	runTestGit(t, dir, "rm", "-q", "b.txt")
	runTestGit(t, dir, "commit", "-q", "-m", "delete")
	deleted := headHash(t, dir)
	files, err = CommitFiles(ctx, dir, deleted)
	if err != nil {
		t.Fatal(err)
	}
	old, new, err = CommitDiff(ctx, dir, deleted, files[0])
	if err != nil {
		t.Fatal(err)
	}
	if old != "two\n" || new != "" {
		t.Fatalf("deleted file sides = %q → %q", old, new)
	}
}
