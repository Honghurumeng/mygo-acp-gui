package git

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
)

// emptyTree is git's well-known empty tree hash. It resolves even in a
// repository without commits, so one diff command covers both cases.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// DiffStat is the uncommitted work a repository carries: how many paths
// changed and how many lines those changes add and remove. Untracked files
// count as additions because git's own numstat leaves them out.
type DiffStat struct {
	Files   int
	Added   int
	Removed int
}

// readStat counts the uncommitted work in files: tracked paths against HEAD
// (or the empty tree before the first commit), plus untracked files read from
// disk. It returns the zero value for a clean work tree.
func readStat(ctx context.Context, root string, files []File, hasCommits bool) (DiffStat, error) {
	stat := DiffStat{Files: len(files)}
	if len(files) == 0 {
		return stat, nil
	}

	base := emptyTree
	if hasCommits {
		base = "HEAD"
	}
	out, err := run(ctx, root, []string{"GIT_OPTIONAL_LOCKS=0"}, "diff", "--numstat", "-z", base)
	if err != nil {
		return DiffStat{}, err
	}
	stat.Added, stat.Removed = parseNumstat(out)

	for _, file := range files {
		if file.Index != '?' || file.Conflict() {
			continue
		}
		if err := ctx.Err(); err != nil {
			return DiffStat{}, err
		}
		if lines, ok := untrackedLines(root, file.Path); ok {
			stat.Added += lines
		}
	}
	return stat, nil
}

// parseNumstat sums "added\tremoved\tpath\0" records. A rename writes an empty
// path followed by the old and new names, so those two records are skipped.
// Binary files report "-" and contribute no line counts.
func parseNumstat(out []byte) (added, removed int) {
	records := bytes.Split(out, []byte{0})
	for i := 0; i < len(records); i++ {
		fields := bytes.SplitN(records[i], []byte{'\t'}, 3)
		if len(fields) < 3 {
			continue
		}
		added += numstatCount(fields[0])
		removed += numstatCount(fields[1])
		if len(fields[2]) == 0 {
			i += 2
		}
	}
	return added, removed
}

// numstatCount reads one numstat field; "-" marks a binary file.
func numstatCount(field []byte) int {
	if len(field) == 0 || field[0] == '-' {
		return 0
	}
	n, err := strconv.Atoi(string(field))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// untrackedLines counts the lines a new file adds, the way git would: every
// newline starts a line and a final line without one still counts. Binary or
// oversized files only count as a changed path, like git's binary records.
func untrackedLines(root, path string) (int, bool) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil || len(data) > MaxPreview {
		return 0, false
	}
	head := data
	if len(head) > previewProbe {
		head = head[:previewProbe]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return 0, false
	}
	lines := bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		lines++
	}
	return lines, true
}
