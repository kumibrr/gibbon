// Package git is a thin typed wrapper around the git binary.
package git

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/kumibrr/gibbon/internal/pathx"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Error carries git's stderr for a failed command.
type Error struct {
	Args   []string
	Dir    string
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), msg)
}

func (e *Error) Unwrap() error { return e.Err }

// Run executes git with args in dir and returns trimmed stdout.
func Run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", &Error{Args: args, Dir: dir, Stderr: stderr.String(), Err: err}
	}
	return strings.TrimSpace(stdout.String()), nil
}

// IsRepo reports whether dir contains a .git entry (directory or worktree file).
func IsRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// CurrentBranch returns the checked-out branch, or "" when detached.
func CurrentBranch(dir string) (string, error) {
	out, err := Run(dir, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		var ge *Error
		if errors.As(err, &ge) {
			var ee *exec.ExitError
			if errors.As(ge.Err, &ee) && ee.ExitCode() == 1 {
				return "", nil
			}
		}
		return "", err
	}
	return out, nil
}

// IsDirty reports uncommitted tracked changes or untracked non-ignored files.
func IsDirty(dir string) (bool, error) {
	out, err := Run(dir, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return false, err
	}
	return out != "", nil
}

// Fetch fetches origin, optionally pruning stale remote-tracking refs.
func Fetch(dir string, prune bool) error {
	args := []string{"fetch", "-q", "origin"}
	if prune {
		args = append(args, "--prune")
	}
	_, err := Run(dir, args...)
	return err
}

// HasRemote reports whether a remote named origin exists.
func HasRemote(dir string) bool {
	_, err := Run(dir, "remote", "get-url", "origin")
	return err == nil
}

func refExists(dir, ref string) bool {
	_, err := Run(dir, "show-ref", "--verify", "--quiet", ref)
	return err == nil
}

// BranchExists reports whether a local branch exists.
func BranchExists(dir, name string) bool { return refExists(dir, "refs/heads/"+name) }

// RemoteBranchExists reports whether origin/<name> exists.
func RemoteBranchExists(dir, name string) bool {
	return refExists(dir, "refs/remotes/origin/"+name)
}

// OriginHead returns the branch origin/HEAD points at, or "" if unset.
func OriginHead(dir string) (string, error) {
	out, err := Run(dir, "symbolic-ref", "--short", "-q", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", nil
	}
	return strings.TrimPrefix(out, "origin/"), nil
}

// Upstream returns the upstream ref of branch (e.g. "origin/main"), or "" if none.
func Upstream(dir, branch string) (string, error) {
	out, err := Run(dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", branch+"@{upstream}")
	if err != nil {
		return "", nil
	}
	return out, nil
}

// AheadBehind returns how many commits ref is ahead of and behind base.
func AheadBehind(dir, ref, base string) (ahead, behind int, err error) {
	out, err := Run(dir, "rev-list", "--left-right", "--count", ref+"..."+base)
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list output %q", out)
	}
	ahead, _ = strconv.Atoi(fields[0])
	behind, _ = strconv.Atoi(fields[1])
	return ahead, behind, nil
}

// IsMerged reports whether every commit on branch is reachable from into.
func IsMerged(dir, branch, into string) (bool, error) {
	_, err := Run(dir, "merge-base", "--is-ancestor", branch, into)
	if err == nil {
		return true, nil
	}
	var ge *Error
	if errors.As(err, &ge) {
		var ee *exec.ExitError
		if errors.As(ge.Err, &ee) && ee.ExitCode() == 1 {
			return false, nil
		}
	}
	return false, err
}

// LocalBranches lists local branch names.
func LocalBranches(dir string) ([]string, error) {
	out, err := Run(dir, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// DeleteBranch deletes a local branch; force deletes unmerged branches.
func DeleteBranch(dir, name string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := Run(dir, "branch", flag, name)
	return err
}

// FastForward merges origin/<branch> into the checked-out branch with --ff-only.
func FastForward(dir, branch string) error {
	_, err := Run(dir, "merge", "-q", "--ff-only", "origin/"+branch)
	return err
}

// Worktree is one entry from `git worktree list --porcelain`, excluding the main worktree.
type Worktree struct {
	Path     string
	Branch   string // short name, "" when detached
	Prunable bool
}

// WorktreeList lists linked worktrees registered in the repo at dir.
func WorktreeList(dir string) ([]Worktree, error) {
	out, err := Run(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var all []Worktree
	var cur *Worktree
	flush := func() {
		if cur != nil {
			all = append(all, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur = &Worktree{Path: pathx.Canonical(strings.TrimPrefix(line, "worktree "))}
		case cur == nil:
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case strings.HasPrefix(line, "prunable"):
			cur.Prunable = true
		}
	}
	flush()
	if len(all) > 0 {
		all = all[1:] // first entry is the main worktree
	}
	return all, nil
}

// WorktreeAdd registers a worktree at path. When create is true the branch is
// created from startPoint; otherwise the existing branch is checked out.
func WorktreeAdd(dir, path, branch string, create bool, startPoint string) error {
	args := []string{"worktree", "add", "-q"}
	if create {
		args = append(args, "--no-track", "-b", branch, path, startPoint)
	} else {
		args = append(args, path, branch)
	}
	_, err := Run(dir, args...)
	return err
}

// WorktreeRemove unregisters and deletes a worktree.
func WorktreeRemove(dir, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	_, err := Run(dir, append(args, path)...)
	return err
}

// WorktreePrune drops registrations whose directories are gone.
func WorktreePrune(dir string) error {
	_, err := Run(dir, "worktree", "prune")
	return err
}

// WorktreeRepair fixes path pointers after the repo or its worktrees moved.
func WorktreeRepair(dir string, paths ...string) error {
	_, err := Run(dir, append([]string{"worktree", "repair"}, paths...)...)
	return err
}

// CreateTrackingBranch creates a local branch tracking origin/<name>.
func CreateTrackingBranch(dir, name string) error {
	_, err := Run(dir, "branch", "-q", "--track", name, "origin/"+name)
	return err
}
