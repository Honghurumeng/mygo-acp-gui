package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Branch is one local branch as the branch pickers list it.
type Branch struct {
	Name     string
	Upstream string
	Current  bool
}

// Branches lists the repository's local branches, the current one first and
// the rest by name. Remote-tracking refs stay out: switching to one would
// create a second local branch, which is a separate action. An unborn branch
// (a repository before its first commit) is listed even though it has no ref
// yet, so a draft can still show what HEAD points at.
func Branches(ctx context.Context, dir string) ([]Branch, error) {
	out, err := run(ctx, dir, []string{"GIT_OPTIONAL_LOCKS=0"},
		"for-each-ref", "--sort=refname",
		"--format=%(refname:short)%00%(upstream:short)%00%(HEAD)",
		"refs/heads")
	if err != nil {
		return nil, err
	}
	var branches []Branch
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Split(line, "\x00")
		if len(fields) < 3 {
			continue
		}
		name := strings.TrimSpace(fields[0])
		if name == "" {
			continue
		}
		seen[name] = true
		branches = append(branches, Branch{
			Name:     name,
			Upstream: strings.TrimSpace(fields[1]),
			Current:  strings.TrimSpace(fields[2]) == "*",
		})
	}
	if current := currentBranch(ctx, dir); current != "" && !seen[current] {
		branches = append(branches, Branch{Name: current, Current: true})
	}
	sort.SliceStable(branches, func(i, j int) bool {
		if branches[i].Current != branches[j].Current {
			return branches[i].Current
		}
		return branches[i].Name < branches[j].Name
	})
	return branches, nil
}

// CreateBranch makes a branch at base without touching the work tree or
// checking it out. An empty base means HEAD; the graph passes the commit the
// user right-clicked.
func CreateBranch(ctx context.Context, root, name, base string) error {
	name = strings.TrimSpace(name)
	if err := validateBranchName(ctx, root, name); err != nil {
		return err
	}
	base = strings.TrimSpace(base)
	switch {
	case base == "", base == "HEAD":
		base = "HEAD"
	case !isHash(base):
		return fmt.Errorf("invalid branch start point %q", base)
	}
	_, err := run(ctx, root, nil, "branch", name, base)
	return err
}

// SwitchBranch checks out an existing local branch. A conflicting work-tree
// change makes git fail; the error reaches the panel and the branch stays as
// it was.
func SwitchBranch(ctx context.Context, root, name string) error {
	name = strings.TrimSpace(name)
	if err := validateBranchName(ctx, root, name); err != nil {
		return err
	}
	_, err := run(ctx, root, nil, "switch", name)
	return err
}

// SwitchCarryResult reports what SwitchBranchCarryingChanges did with the
// work tree. A false RestoreConflict means the stash popped cleanly and is
// gone; a true one means the switch happened but the changes stay in StashRef
// for the user to reapply or drop.
type SwitchCarryResult struct {
	Stashed         bool
	RestoreConflict bool
	StashRef        string
}

// LocalChangesBlockSwitch reports whether err is git refusing a checkout
// because local changes or untracked files would be overwritten. The app
// uses it to offer the stash-and-switch fallback instead of only repeating
// the error.
func LocalChangesBlockSwitch(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "would be overwritten") ||
		strings.Contains(message, "local changes") ||
		strings.Contains(message, "untracked working tree files")
}

// SwitchBranchCarryingChanges stashes tracked, staged and untracked changes,
// checks out the branch, then reapplies the stash with its index. A clean
// restore leaves no trace; a conflicting one keeps the stash entry and
// reports RestoreConflict instead of throwing the changes away. If the
// checkout itself fails, the stash is restored on the old branch.
func SwitchBranchCarryingChanges(ctx context.Context, root, name string) (SwitchCarryResult, error) {
	name = strings.TrimSpace(name)
	if err := validateBranchName(ctx, root, name); err != nil {
		return SwitchCarryResult{}, err
	}
	status, err := run(ctx, root, []string{"GIT_OPTIONAL_LOCKS=0"}, "status", "--porcelain")
	if err != nil {
		return SwitchCarryResult{}, err
	}
	if len(bytes.TrimSpace(status)) == 0 {
		return SwitchCarryResult{}, SwitchBranch(ctx, root, name)
	}
	if _, err := run(ctx, root, nil, "stash", "push", "--include-untracked",
		"-m", "acp-gui: switch to "+name); err != nil {
		return SwitchCarryResult{}, err
	}
	result := SwitchCarryResult{Stashed: true, StashRef: "stash@{0}"}
	if err := SwitchBranch(ctx, root, name); err != nil {
		// The old branch is still checked out, so the stash belongs there.
		if _, popErr := run(ctx, root, nil, "stash", "pop", "--index"); popErr != nil {
			return result, fmt.Errorf("%w; restoring local changes failed: %v (stash kept as %s)",
				err, popErr, result.StashRef)
		}
		return result, err
	}
	if _, err := run(ctx, root, nil, "stash", "pop", "--index"); err != nil {
		result.RestoreConflict = true
		return result, nil
	}
	result.StashRef = ""
	return result, nil
}

// validateBranchName rejects names git cannot store and names that would be
// read as a command option. git check-ref-format owns the actual rules.
func validateBranchName(ctx context.Context, root, name string) error {
	if name == "" {
		return errors.New("branch name is empty")
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("invalid branch name %q", name)
	}
	if _, err := run(ctx, root, []string{"GIT_OPTIONAL_LOCKS=0"},
		"check-ref-format", "--branch", name); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("invalid branch name %q: %w", name, err)
	}
	return nil
}

// PushBranch publishes the current branch. A branch without an upstream
// (typically one the graph just created) is pushed to origin, or the only
// remote, and gets that remote as its upstream, so a later Pull works too.
func PushBranch(ctx context.Context, root string) error {
	branch := currentBranch(ctx, root)
	if branch == "" {
		return errors.New("cannot push a detached HEAD")
	}
	if _, err := run(ctx, root, []string{"GIT_OPTIONAL_LOCKS=0"},
		"rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
		return Push(ctx, root)
	} else if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	remote, err := pushRemote(ctx, root, branch)
	if err != nil {
		return err
	}
	_, err = run(ctx, root, nil, "push", "--set-upstream", remote, branch)
	return err
}

// PullBranch integrates the current branch's upstream. A local branch
// without one takes a remote-tracking branch of the same name (origin first,
// then a unique remote), so a branch fetched from elsewhere can be pulled
// without a manual --set-upstream first. A branch with no matching remote
// reports why instead of running a pull that git would reject.
func PullBranch(ctx context.Context, root string) error {
	branch := currentBranch(ctx, root)
	if branch == "" {
		return errors.New("cannot pull a detached HEAD")
	}
	if _, err := run(ctx, root, []string{"GIT_OPTIONAL_LOCKS=0"},
		"rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
		return Pull(ctx, root)
	} else if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	upstream, err := matchingRemoteBranch(ctx, root, branch)
	if err != nil {
		return err
	}
	if _, err := run(ctx, root, nil, "branch", "--set-upstream-to="+upstream, branch); err != nil {
		return err
	}
	return Pull(ctx, root)
}

// matchingRemoteBranch finds <remote>/<branch> among the remote-tracking
// refs, preferring origin when several remotes have the name.
func matchingRemoteBranch(ctx context.Context, root, branch string) (string, error) {
	out, err := run(ctx, root, []string{"GIT_OPTIONAL_LOCKS=0"}, "remote")
	if err != nil {
		return "", err
	}
	var remotes []string
	for _, remote := range bytes.Fields(out) {
		remotes = append(remotes, string(remote))
	}
	if len(remotes) == 0 {
		return "", errors.New("no remote is configured")
	}
	var matches []string
	for _, remote := range remotes {
		ref := "refs/remotes/" + remote + "/" + branch
		if _, err := run(ctx, root, []string{"GIT_OPTIONAL_LOCKS=0"},
			"rev-parse", "--verify", "--quiet", ref); err == nil {
			matches = append(matches, remote+"/"+branch)
		} else if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
	}
	for _, match := range matches {
		if strings.HasPrefix(match, "origin/") {
			return match, nil
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no upstream is set for %s and no remote branch has that name", branch)
	default:
		return "", fmt.Errorf("no upstream is set for %s; several remote branches have that name", branch)
	}
}

// currentBranch reads the checked-out branch, "" on a detached HEAD.
func currentBranch(ctx context.Context, dir string) string {
	out, err := run(ctx, dir, []string{"GIT_OPTIONAL_LOCKS=0"},
		"symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// pushRemote chooses where a branch without an upstream goes: origin when it
// exists, the only remote otherwise. More than one without origin needs an
// explicit upstream instead of a silent guess.
func pushRemote(ctx context.Context, root, branch string) (string, error) {
	out, err := run(ctx, root, []string{"GIT_OPTIONAL_LOCKS=0"}, "remote")
	if err != nil {
		return "", err
	}
	var remotes []string
	for _, remote := range bytes.Fields(out) {
		remotes = append(remotes, string(remote))
	}
	for _, remote := range remotes {
		if remote == "origin" {
			return remote, nil
		}
	}
	switch len(remotes) {
	case 0:
		return "", errors.New("no remote is configured")
	case 1:
		return remotes[0], nil
	default:
		return "", fmt.Errorf("no upstream is set for %s; push it once with an explicit remote", branch)
	}
}
