package ops

import (
	"github.com/kumibrr/gibbon/internal/discover"
	"github.com/kumibrr/gibbon/internal/git"
	"github.com/kumibrr/gibbon/internal/pool"
	"github.com/kumibrr/gibbon/internal/workspace"
)

// RepoStatus is one row of `gibbon status`.
type RepoStatus struct {
	Feature      string `json:"feature"`
	Repo         string `json:"repo"`
	Branch       string `json:"branch"`
	BaseBranch   string `json:"base_branch"`
	AheadBase    int    `json:"ahead_base"`
	BehindBase   int    `json:"behind_base"`
	Upstream     string `json:"upstream,omitempty"`
	AheadUp      int    `json:"ahead_upstream"`
	BehindUp     int    `json:"behind_upstream"`
	Dirty        bool   `json:"dirty"`
	Unregistered bool   `json:"unregistered"` // directory exists but is not a worktree of base
	Missing      bool   `json:"missing"`      // registered but directory is gone
	NoBase       bool   `json:"no_base"`      // no counterpart under base/
	Error        string `json:"error,omitempty"`
}

// Status reports every repo in the given features.
func Status(ws *workspace.Workspace, features []string, workers int) ([]RepoStatus, error) {
	base, err := discover.Repos(ws.BaseDir())
	if err != nil {
		return nil, err
	}
	type item struct {
		feature string
		fr      discover.FeatureRepo
		base    string
	}
	var items []item
	for _, feat := range features {
		frs, err := discover.FeatureRepos(ws, feat, base)
		if err != nil {
			return nil, err
		}
		for _, fr := range frs {
			it := item{feature: feat, fr: fr}
			if fr.HasBase {
				it.base, _ = ws.BaseBranch(fr.ID)
			}
			items = append(items, it)
		}
	}
	return pool.Map(items, workers, func(it item) RepoStatus {
		fr := it.fr
		s := RepoStatus{Feature: it.feature, Repo: fr.ID, Branch: fr.Branch, BaseBranch: it.base,
			Unregistered: fr.DirExists && !fr.Registered, Missing: fr.Registered && !fr.DirExists, NoBase: !fr.HasBase}
		if fr.DirExists {
			s.Dirty, _ = git.IsDirty(fr.Path)
			if s.Branch == "" {
				s.Branch, _ = git.CurrentBranch(fr.Path)
			}
		}
		if !fr.HasBase || s.Branch == "" {
			return s
		}
		baseDir := ws.RepoBaseDir(fr.ID)
		baseRef := it.base
		if git.RemoteBranchExists(baseDir, it.base) {
			baseRef = "origin/" + it.base
		}
		if a, b, err := git.AheadBehind(baseDir, s.Branch, baseRef); err == nil {
			s.AheadBase, s.BehindBase = a, b
		} else {
			s.Error = err.Error()
		}
		if up, _ := git.Upstream(baseDir, s.Branch); up != "" {
			s.Upstream = up
			if a, b, err := git.AheadBehind(baseDir, s.Branch, up); err == nil {
				s.AheadUp, s.BehindUp = a, b
			}
		}
		return s
	}), nil
}
