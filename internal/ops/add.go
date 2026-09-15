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
	Branch   string // overrides the feature's branch template for this call
	Orphan   string // path of an existing worktree of the repo to move into the feature
	Workers  int
	Progress Progress // optional; receives per-repo events
}

// Add creates a worktree of each repo inside the feature. Per repo it fetches
// origin, then reuses a local branch, tracks a remote-only branch, or creates
// the branch from the repo's base branch.
//
// With Orphan set, exactly one repo is expected and the worktree at that path
// is moved into the feature instead of creating a new one.
func Add(ws *workspace.Workspace, feature string, repos []discover.Repo, o AddOptions) []Result {
	if !ws.FeatureExists(feature) {
		return []Result{result("", "", fmt.Errorf("feature %q does not exist", feature))}
	}
	if o.Orphan != "" {
		if len(repos) != 1 {
			return []Result{result("", "", fmt.Errorf("--orphan adopts one worktree, so exactly one repo is expected, got %d", len(repos)))}
		}
		pg := notify{o.Progress}
		pg.Plan(1)
		pg.Start(repos[0].ID)
		return []Result{pg.Finish(adoptOne(ws, feature, repos[0], o.Orphan, o.Branch))}
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
	pg := notify{o.Progress}
	pg.Plan(len(repos))
	return pool.Map(jobs, o.Workers, func(j job) Result {
		pg.Start(j.repo.ID)
		if j.err != nil {
			return pg.Finish(result(j.repo.ID, "", j.err))
		}
		return pg.Finish(addOne(ws, feature, j.repo, branch, j.base))
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

// adoptOne moves an existing worktree of r into the feature. The worktree must
// be registered with base/<repo>; when branch is set it must match what the
// worktree has checked out. Uncommitted changes travel with the directory.
func adoptOne(ws *workspace.Workspace, feature string, r discover.Repo, orphan, branch string) Result {
	dest := ws.RepoFeatureDir(feature, r.ID)
	if _, err := os.Stat(dest); err == nil {
		return result(r.ID, "", fmt.Errorf("%s already exists", dest))
	}
	src := pathx.Canonical(orphan)
	wts, err := git.WorktreeList(r.Path)
	if err != nil {
		return result(r.ID, "", err)
	}
	var wt *git.Worktree
	for i := range wts {
		if pathx.Same(wts[i].Path, src) {
			wt = &wts[i]
			break
		}
	}
	if wt == nil {
		return result(r.ID, "", fmt.Errorf("%s is not a registered worktree of base/%s", orphan, r.ID))
	}
	if wt.Prunable {
		return result(r.ID, "", fmt.Errorf("%s is registered but its directory is gone; run gibbon doctor --fix", orphan))
	}
	if branch != "" && wt.Branch != branch {
		return result(r.ID, "", fmt.Errorf("%s has %q checked out, not %q", orphan, wt.Branch, branch))
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return result(r.ID, "", err)
	}
	if err := git.WorktreeMove(r.Path, wt.Path, dest); err != nil {
		return result(r.ID, "", err)
	}
	return result(r.ID, "adopted", nil)
}
