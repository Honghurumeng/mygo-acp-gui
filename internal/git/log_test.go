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
