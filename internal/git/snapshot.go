package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// WorktreeSnapshot captures the workspace's tracked and untracked files before
// a task starts. A failed task can then be rolled back without throwing away
// changes the user already had in the work tree.
//
// Tree is a Git tree object written with a temporary index. IndexTree is the
// repository's original index tree, so a restore also puts staged changes back
// where they were. IndexTree is empty when the index was missing or had
// unmerged entries; Restore then resets the index to empty.
type WorktreeSnapshot struct {
	Root      string
	Tree      string
	IndexTree string
}

// SnapshotWorktree records every non-ignored file in the repository that
// contains workspace. It does not modify the real index or work tree: the
// snapshot index lives in a temporary file and only tree objects are written
// to the repository.
func SnapshotWorktree(ctx context.Context, workspace string) (WorktreeSnapshot, error) {
	if !Available() {
		return WorktreeSnapshot{}, ErrGitNotFound
	}
	workspace = strings.TrimSpace(workspace)
	if workspace == "" || !filepath.IsAbs(workspace) {
		return WorktreeSnapshot{}, ErrNotRepository
	}
	root, err := RepoRoot(ctx, workspace)
	if err != nil {
		return WorktreeSnapshot{}, err
	}
	// An unmerged index cannot be represented by a single tree. Refusing the
	// snapshot also prevents a failure from silently dropping conflict state.
	unmerged, err := run(ctx, root, nil, "ls-files", "-u", "-z")
	if err != nil {
		return WorktreeSnapshot{}, err
	}
	if len(unmerged) > 0 {
		return WorktreeSnapshot{}, errors.New("git: workspace has unresolved conflicts")
	}

	dir, err := os.MkdirTemp("", "acp-gui-snapshot-*")
	if err != nil {
		return WorktreeSnapshot{}, err
	}
	defer os.RemoveAll(dir)
	indexPath := filepath.Join(dir, "index")
	indexEnv := []string{"GIT_INDEX_FILE=" + indexPath}

	// Build a tree from the work tree itself (not HEAD or the real index), so
	// pre-existing staged, unstaged and untracked changes are all captured.
	if _, err := run(ctx, root, indexEnv, "read-tree", "--empty"); err != nil {
		return WorktreeSnapshot{}, err
	}
	if _, err := run(ctx, root, indexEnv, "add", "-A"); err != nil {
		return WorktreeSnapshot{}, err
	}
	treeOut, err := run(ctx, root, indexEnv, "write-tree")
	if err != nil {
		return WorktreeSnapshot{}, err
	}

	snapshot := WorktreeSnapshot{
		Root: root,
		Tree: strings.TrimSpace(string(treeOut)),
	}
	if snapshot.Tree == "" {
		return WorktreeSnapshot{}, errors.New("git: snapshot produced an empty tree id")
	}
	indexOut, indexErr := run(ctx, root, nil, "write-tree")
	if indexErr == nil {
		snapshot.IndexTree = strings.TrimSpace(string(indexOut))
	} else if ctx.Err() != nil {
		return WorktreeSnapshot{}, ctx.Err()
	}
	return snapshot, nil
}

// Restore puts the work tree and index back into the snapshot state. It is the
// failure counterpart to the normal task commit: files created by the task are
// removed, files it changed are restored, and the user's own staged and
// unstaged changes come back. Ignored files are left alone, matching Git's
// ordinary clean behavior.
func (s WorktreeSnapshot) Restore(ctx context.Context) error {
	if strings.TrimSpace(s.Root) == "" || strings.TrimSpace(s.Tree) == "" {
		return errors.New("git: snapshot has no tree to restore")
	}
	// Make the snapshot tree the index, write every file from it, then remove
	// whatever is left untracked (the task's new files). Pre-existing
	// untracked files are part of the snapshot tree, so clean keeps them.
	if _, err := run(ctx, s.Root, nil, "read-tree", "--reset", s.Tree); err != nil {
		return err
	}
	if _, err := run(ctx, s.Root, nil, "checkout-index", "-a", "-f"); err != nil {
		return err
	}
	if _, err := run(ctx, s.Root, nil, "clean", "-fd"); err != nil {
		return err
	}
	if strings.TrimSpace(s.IndexTree) != "" {
		_, err := run(ctx, s.Root, nil, "read-tree", "--reset", s.IndexTree)
		return err
	}
	_, err := run(ctx, s.Root, nil, "read-tree", "--empty")
	return err
}
