package ops

import (
	"fmt"

	"github.com/kumibrr/gibbon/internal/git"
)

// blockers lists reasons a worktree must not be removed. wtPath may be ""
// when the directory is gone. checkBranch adds the unpushed/unmerged checks
// that apply when the branch itself is about to be deleted.
func blockers(baseDir, wtPath, branch, baseBranch string, checkBranch bool) []string {
	var out []string
	if wtPath != "" {
		if dirty, err := git.IsDirty(wtPath); err != nil {
			out = append(out, "cannot read status: "+err.Error())
		} else if dirty {
			out = append(out, "uncommitted changes")
		}
	}
	if !checkBranch || branch == "" {
		return out
	}
	if !git.BranchExists(baseDir, branch) {
		return out
	}
	if up, _ := git.Upstream(baseDir, branch); up != "" {
		if ahead, _, err := git.AheadBehind(baseDir, branch, up); err == nil && ahead > 0 {
			out = append(out, fmt.Sprintf("%d commit(s) not pushed to %s", ahead, up))
		}
	} else if ahead, _, err := git.AheadBehind(baseDir, branch, baseBranch); err == nil && ahead > 0 {
		out = append(out, fmt.Sprintf("no upstream and %d commit(s) not on %s", ahead, baseBranch))
	}
	merged, err := git.IsMerged(baseDir, branch, baseBranch)
	if err == nil && !merged {
		// also accept merged into the remote base
		if git.RemoteBranchExists(baseDir, baseBranch) {
			merged, _ = git.IsMerged(baseDir, branch, "origin/"+baseBranch)
		}
	}
	if err == nil && !merged {
		out = append(out, fmt.Sprintf("not merged into %s", baseBranch))
	}
	return out
}
