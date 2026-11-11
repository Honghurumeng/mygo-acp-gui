package ui

import (
	"os"
	"path/filepath"
	"strings"
)

// gitBranch returns the branch of the workspace, or "" outside a git
// repository. It reads .git/HEAD so the app does not start a process just
// to label the composer.
func (c *Chat) gitBranch() string {
	if c == nil {
		return ""
	}
	workspace := strings.TrimSpace(c.Workspace)
	if workspace != c.gitWorkspace {
		c.gitWorkspace = workspace
		c.gitBranchName = readGitBranch(workspace)
	}
	return c.gitBranchName
}

func readGitBranch(workspace string) string {
	dir := strings.TrimSpace(workspace)
	if dir == "" || !filepath.IsAbs(dir) {
		return ""
	}
	for {
		gitPath := filepath.Join(dir, ".git")
		info, err := os.Stat(gitPath)
		if err == nil {
			if info.IsDir() {
				return branchFromHead(filepath.Join(gitPath, "HEAD"))
			}
			return branchFromGitFile(gitPath)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func branchFromGitFile(gitPath string) string {
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "gitdir:") {
		return ""
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if gitDir == "" {
		return ""
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(filepath.Dir(gitPath), gitDir)
	}
	return branchFromHead(filepath.Join(gitDir, "HEAD"))
}

func branchFromHead(headPath string) string {
	data, err := os.ReadFile(headPath)
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(data))
	if strings.HasPrefix(head, "ref:") {
		ref := strings.TrimSpace(strings.TrimPrefix(head, "ref:"))
		ref = strings.TrimPrefix(ref, "refs/heads/")
		if ref != "" {
			return ref
		}
	}
	if len(head) >= 7 {
		return head[:7]
	}
	return head
}
