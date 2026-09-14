package ops

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kumibrr/gibbon/internal/discover"
	"github.com/kumibrr/gibbon/internal/git"
	"github.com/kumibrr/gibbon/internal/workspace"
)

// InitOptions configures Init.
type InitOptions struct {
	// BaseBranch, when set, is recorded for every repo instead of detecting.
	BaseBranch string
}

// InitReport describes what Init did.
type InitReport struct {
	Moved    []Result `json:"moved"`
	NotMoved []string `json:"not_moved,omitempty"` // repos left in place after a failed move
	Skipped  []string `json:"skipped,omitempty"`   // non-repo entries left where they were
	Warnings []string `json:"warnings,omitempty"`  // dirty / off-base repos
}

// Init converts a plain directory of repos into a gibbon workspace by moving
// every repo under base/ and writing the initial config. It refuses to run if
// .gibbon exists, base/ is non-empty, or a repo is named "base".
func Init(root string, o InitOptions) (InitReport, error) {
	var rep InitReport
	root, err := filepath.Abs(root)
	if err != nil {
		return rep, err
	}
	ws := &workspace.Workspace{Root: root}

	if _, err := os.Stat(ws.GibbonDir()); err == nil {
		return rep, fmt.Errorf("%s already exists; this directory is already a gibbon workspace", ws.GibbonDir())
	}
	if entries, err := os.ReadDir(ws.BaseDir()); err == nil && len(entries) > 0 {
		return rep, fmt.Errorf("%s exists and is not empty; move or remove it first", ws.BaseDir())
	}
	if git.IsRepo(ws.BaseDir()) {
		return rep, fmt.Errorf("a repo named %q sits at the workspace root; rename it first", workspace.BaseDirName)
	}

	w, err := discover.WalkRoot(root, workspace.BaseDirName)
	if err != nil {
		return rep, err
	}
	rep.Skipped = w.Skipped
	if len(w.Repos) == 0 {
		return rep, errors.New("no git repositories found under " + root)
	}
	for _, r := range w.Repos {
		if path.Base(r.ID) == workspace.BaseDirName {
			return rep, fmt.Errorf("repo %q is named %q, which is reserved; rename it first", r.ID, workspace.BaseDirName)
		}
	}

	// Move sequentially; stop at the first failure.
	cfg := workspace.DefaultConfig()
	for i, r := range w.Repos {
		dest := ws.RepoBaseDir(r.ID)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return failInit(rep, w.Repos[i:], fmt.Errorf("%s: %w", r.ID, err))
		}
		if err := os.Rename(r.Path, dest); err != nil {
			return failInit(rep, w.Repos[i:], fmt.Errorf("%s: %w", r.ID, err))
		}
		var warnings []string
		if err := repairMovedWorktrees(r.Path, dest); err != nil {
			warnings = append(warnings, "worktree repair: "+err.Error())
		}
		base := o.BaseBranch
		if base == "" {
			b, err := workspace.DetectBaseBranch(dest)
			if err != nil {
				warnings = append(warnings, err.Error())
			}
			base = b
		}
		if base != "" {
			cfg.Repos[r.ID] = workspace.RepoConfig{BaseBranch: base}
		}
		if dirty, _ := git.IsDirty(dest); dirty {
			rep.Warnings = append(rep.Warnings, r.ID+": has uncommitted changes")
		}
		if cur, _ := git.CurrentBranch(dest); base != "" && cur != base {
			shown := cur
			if shown == "" {
				shown = "detached HEAD"
			}
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s: on %s, base branch is %s", r.ID, shown, base))
		}
		rep.Moved = append(rep.Moved, result(r.ID, "moved", nil, warnings...))
	}

	// Remove group folders that are now empty.
	if err := removeEmptyDirs(root, ws.BaseDir()); err != nil {
		rep.Warnings = append(rep.Warnings, "cleanup: "+err.Error())
	}

	if err := ws.SaveConfig(cfg); err != nil {
		return rep, err
	}
	if err := os.MkdirAll(ws.FeaturesDir(), 0o755); err != nil {
		return rep, err
	}
	sort.Strings(rep.Warnings)
	return rep, nil
}

func failInit(rep InitReport, remaining []discover.Repo, err error) (InitReport, error) {
	for _, r := range remaining {
		rep.NotMoved = append(rep.NotMoved, r.ID)
	}
	return rep, err
}

// removeEmptyDirs removes empty non-dot directories under root, never
// descending into exclude and never removing root itself.
func removeEmptyDirs(root, exclude string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		full := filepath.Join(root, e.Name())
		if full == exclude {
			continue
		}
		if err := pruneEmpty(full); err != nil {
			return err
		}
	}
	return nil
}

// pruneEmpty removes dir if it is empty after recursively pruning its children.
func pruneEmpty(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() && !git.IsRepo(filepath.Join(dir, e.Name())) {
			if err := pruneEmpty(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
	}
	entries, err = os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return os.Remove(dir)
	}
	return nil
}

// RemoveEmptyParents removes empty directories from dir upward until stop.
func RemoveEmptyParents(dir, stop string) {
	for dir != stop && strings.HasPrefix(dir, stop) {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if os.Remove(dir) != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// repairMovedWorktrees fixes worktree links after a repo moved from oldPath to
// newPath. Worktrees that lived inside the repo (legacy .worktrees/) moved
// with it, so their new locations are passed to repair explicitly.
func repairMovedWorktrees(oldPath, newPath string) error {
	wts, err := git.WorktreeList(newPath)
	if err != nil {
		return err
	}
	var moved []string
	for _, wt := range wts {
		if rel, err := filepath.Rel(oldPath, wt.Path); err == nil && !strings.HasPrefix(rel, "..") {
			moved = append(moved, filepath.Join(newPath, rel))
		}
	}
	return git.WorktreeRepair(newPath, moved...)
}
