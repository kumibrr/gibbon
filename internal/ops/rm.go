package ops

import (
	"fmt"
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
	return pool.Map(jobs, o.Workers, func(j job) Result {
		return removeOne(ws, feature, j.repo, j.base, o)
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
		if filepath.Clean(wts[i].Path) == filepath.Clean(dest) {
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
