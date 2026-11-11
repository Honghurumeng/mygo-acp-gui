package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffSides(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	dir := initRepo(t)
	writeFile(t, dir, "hello.txt", "one\n")
	runTestGit(t, dir, "add", ".")
	runTestGit(t, dir, "commit", "-q", "-m", "first")
	writeFile(t, dir, "hello.txt", "two\n")

	file := File{Path: "hello.txt", Index: ' ', Worktree: 'M'}
	old, new, err := Diff(ctx, dir, file, false)
	if err != nil {
		t.Fatal(err)
	}
	if old != "one\n" || new != "two\n" {
		t.Fatalf("unstaged sides = %q → %q", old, new)
	}

	if err := Stage(ctx, dir, "hello.txt"); err != nil {
		t.Fatal(err)
	}
	file = File{Path: "hello.txt", Index: 'M', Worktree: ' '}
	if old, new, err = Diff(ctx, dir, file, true); err != nil {
		t.Fatal(err)
	}
	if old != "one\n" || new != "two\n" {
		t.Fatalf("staged sides = %q → %q", old, new)
	}
}

func TestDiffUntrackedAddedAndDeleted(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	dir := initRepo(t)
	writeFile(t, dir, "kept.txt", "kept\n")
	writeFile(t, dir, "gone.txt", "gone\n")
	runTestGit(t, dir, "add", ".")
	runTestGit(t, dir, "commit", "-q", "-m", "first")

	// Untracked: nothing to compare against.
	writeFile(t, dir, "new.txt", "fresh\n")
	old, new, err := Diff(ctx, dir, File{Path: "new.txt", Index: '?', Worktree: '?'}, false)
	if err != nil {
		t.Fatal(err)
	}
	if old != "" || new != "fresh\n" {
		t.Fatalf("untracked sides = %q → %q", old, new)
	}

	// Added: the index has the text, HEAD does not.
	if err := Stage(ctx, dir, "new.txt"); err != nil {
		t.Fatal(err)
	}
	old, new, err = Diff(ctx, dir, File{Path: "new.txt", Index: 'A', Worktree: ' '}, true)
	if err != nil {
		t.Fatal(err)
	}
	if old != "" || new != "fresh\n" {
		t.Fatalf("added sides = %q → %q", old, new)
	}

	// Deleted in the work tree: the index still has the text.
	if err := os.Remove(filepath.Join(dir, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	old, new, err = Diff(ctx, dir, File{Path: "gone.txt", Index: ' ', Worktree: 'D'}, false)
	if err != nil {
		t.Fatal(err)
	}
	if old != "gone\n" || new != "" {
		t.Fatalf("deleted sides = %q → %q", old, new)
	}

	// Deleted in the index: HEAD has the text, the index does not.
	if err := Stage(ctx, dir, "gone.txt"); err != nil {
		t.Fatal(err)
	}
	old, new, err = Diff(ctx, dir, File{Path: "gone.txt", Index: 'D', Worktree: ' '}, true)
	if err != nil {
		t.Fatal(err)
	}
	if old != "gone\n" || new != "" {
		t.Fatalf("staged delete sides = %q → %q", old, new)
	}
}

func TestDiffRenameComparesOriginalPath(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	dir := initRepo(t)
	writeFile(t, dir, "before.txt", "same text\n")
	runTestGit(t, dir, "add", ".")
	runTestGit(t, dir, "commit", "-q", "-m", "first")
	runTestGit(t, dir, "mv", "before.txt", "after.txt")

	file := File{Path: "after.txt", OrigPath: "before.txt", Index: 'R', Worktree: ' '}
	old, new, err := Diff(ctx, dir, file, true)
	if err != nil {
		t.Fatal(err)
	}
	if old != "same text\n" || new != "same text\n" {
		t.Fatalf("rename sides = %q → %q", old, new)
	}
}

func TestDiffRejectsBinaryAndHugeFiles(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), []byte("head\x00tail\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := Diff(ctx, dir, File{Path: "blob.bin", Index: '?', Worktree: '?'}, false)
	if !errors.Is(err, ErrBinaryFile) {
		t.Fatalf("binary error = %v", err)
	}

	writeFile(t, dir, "big.txt", strings.Repeat("x", MaxPreview+1))
	_, _, err = Diff(ctx, dir, File{Path: "big.txt", Index: '?', Worktree: '?'}, false)
	if !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("large error = %v", err)
	}
}

func TestDiffRejectsPathsOutsideRepository(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	dir := initRepo(t)
	for _, path := range []string{"../secret.txt", "/etc/passwd", ""} {
		if _, _, err := Diff(ctx, dir, File{Path: path, Index: ' ', Worktree: 'M'}, false); err == nil {
			t.Errorf("path %q was accepted", path)
		}
	}
	if _, _, err := Diff(ctx, "", File{Path: "a.txt"}, false); err == nil {
		t.Error("empty root was accepted")
	}
}

func writeFile(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
