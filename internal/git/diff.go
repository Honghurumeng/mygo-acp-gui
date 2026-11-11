package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Preview limits keep the diff viewer responsive.
const (
	// MaxPreview is the largest text side a preview carries.
	MaxPreview = 512 << 10
	// previewProbe is how much of a side is checked for NUL bytes.
	previewProbe = 8000
)

// ErrBinaryFile means a side of the preview is not text.
var ErrBinaryFile = errors.New("binary file cannot be previewed")

// ErrFileTooLarge means a side of the preview is over MaxPreview.
var ErrFileTooLarge = errors.New("file is too large to preview")

// Diff returns the old and new text of file for the staged or unstaged side,
// so a diff viewer can compare them. An untracked or deleted file has an empty
// side, and a renamed file compares against its original path. Binary or huge
// sides are errors rather than text.
func Diff(ctx context.Context, root string, file File, staged bool) (old, new string, err error) {
	if strings.TrimSpace(root) == "" || file.Path == "" {
		return "", "", errors.New("diff needs a repository root and a path")
	}
	if file.Conflict() {
		return "", "", errors.New("resolve the conflict before previewing the diff")
	}
	path, err := repoPath(file.Path)
	if err != nil {
		return "", "", err
	}
	oldPath := path
	if file.OrigPath != "" {
		orig, err := repoPath(file.OrigPath)
		if err != nil {
			return "", "", err
		}
		oldPath = orig
	}
	if staged {
		if file.Index == 'D' {
			new = ""
		} else if new, err = object(ctx, root, ":"+path); err != nil {
			return "", "", err
		}
		if file.Index != 'A' && file.Index != '?' {
			if old, err = object(ctx, root, "HEAD:"+oldPath); err != nil {
				return "", "", err
			}
		}
		return old, new, nil
	}
	if file.Index != '?' {
		if old, err = object(ctx, root, ":"+oldPath); err != nil {
			return "", "", err
		}
	}
	if file.Worktree == 'D' {
		return old, "", nil
	}
	new, err = worktreeText(root, path)
	if err != nil {
		return "", "", err
	}
	return old, new, nil
}

// CommitDiff returns the old and new text of one file in a commit, against
// its first parent, for the preview the expanded commit list opens. An added
// file has an empty old side, a deleted one an empty new side, and a rename
// compares the previous path with the new one. Binary or huge sides are
// errors, as in Diff.
func CommitDiff(ctx context.Context, root, hash string, file CommitFile) (old, new string, err error) {
	if strings.TrimSpace(root) == "" || file.Path == "" {
		return "", "", errors.New("commit diff needs a repository root and a path")
	}
	if !isHash(hash) {
		return "", "", fmt.Errorf("invalid commit hash %q", hash)
	}
	path, err := repoPath(file.Path)
	if err != nil {
		return "", "", err
	}
	oldPath := path
	if file.OrigPath != "" {
		orig, err := repoPath(file.OrigPath)
		if err != nil {
			return "", "", err
		}
		oldPath = orig
	}
	if file.Status != 'A' {
		if old, err = object(ctx, root, hash+"^:"+oldPath); err != nil {
			return "", "", err
		}
	}
	if file.Status != 'D' {
		if new, err = object(ctx, root, hash+":"+path); err != nil {
			return "", "", err
		}
	}
	return old, new, nil
}

// repoPath keeps a status path inside the repository.
func repoPath(path string) (string, error) {
	if path == "" || filepath.IsAbs(path) || strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("path %q is outside the repository", path)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path %q is outside the repository", path)
	}
	return clean, nil
}

// object reads a blob spec such as HEAD:path or :path.
func object(ctx context.Context, root, spec string) (string, error) {
	out, err := run(ctx, root, []string{"GIT_OPTIONAL_LOCKS=0"}, "show", spec)
	if err != nil {
		return "", err
	}
	return previewText(out)
}

// worktreeText reads a file as it is on disk, empty when it is gone.
func worktreeText(root, path string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return previewText(data)
}

// previewText rejects binary and oversized sides.
func previewText(data []byte) (string, error) {
	if len(data) > MaxPreview {
		return "", ErrFileTooLarge
	}
	head := data
	if len(head) > previewProbe {
		head = head[:previewProbe]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return "", ErrBinaryFile
	}
	return string(data), nil
}
