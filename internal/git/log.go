package git

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// GraphLanes is how many lanes a commit graph may draw.
const GraphLanes = 8

// commitLimit bounds the history the panel reads and draws.
const commitLimit = 80

// CommitFile is one path a commit changed against its first parent. Status
// is git's one-letter code: A, M, D, R, C or T; a rename or copy keeps its
// previous path in OrigPath.
type CommitFile struct {
	Path     string
	OrigPath string
	Status   byte
}

// CommitFiles reads the paths a commit changed against its first parent, as
// the graph's expanded row lists them. A merge shows what it took over its
// first parent, not the combined diff.
func CommitFiles(ctx context.Context, root, hash string) ([]CommitFile, error) {
	hash = strings.TrimSpace(hash)
	if !isHash(hash) {
		return nil, fmt.Errorf("invalid commit hash %q", hash)
	}
	out, err := run(ctx, root, []string{"GIT_OPTIONAL_LOCKS=0"},
		"show", "--no-color", "--name-status", "-z", "--format=",
		"--find-renames", "--first-parent", hash)
	if err != nil {
		return nil, err
	}
	return parseCommitFiles(out), nil
}

// CommitStat counts the paths and lines a commit changed against its first
// parent, for the graph's details card. As CommitFiles, a merge shows its
// difference from its first parent; binary files count as a path without
// line changes.
func CommitStat(ctx context.Context, root, hash string) (DiffStat, error) {
	hash = strings.TrimSpace(hash)
	if !isHash(hash) {
		return DiffStat{}, fmt.Errorf("invalid commit hash %q", hash)
	}
	out, err := run(ctx, root, []string{"GIT_OPTIONAL_LOCKS=0"},
		"show", "--no-color", "--numstat", "-z", "--format=",
		"--find-renames", "--first-parent", hash)
	if err != nil {
		return DiffStat{}, err
	}
	return parseNumstatStat(out), nil
}

// parseCommitFiles reads NUL-separated name-status records: a status, then
// one path, or a previous and a new one for R and C.
func parseCommitFiles(out []byte) []CommitFile {
	fields := bytes.Split(out, []byte{0})
	var files []CommitFile
	for i := 0; i < len(fields); i++ {
		record := fields[i]
		if len(record) == 0 {
			continue
		}
		status := record[0]
		if status == 'R' || status == 'C' {
			if i+2 >= len(fields) {
				break
			}
			files = append(files, CommitFile{
				Status:   status,
				OrigPath: filepath.FromSlash(string(fields[i+1])),
				Path:     filepath.FromSlash(string(fields[i+2])),
			})
			i += 2
			continue
		}
		if i+1 >= len(fields) {
			break
		}
		files = append(files, CommitFile{
			Status: status,
			Path:   filepath.FromSlash(string(fields[i+1])),
		})
		i++
	}
	return files
}

// LogCommit is one commit of the panel's history, newest first.
type LogCommit struct {
	Hash    string
	Message string
	Author  string
	// Time is the committer's time, zero when git does not report it.
	Time    time.Time
	Parents []string
	Refs    []string
	// Head marks the commit HEAD points at, from its decoration.
	Head bool
	// Incoming marks a commit the current branch's upstream has and HEAD
	// does not: a fetch brought it, and a pull would integrate it.
	Incoming bool
	// Lane is the column its line occupies, from 0 to GraphLanes-1.
	Lane int
}

// readCommits reads a bounded, topologically ordered history. Topological
// order matters: a graph draws a parent under all of its children.
func readCommits(ctx context.Context, root string) ([]LogCommit, error) {
	out, err := run(ctx, root, []string{"GIT_OPTIONAL_LOCKS=0"},
		"log", "--topo-order", "--all", "-n", strconv.Itoa(commitLimit),
		"--pretty=format:%H%x1f%P%x1f%an%x1f%s%x1f%D%x1f%cI")
	if err != nil {
		return nil, err
	}
	commits := parseCommits(out)
	assignLanes(commits)
	markIncoming(commits, readIncoming(ctx, root))
	return commits, nil
}

// readIncoming lists the commits the current branch's upstream has and HEAD
// does not: what a fetch downloaded and a pull would integrate. A branch
// without an upstream, or a detached HEAD, has none. The read is best effort:
// a failure leaves the graph unmarked rather than costing it the history.
func readIncoming(ctx context.Context, root string) map[string]bool {
	out, err := run(ctx, root, []string{"GIT_OPTIONAL_LOCKS=0"},
		"rev-list", "--max-count", strconv.Itoa(commitLimit), "HEAD..@{upstream}")
	if err != nil {
		return nil
	}
	incoming := make(map[string]bool)
	for _, line := range strings.Split(string(out), "\n") {
		if hash := strings.TrimSpace(line); isHash(hash) {
			incoming[hash] = true
		}
	}
	return incoming
}

// markIncoming flags the commits of a read history the upstream alone has.
func markIncoming(commits []LogCommit, incoming map[string]bool) {
	if len(incoming) == 0 {
		return
	}
	for i := range commits {
		commits[i].Incoming = incoming[commits[i].Hash]
	}
}

// parseCommits reads the unit-separated log without trusting every line:
// a warning mixed into the output is skipped, not turned into a commit.
func parseCommits(out []byte) []LogCommit {
	var commits []LogCommit
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Split(line, "\x1f")
		if len(fields) < 5 || !isHash(fields[0]) {
			continue
		}
		message := strings.TrimSpace(fields[3])
		if message == "" {
			message = "(no subject)"
		}
		commit := LogCommit{
			Hash:    fields[0],
			Message: message,
			Author:  strings.TrimSpace(fields[2]),
			Parents: strings.Fields(fields[1]),
			Refs:    parseRefs(fields[4]),
			Head:    hasHeadRef(fields[4]),
		}
		if len(fields) > 5 {
			commit.Time, _ = time.Parse(time.RFC3339, strings.TrimSpace(fields[5]))
		}
		commits = append(commits, commit)
	}
	return commits
}

// isHash reports a full object id, SHA-1 or SHA-256.
func isHash(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// hasHeadRef reports HEAD's marker in a decoration list: "HEAD -> main" on
// a branch, or "HEAD" alone when detached. The remote's origin/HEAD is a
// different ref and does not count.
func hasHeadRef(raw string) bool {
	for _, part := range strings.Split(raw, ", ") {
		part = strings.TrimSpace(part)
		if part == "HEAD" || strings.HasPrefix(part, "HEAD -> ") {
			return true
		}
	}
	return false
}

// parseRefs turns git's decoration list into branch and tag names.
func parseRefs(raw string) []string {
	var refs []string
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ", ") {
		part = strings.TrimSpace(part)
		if i := strings.Index(part, " -> "); i >= 0 {
			part = strings.TrimSpace(part[i+4:])
		}
		part = strings.TrimPrefix(part, "tag: ")
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		refs = append(refs, part)
	}
	return refs
}

// assignLanes tracks the parents still to come and gives each commit the lane
// its child's line can reach. Lanes stay under GraphLanes; a busier history
// shares the last lane rather than failing to draw.
func assignLanes(commits []LogCommit) {
	active := make([]string, GraphLanes)
	index := func(hash string) int {
		for i, h := range active {
			if h == hash {
				return i
			}
		}
		return -1
	}
	free := func() int {
		for i, h := range active {
			if h == "" {
				return i
			}
		}
		return GraphLanes - 1
	}
	for i := range commits {
		c := &commits[i]
		lane := index(c.Hash)
		if lane < 0 {
			lane = free()
		}
		c.Lane = lane
		active[lane] = ""
		for pi, parent := range c.Parents {
			if parent == "" || index(parent) >= 0 {
				continue
			}
			if pi == 0 {
				active[lane] = parent
				continue
			}
			slot := free()
			if active[slot] != "" && slot == lane {
				continue
			}
			active[slot] = parent
		}
	}
}
