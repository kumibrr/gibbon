package ops

import (
	"fmt"

	"github.com/kumibrr/gibbon/internal/discover"
	"github.com/kumibrr/gibbon/internal/git"
	"github.com/kumibrr/gibbon/internal/pool"
	"github.com/kumibrr/gibbon/internal/workspace"
)

// SyncOptions configures Sync.
type SyncOptions struct {
	Prune   bool
	Workers int
}

// Sync fetches every base clone and fast-forwards its base branch when the
// clone is clean and checked out on it. Feature branches are never touched.
func Sync(ws *workspace.Workspace, o SyncOptions) ([]Result, error) {
	repos, err := discover.Repos(ws.BaseDir())
	if err != nil {
		return nil, err
	}
	bases := make([]string, len(repos))
	for i, r := range repos {
		bases[i], _ = ws.BaseBranch(r.ID)
	}
	type job struct {
		repo discover.Repo
		base string
	}
	jobs := make([]job, len(repos))
	for i, r := range repos {
		jobs[i] = job{r, bases[i]}
	}
	return pool.Map(jobs, o.Workers, func(j job) Result {
		r := j.repo
		if !git.HasRemote(r.Path) {
			return result(r.ID, "skipped: no origin", nil)
		}
		if err := git.Fetch(r.Path, o.Prune); err != nil {
			return result(r.ID, "", err)
		}
		if j.base == "" {
			return result(r.ID, "fetched", nil, "base branch unknown")
		}
		cur, err := git.CurrentBranch(r.Path)
		if err != nil {
			return result(r.ID, "", err)
		}
		if cur != j.base {
			shown := cur
			if shown == "" {
				shown = "detached HEAD"
			}
			return result(r.ID, fmt.Sprintf("fetched, skipped ff: on %s", shown), nil)
		}
		if dirty, _ := git.IsDirty(r.Path); dirty {
			return result(r.ID, "fetched, skipped ff: dirty", nil)
		}
		if !git.RemoteBranchExists(r.Path, j.base) {
			return result(r.ID, "fetched, skipped ff: no origin/"+j.base, nil)
		}
		ahead, behind, err := git.AheadBehind(r.Path, j.base, "origin/"+j.base)
		if err != nil {
			return result(r.ID, "", err)
		}
		if behind == 0 {
			return result(r.ID, "up-to-date", nil)
		}
		if ahead > 0 {
			return result(r.ID, fmt.Sprintf("fetched, skipped ff: %d local commit(s) ahead", ahead), nil)
		}
		if err := git.FastForward(r.Path, j.base); err != nil {
			return result(r.ID, "", err)
		}
		return result(r.ID, fmt.Sprintf("fast-forwarded %d commit(s)", behind), nil)
	}), nil
}
