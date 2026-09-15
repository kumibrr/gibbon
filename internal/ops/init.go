package ops

import (
	"errors"
	"fmt"
	"github.com/kumibrr/gibbon/internal/pathx"
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
// .gibbon exists, base/ is non-empty, or a repo is named "base". If anything
// fails partway through, it rolls back: repos already moved are put back and
// any directories Init created (base/, .gibbon/) are removed.
func Init(root string, o InitOptions) (rep InitReport, err error) {
	root, err = filepath.Abs(root)
	if err != nil {
		return rep, err
	}
	ws := workspace.New(root)

	if _, err := os.Stat(ws.GibbonDir()); err == nil {
		return rep, fmt.Errorf("%s already exists; this directory is already a gibbon workspace", ws.GibbonDir())
	}
	baseExisted := false
	if entries, err := os.ReadDir(ws.BaseDir()); err == nil {
		baseExisted = true
		if len(entries) > 0 {
			return rep, fmt.Errorf("%s exists and is not empty; move or remove it first", ws.BaseDir())
		}
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

	// From here on, any failure needs to unwind whatever's already moved.
	var moved []movedRepo
	defer func() {
		if err == nil {
			return
		}
		problems := rollbackInit(ws, moved, baseExisted)
		rep.Moved = nil
		rep.Warnings = nil
		ids := make([]string, len(w.Repos))
		for i, r := range w.Repos {
			ids[i] = r.ID
		}
		rep.NotMoved = ids
		if len(problems) > 0 {
			err = fmt.Errorf("%w (rollback incomplete, needs manual attention: %s)", err, strings.Join(problems, "; "))
		}
	}()

	// Move sequentially; stop at the first failure.
	cfg := workspace.DefaultConfig()
	for _, r := range w.Repos {
		dest := ws.RepoBaseDir(r.ID)
		if mkErr := os.MkdirAll(filepath.Dir(dest), 0o755); mkErr != nil {
			err = fmt.Errorf("%s: %w", r.ID, mkErr)
			return rep, err
		}
		if renErr := os.Rename(r.Path, dest); renErr != nil {
			err = fmt.Errorf("%s: %w", r.ID, renErr)
			return rep, err
		}
		moved = append(moved, movedRepo{id: r.ID, orig: r.Path, dest: dest})
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

	if saveErr := ws.SaveConfig(cfg); saveErr != nil {
		err = saveErr
		return rep, err
	}
	if mkErr := os.MkdirAll(ws.FeaturesDir(), 0o755); mkErr != nil {
		err = mkErr
		return rep, err
	}
	sort.Strings(rep.Warnings)
	return rep, nil
}

// movedRepo records a repo's original and post-move location so a failed
// Init can put it back.
type movedRepo struct {
	id, orig, dest string
}

// rollbackInit undoes partially completed moves after an Init failure: it
// restores repos to their original locations, repairs their worktree links
// back, and removes directories Init created (base/, .gibbon/). It returns a
// description of any repo it could not restore, best-effort.
func rollbackInit(ws *workspace.Workspace, moved []movedRepo, baseExisted bool) []string {
	var problems []string
	for i := len(moved) - 1; i >= 0; i-- {
		m := moved[i]
		if err := os.MkdirAll(filepath.Dir(m.orig), 0o755); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", m.id, err))
			continue
		}
		if err := os.Rename(m.dest, m.orig); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", m.id, err))
			continue
		}
		if err := repairMovedWorktrees(m.dest, m.orig); err != nil {
			problems = append(problems, fmt.Sprintf("%s: worktree repair: %v", m.id, err))
		}
	}
	if entries, err := os.ReadDir(ws.BaseDir()); err == nil {
		for _, e := range entries {
			_ = pruneEmpty(filepath.Join(ws.BaseDir(), e.Name()))
		}
		if !baseExisted {
			if entries, err := os.ReadDir(ws.BaseDir()); err == nil && len(entries) == 0 {
				os.Remove(ws.BaseDir())
			}
		}
	}
	os.RemoveAll(ws.GibbonDir())
	return problems
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
		if pathx.Within(oldPath, wt.Path) {
			rel, _ := filepath.Rel(pathx.Canonical(oldPath), wt.Path)
			moved = append(moved, filepath.Join(newPath, rel))
		}
	}
	return git.WorktreeRepair(newPath, moved...)
}
