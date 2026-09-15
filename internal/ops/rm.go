package ops

import (
	"fmt"
	"github.com/kumibrr/gibbon/internal/pathx"
	"os"
	"path/filepath"
	"strings"

	"github.com/kumibrr/gibbon/internal/discover"
	"github.com/kumibrr/gibbon/internal/git"
	"github.com/kumibrr/gibbon/internal/pool"
	"github.com/kumibrr/gibbon/internal/workspace"
)

// RmOptions configures Remove.
type RmOptions struct {
	DeleteBranch bool
	Force        bool
	KeepDir      bool // used by prune: the whole feature dir is removed afterwards
	Workers      int
	Progress     Progress // optional; receives per-repo events
}

// Remove unregisters each repo's worktree from the feature. The branch is
// kept unless DeleteBranch is set. It refuses on uncommitted changes (and,
// with DeleteBranch, on unpushed or unmerged commits) unless Force is set.
func Remove(ws *workspace.Workspace, feature string, repos []discover.Repo, o RmOptions) []Result {
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
	stepOutOf(ws.FeatureDir(feature), repos)
	pg := notify{o.Progress}
	pg.Plan(len(repos))
	return pool.Map(jobs, o.Workers, func(j job) Result {
		pg.Start(j.repo.ID)
		return pg.Finish(removeOne(ws, feature, j.repo, j.base, o))
	})
}

func removeOne(ws *workspace.Workspace, feature string, r discover.Repo, base string, o RmOptions) Result {
	dest := ws.RepoFeatureDir(feature, r.ID)
	wts, err := git.WorktreeList(r.Path)
	if err != nil {
		return result(r.ID, "", err)
	}
	var reg *git.Worktree
	for i := range wts {
		if pathx.Same(wts[i].Path, dest) {
			reg = &wts[i]
		}
	}
	dirExists := false
	if _, err := os.Stat(dest); err == nil {
		dirExists = true
	}
	if reg == nil {
		if dirExists {
			return result(r.ID, "", fmt.Errorf("%s is not a registered worktree of base/%s; remove it by hand", dest, r.ID))
		}
		return result(r.ID, "not-in-feature", nil)
	}
	wtPath := ""
	if dirExists {
		wtPath = dest
	}
	if bl := blockers(r.Path, wtPath, reg.Branch, base, o.DeleteBranch); len(bl) > 0 && !o.Force {
		return result(r.ID, "", fmt.Errorf("refusing: %s (use --force)", strings.Join(bl, "; ")))
	}
	if dirExists {
		if err := git.WorktreeRemove(r.Path, dest, true); err != nil {
			return result(r.ID, "", err)
		}
	} else if err := git.WorktreePrune(r.Path); err != nil {
		return result(r.ID, "", err)
	}
	action := "removed"
	var warnings []string
	if o.DeleteBranch && reg.Branch != "" {
		if err := git.DeleteBranch(r.Path, reg.Branch, true); err != nil {
			warnings = append(warnings, "branch not deleted: "+err.Error())
		} else {
			action = "removed, branch deleted"
		}
	}
	if !o.KeepDir {
		RemoveEmptyParents(filepath.Dir(dest), ws.FeatureDir(feature))
	}
	return result(r.ID, action, nil, warnings...)
}

// stepOutOf moves the process cwd to featureDir when it is inside one of the
// worktrees about to be removed. Windows cannot delete a process's cwd.
func stepOutOf(featureDir string, repos []discover.Repo) {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	for _, r := range repos {
		if pathx.Within(filepath.Join(featureDir, filepath.FromSlash(r.ID)), cwd) {
			_ = os.Chdir(featureDir)
			return
		}
	}
}
