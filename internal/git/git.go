// Package git runs the user's git executable for the repository panel.
//
// It never uses a shell and never prompts for credentials: commands inherit
// the user's environment with GIT_TERMINAL_PROMPT=0, so fetch/pull/push fail
// visibly instead of blocking the UI on a hidden terminal prompt.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrGitNotFound means no git executable is on PATH.
var ErrGitNotFound = errors.New("git is not installed")

// ErrNotRepository means the directory is not inside a Git work tree.
var ErrNotRepository = errors.New("not a git repository")

// File is one path reported by git status --porcelain.
type File struct {
	Path     string
	OrigPath string
	Index    byte
	Worktree byte
}

// Conflict reports an unresolved merge/rebase conflict.
func (f File) Conflict() bool {
	pair := string([]byte{f.Index, f.Worktree})
	switch pair {
	case "DD", "AU", "UD", "UA", "DU", "AA", "UU":
		return true
	default:
		return false
	}
}

// Staged reports whether the index differs from HEAD. A file that is both
// staged and modified appears in both lists.
func (f File) Staged() bool {
	return f.Index != ' ' && f.Index != '?' && !f.Conflict()
}

// Unstaged reports whether the work tree differs from the index, including
// untracked files and conflicts.
func (f File) Unstaged() bool {
	return f.Worktree != ' ' || f.Index == '?' || f.Conflict()
}

// IndexCode is the one-letter status shown for the staged side.
func (f File) IndexCode() string {
	if f.Conflict() {
		return "!"
	}
	if f.Index == '?' {
		return "??"
	}
	if f.Index == ' ' {
		return ""
	}
	return string(f.Index)
}

// WorktreeCode is the one-letter status shown for the unstaged side.
func (f File) WorktreeCode() string {
	if f.Conflict() {
		return "!"
	}
	if f.Index == '?' {
		return "??"
	}
	if f.Worktree == ' ' {
		return ""
	}
	return string(f.Worktree)
}

// State is everything the panel needs from one repository read.
type State struct {
	Root       string
	Branch     string
	Ahead      int
	Behind     int
	HasCommits bool
	Files      []File
	Commits    []LogCommit
}

// Staged returns the files with staged changes, in status order.
func (s State) Staged() []File {
	var out []File
	for _, file := range s.Files {
		if file.Staged() {
			out = append(out, file)
		}
	}
	return out
}

// Changes returns the files with unstaged or untracked changes.
func (s State) Changes() []File {
	var out []File
	for _, file := range s.Files {
		if file.Unstaged() {
			out = append(out, file)
		}
	}
	return out
}

// Available reports whether git can be found on PATH.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// Load reads the repository containing workspace: root, branch, status and
// a bounded history. A missing repository is ErrNotRepository.
func Load(ctx context.Context, workspace string) (State, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return State{}, ErrGitNotFound
	}
	workspace = strings.TrimSpace(workspace)
	if workspace == "" || !filepath.IsAbs(workspace) {
		return State{}, ErrNotRepository
	}
	root, err := RepoRoot(ctx, workspace)
	if err != nil {
		return State{}, err
	}
	status, err := readStatus(ctx, root)
	if err != nil {
		return State{}, err
	}
	state := State{
		Root:       root,
		Branch:     status.branch,
		Ahead:      status.ahead,
		Behind:     status.behind,
		HasCommits: status.hasCommits,
		Files:      status.files,
	}
	if state.HasCommits {
		commits, err := readCommits(ctx, root)
		if err != nil {
			return State{}, err
		}
		state.Commits = commits
	}
	return state, nil
}

// RepoRoot returns the top-level work tree that contains dir.
func RepoRoot(ctx context.Context, dir string) (string, error) {
	out, err := run(ctx, dir, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "not a git repository") || strings.Contains(lower, "not a git work tree") {
			return "", fmt.Errorf("%w: %s", ErrNotRepository, err.Error())
		}
		return "", err
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", ErrNotRepository
	}
	return filepath.Clean(root), nil
}

type statusResult struct {
	branch     string
	ahead      int
	behind     int
	hasCommits bool
	files      []File
}

func readStatus(ctx context.Context, root string) (statusResult, error) {
	out, err := run(ctx, root, []string{"GIT_OPTIONAL_LOCKS=0"},
		"status", "--porcelain=v1", "-z", "--branch", "--untracked-files=all")
	if err != nil {
		return statusResult{}, err
	}
	return parseStatus(out), nil
}

func parseStatus(out []byte) statusResult {
	result := statusResult{hasCommits: true}
	records := bytes.Split(out, []byte{0})
	for i := 0; i < len(records); i++ {
		record := records[i]
		if len(record) == 0 {
			continue
		}
		if bytes.HasPrefix(record, []byte("## ")) {
			parseBranch(string(record[3:]), &result)
			continue
		}
		if len(record) < 3 || record[2] != ' ' {
			continue
		}
		file := File{Index: record[0], Worktree: record[1], Path: filepath.FromSlash(string(record[3:]))}
		if file.Index == 'R' || file.Index == 'C' || file.Worktree == 'R' || file.Worktree == 'C' {
			if i+1 < len(records) {
				i++
				file.OrigPath = filepath.FromSlash(string(records[i]))
			}
		}
		result.files = append(result.files, file)
	}
	return result
}

func parseBranch(header string, result *statusResult) {
	switch {
	case strings.HasPrefix(header, "No commits yet on "):
		result.branch = strings.TrimSpace(strings.TrimPrefix(header, "No commits yet on "))
		result.hasCommits = false
		return
	case header == "HEAD (no branch)":
		result.branch = "HEAD (detached)"
		return
	}
	if before, after, ok := strings.Cut(header, "..."); ok {
		result.branch = before
		header = after
	} else {
		result.branch = header
	}
	if _, rest, ok := strings.Cut(header, "["); ok {
		if details, _, ok := strings.Cut(rest, "]"); ok {
			for _, part := range strings.Split(details, ",") {
				part = strings.TrimSpace(part)
				switch {
				case strings.HasPrefix(part, "ahead "):
					result.ahead, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(part, "ahead ")))
				case strings.HasPrefix(part, "behind "):
					result.behind, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(part, "behind ")))
				}
			}
		}
	}
}

// Init creates a repository in dir.
func Init(ctx context.Context, dir string) error {
	_, err := run(ctx, dir, nil, "init")
	return err
}

// Stage adds one path. The caller passes paths from status output.
func Stage(ctx context.Context, root, path string) error {
	_, err := run(ctx, root, nil, "add", "--", path)
	return err
}

// Unstage resets one path in the index. It also works before the first commit.
func Unstage(ctx context.Context, root, path string) error {
	_, err := run(ctx, root, nil, "reset", "-q", "--", path)
	return err
}

// StageAll stages every change, including deletions and untracked files.
func StageAll(ctx context.Context, root string) error {
	_, err := run(ctx, root, nil, "add", "-A")
	return err
}

// UnstageAll unstages every path. It also works before the first commit.
func UnstageAll(ctx context.Context, root string) error {
	_, err := run(ctx, root, nil, "reset", "-q")
	return err
}

// DiscardAll throws away every change in the work tree: it unstages the index,
// restores tracked files to HEAD, and deletes untracked files and directories.
// Ignored files are kept. It is destructive and only meant for a confirmed
// "discard everything" action.
func DiscardAll(ctx context.Context, root string) error {
	if _, err := run(ctx, root, nil, "reset", "-q"); err != nil {
		return err
	}
	// Without a commit there is nothing in HEAD to restore: the reset above
	// made every path untracked, so clean removes them.
	if _, err := run(ctx, root, []string{"GIT_OPTIONAL_LOCKS=0"}, "rev-parse", "--verify", "--quiet", "HEAD"); err == nil {
		if _, err := run(ctx, root, nil, "checkout", "--", "."); err != nil {
			return err
		}
	} else if ctx.Err() != nil {
		return ctx.Err()
	}
	_, err := run(ctx, root, nil, "clean", "-fd")
	return err
}

// Commit records the staged changes with message.
func Commit(ctx context.Context, root, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return errors.New("commit message is empty")
	}
	_, err := run(ctx, root, nil, "commit", "-m", message)
	return err
}

// Fetch downloads remote refs and prunes stale remote-tracking branches.
func Fetch(ctx context.Context, root string) error {
	_, err := run(ctx, root, nil, "fetch", "--prune")
	return err
}

// Pull integrates the upstream branch. GIT_MERGE_AUTOEDIT=no keeps git from
// waiting for a merge message editor.
func Pull(ctx context.Context, root string) error {
	_, err := run(ctx, root, []string{"GIT_MERGE_AUTOEDIT=no"}, "pull", "--no-edit")
	return err
}

// Push publishes the current branch to its configured upstream.
func Push(ctx context.Context, root string) error {
	_, err := run(ctx, root, nil, "push")
	return err
}

// UndoLastCommit moves HEAD back one commit and keeps its changes staged.
// The first commit has no parent, so it is removed by clearing the HEAD ref.
func UndoLastCommit(ctx context.Context, root string) error {
	if _, err := run(ctx, root, []string{"GIT_OPTIONAL_LOCKS=0"}, "rev-parse", "--verify", "--quiet", "HEAD^"); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, err = run(ctx, root, nil, "update-ref", "-d", "HEAD")
		return err
	}
	_, err := run(ctx, root, nil, "reset", "--soft", "HEAD~1")
	return err
}

func run(ctx context.Context, dir string, extraEnv []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PAGER=cat",
		"PAGER=cat",
		"LC_ALL=C",
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}
		return nil, errors.New(message)
	}
	return out, nil
}
