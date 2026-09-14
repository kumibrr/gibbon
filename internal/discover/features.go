package discover

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kumibrr/gibbon/internal/git"
	"github.com/kumibrr/gibbon/internal/workspace"
)

// Features lists feature directories: direct children of the root that are
// directories, not "base", and not dot-prefixed.
func Features(ws *workspace.Workspace) ([]string, error) {
	entries, err := os.ReadDir(ws.Root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() || n == workspace.BaseDirName || strings.HasPrefix(n, ".") {
			continue
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

// FeatureRepo is a repo's presence in a feature, from both points of view.
type FeatureRepo struct {
	ID         string
	Path       string
	DirExists  bool   // <feature>/<id> exists on disk with a .git entry
	Registered bool   // base/<id> has a worktree registered at that path
	Prunable   bool   // registered but git says the directory is gone
	Branch     string // from the registration, if any
	HasBase    bool   // base/<id> exists
}

// FeatureRepos returns the union of directories under the feature that look
// like repos and worktree registrations in base repos that point into the
// feature. base is the list of base repos (pass nil to discover it).
func FeatureRepos(ws *workspace.Workspace, feature string, base []Repo) ([]FeatureRepo, error) {
	if base == nil {
		var err error
		base, err = Repos(ws.BaseDir())
		if err != nil {
			return nil, err
		}
	}
	featDir := ws.FeatureDir(feature)
	byID := map[string]*FeatureRepo{}
	get := func(id string) *FeatureRepo {
		if fr, ok := byID[id]; ok {
			return fr
		}
		fr := &FeatureRepo{ID: id, Path: ws.RepoFeatureDir(feature, id)}
		byID[id] = fr
		return fr
	}

	// Directories on disk.
	if _, err := os.Stat(featDir); err == nil {
		onDisk, err := Repos(featDir)
		if err != nil {
			return nil, err
		}
		for _, r := range onDisk {
			get(r.ID).DirExists = true
		}
	}

	// Registrations from base repos.
	baseIDs := map[string]bool{}
	for _, b := range base {
		baseIDs[b.ID] = true
		wts, err := git.WorktreeList(b.Path)
		if err != nil {
			return nil, err
		}
		want := ws.RepoFeatureDir(feature, b.ID)
		for _, wt := range wts {
			if samePath(wt.Path, want) {
				fr := get(b.ID)
				fr.Registered = true
				fr.Prunable = wt.Prunable
				fr.Branch = wt.Branch
			}
		}
	}
	var out []FeatureRepo
	for _, fr := range byID {
		fr.HasBase = baseIDs[fr.ID]
		out = append(out, *fr)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func samePath(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 == nil && err2 == nil {
		return ra == rb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
