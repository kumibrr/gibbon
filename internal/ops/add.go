package ops

import (
	"fmt"
	"github.com/kumibrr/gibbon/internal/pathx"
	"os"
	"path/filepath"

	"github.com/kumibrr/gibbon/internal/discover"
	"github.com/kumibrr/gibbon/internal/git"
	"github.com/kumibrr/gibbon/internal/pool"
	"github.com/kumibrr/gibbon/internal/workspace"
)

// AddOptions configures Add.
type AddOptions struct {
	Branch  string // overrides the feature's branch template for this call
	Workers int
}

// Add creates a worktree of each repo inside the feature. Per repo it fetches
// origin, then reuses a local branch, tracks a remote-only branch, or creates
// the branch from the repo's base branch.
func Add(ws *workspace.Workspace, feature string, repos []discover.Repo, o AddOptions) []Result {
	if !ws.FeatureExists(feature) {
		return []Result{result("", "", fmt.Errorf("feature %q does not exist", feature))}
	}
	branch := o.Branch
	if branch == "" {
		var err error
		branch, err = ws.FeatureBranch(feature)
		if err != nil {
			return []Result{result("", "", err)}
		}
	}
	// Base branches are resolved sequentially: detection may write config.
	bases := make([]string, len(repos))
	baseErr := make([]error, len(repos))
	for i, r := range repos {
		bases[i], baseErr[i] = ws.BaseBranch(r.ID)
	}
	type job struct {
		repo discover.Repo
		base string
		err  error
	}
	jobs := make([]job, len(repos))
	for i, r := range repos {
		jobs[i] = job{r, bases[i], baseErr[i]}
	}
	return pool.Map(jobs, o.Workers, func(j job) Result {
		if j.err != nil {
			return result(j.repo.ID, "", j.err)
		}
		return addOne(ws, feature, j.repo, branch, j.base)
	})
}

func addOne(ws *workspace.Workspace, feature string, r discover.Repo, branch, base string) Result {
	dest := ws.RepoFeatureDir(feature, r.ID)
	if _, err := os.Stat(dest); err == nil {
		wts, err := git.WorktreeList(r.Path)
		if err != nil {
			return result(r.ID, "", err)
		}
		for _, wt := range wts {
			if pathx.Same(wt.Path, dest) {
				return result(r.ID, "exists", nil)
			}
		}
		return result(r.ID, "", fmt.Errorf("%s exists but is not a registered worktree of base/%s", dest, r.ID))
	}
	var warnings []string
	if git.HasRemote(r.Path) {
		if err := git.Fetch(r.Path, false); err != nil {
			warnings = append(warnings, "fetch failed: "+err.Error())
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return result(r.ID, "", err)
	}
	switch {
	case git.BranchExists(r.Path, branch):
		if err := git.WorktreeAdd(r.Path, dest, branch, false, ""); err != nil {
			return result(r.ID, "", err, warnings...)
		}
		return result(r.ID, "checked-out-local", nil, warnings...)
	case git.RemoteBranchExists(r.Path, branch):
		if err := git.CreateTrackingBranch(r.Path, branch); err != nil {
			return result(r.ID, "", err, warnings...)
		}
		if err := git.WorktreeAdd(r.Path, dest, branch, false, ""); err != nil {
			return result(r.ID, "", err, warnings...)
		}
		return result(r.ID, "tracked-remote", nil, warnings...)
	default:
		start := base
		if git.RemoteBranchExists(r.Path, base) {
			start = "origin/" + base
		} else if !git.BranchExists(r.Path, base) {
			return result(r.ID, "", fmt.Errorf("base branch %q not found locally or on origin", base), warnings...)
		}
		if err := git.WorktreeAdd(r.Path, dest, branch, true, start); err != nil {
			return result(r.ID, "", err, warnings...)
		}
		return result(r.ID, "created", nil, warnings...)
	}
}
