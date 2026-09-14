package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/kumibrr/gibbon/internal/discover"
	"github.com/kumibrr/gibbon/internal/git"
	"github.com/kumibrr/gibbon/internal/workspace"
)

// Finding is one doctor observation.
type Finding struct {
	Check   string `json:"check"`
	Repo    string `json:"repo,omitempty"`
	Feature string `json:"feature,omitempty"`
	Message string `json:"message"`
	Suggest string `json:"suggest,omitempty"` // command a human could run
	Fixable bool   `json:"fixable"`
	Fixed   bool   `json:"fixed"`
}

// Doctor audits the workspace. With fix, it applies only non-destructive
// repairs: worktree prune, worktree repair, creating missing feature
// metadata, and dropping config entries for repos that no longer exist.
func Doctor(ws *workspace.Workspace, fix bool) ([]Finding, error) {
	var out []Finding
	base, err := discover.Repos(ws.BaseDir())
	if err != nil {
		return nil, err
	}
	baseIDs := map[string]bool{}
	for _, b := range base {
		baseIDs[b.ID] = true
	}
	features, err := discover.Features(ws)
	if err != nil {
		return nil, err
	}
	featSet := map[string]bool{}
	for _, f := range features {
		featSet[f] = true
	}
	cfg, err := ws.LoadConfig()
	if err != nil {
		return nil, err
	}

	// Per-feature checks.
	templates := map[string]*regexp.Regexp{}
	for _, feat := range features {
		frs, err := discover.FeatureRepos(ws, feat, base)
		if err != nil {
			return nil, err
		}
		for _, fr := range frs {
			switch {
			case !fr.HasBase:
				// 4. feature repo with no base counterpart
				out = append(out, Finding{Check: "no-base", Repo: fr.ID, Feature: feat,
					Message: "present in feature but there is no base/" + fr.ID,
					Suggest: fmt.Sprintf("rm -rf %s", fr.Path)})
			case fr.DirExists && !fr.Registered:
				// 3. directory that is not a registered worktree; maybe repairable
				f := Finding{Check: "unregistered-dir", Repo: fr.ID, Feature: feat,
					Message: "directory exists but is not a registered worktree of base/" + fr.ID,
					Suggest: fmt.Sprintf("rm -rf %s  # or: cd %s && git worktree repair %s", fr.Path, ws.RepoBaseDir(fr.ID), fr.Path)}
				if looksLikeWorktreeOf(fr.Path, ws.RepoBaseDir(fr.ID)) {
					f.Fixable = true
					f.Message = "worktree link to base/" + fr.ID + " is broken"
					if fix {
						if err := git.WorktreeRepair(ws.RepoBaseDir(fr.ID), fr.Path); err == nil {
							f.Fixed = true
						}
					}
				}
				out = append(out, f)
			}
		}
		// 6a. directory without metadata
		if !ws.HasFeatureMeta(feat) {
			f := Finding{Check: "missing-metadata", Feature: feat, Fixable: true,
				Message: "feature directory has no .gibbon/features/" + feat + ".toml"}
			if fix {
				if err := ws.SaveFeature(feat, workspace.FeatureMeta{CreatedAt: time.Now().UTC().Truncate(time.Second)}); err == nil {
					f.Fixed = true
				}
			}
			out = append(out, f)
		}
		if b, err := ws.FeatureBranch(feat); err == nil {
			templates[feat] = regexp.MustCompile("^" + regexp.QuoteMeta(b) + "$")
		}
	}

	// Registered worktree paths per base repo, and live feature worktrees.
	liveBranch := map[string]map[string]bool{} // repo -> branch -> true
	for _, b := range base {
		liveBranch[b.ID] = map[string]bool{}
		// 1. legacy .worktrees
		if fi, err := os.Stat(filepath.Join(b.Path, ".worktrees")); err == nil && fi.IsDir() {
			out = append(out, Finding{Check: "legacy-worktrees", Repo: b.ID,
				Message: "repo contains a legacy .worktrees/ directory not managed by gibbon",
				Suggest: fmt.Sprintf("cd %s && git worktree list", b.Path)})
		}
		wts, err := git.WorktreeList(b.Path)
		if err != nil {
			out = append(out, Finding{Check: "git-error", Repo: b.ID, Message: err.Error()})
			continue
		}
		for _, wt := range wts {
			feat, ok := ws.FeatureContaining(wt.Path)
			if wt.Prunable {
				// 2. registration whose directory is gone
				f := Finding{Check: "stale-registration", Repo: b.ID, Feature: feat,
					Message: "worktree registered at " + wt.Path + " but the directory is gone", Fixable: true,
					Suggest: fmt.Sprintf("cd %s && git worktree prune", b.Path)}
				if fix {
					if err := git.WorktreePrune(b.Path); err == nil {
						f.Fixed = true
					}
				}
				out = append(out, f)
				continue
			}
			if ok && wt.Branch != "" {
				liveBranch[b.ID][wt.Branch] = true
			}
		}
	}

	// 6b. metadata without directory
	metas, _ := ws.ListFeatureMeta()
	for _, m := range metas {
		if !featSet[m] {
			out = append(out, Finding{Check: "orphan-metadata", Feature: m,
				Message: "metadata exists but there is no feature directory",
				Suggest: fmt.Sprintf("rm %s", filepath.Join(ws.FeaturesDir(), m+".toml"))})
		}
	}

	// 5. orphan branches: a branch equal to some feature's branch name (or the
	// workspace template applied to a name that no longer exists) with no live
	// worktree.
	tplRe := templateRegexp(cfg.BranchTemplate)
	for _, b := range base {
		branches, err := git.LocalBranches(b.Path)
		if err != nil {
			continue
		}
		baseBranch := cfg.Repos[b.ID].BaseBranch
		for _, br := range branches {
			if br == baseBranch || liveBranch[b.ID][br] {
				continue
			}
			match := ""
			for feat, re := range templates {
				if re.MatchString(br) {
					match = feat
				}
			}
			if match == "" && tplRe != nil {
				if m := tplRe.FindStringSubmatch(br); m != nil && !featSet[m[1]] {
					match = m[1]
				}
			}
			if match == "" {
				continue
			}
			out = append(out, Finding{Check: "orphan-branch", Repo: b.ID, Feature: match,
				Message: fmt.Sprintf("branch %q matches feature %q but has no worktree", br, match),
				Suggest: fmt.Sprintf("cd %s && git branch -d %s", b.Path, br)})
		}
	}

	// 7. config entries for missing repos
	changed := false
	for id := range cfg.Repos {
		if !baseIDs[id] {
			f := Finding{Check: "stale-config", Repo: id, Fixable: true,
				Message: "config has an entry for a repo that is not under base/"}
			if fix {
				delete(cfg.Repos, id)
				changed = true
				f.Fixed = true
			}
			out = append(out, f)
		}
	}
	if changed {
		if err := ws.SaveConfig(cfg); err != nil {
			return out, err
		}
	}

	// 8. base clones dirty or off base
	for _, b := range base {
		if d, _ := git.IsDirty(b.Path); d {
			out = append(out, Finding{Check: "base-dirty", Repo: b.ID, Message: "base clone has uncommitted changes",
				Suggest: fmt.Sprintf("cd %s && git status", b.Path)})
		}
		want := cfg.Repos[b.ID].BaseBranch
		if cur, _ := git.CurrentBranch(b.Path); want != "" && cur != want {
			shown := cur
			if shown == "" {
				shown = "detached HEAD"
			}
			out = append(out, Finding{Check: "base-off-branch", Repo: b.ID,
				Message: fmt.Sprintf("base clone is on %s, expected %s", shown, want),
				Suggest: fmt.Sprintf("cd %s && git switch %s", b.Path, want)})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Check != out[j].Check {
			return out[i].Check < out[j].Check
		}
		if out[i].Feature != out[j].Feature {
			return out[i].Feature < out[j].Feature
		}
		return out[i].Repo < out[j].Repo
	})
	return out, nil
}

// looksLikeWorktreeOf reports whether dir has a .git file pointing into repo.
func looksLikeWorktreeOf(dir, repo string) bool {
	data, err := os.ReadFile(filepath.Join(dir, ".git"))
	if err != nil {
		return false
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "gitdir: ") {
		return false
	}
	gitdir := strings.TrimPrefix(line, "gitdir: ")
	rel, err := filepath.Rel(filepath.Join(repo, ".git", "worktrees"), gitdir)
	return err == nil && !strings.HasPrefix(rel, "..")
}

// templateRegexp turns "feat/{feature}" into ^feat/(.+)$. It returns nil when
// the template has no placeholder or is the bare "{feature}", since then every
// branch would match and the check would be noise.
func templateRegexp(tpl string) *regexp.Regexp {
	if !strings.Contains(tpl, "{feature}") || strings.TrimSpace(tpl) == "{feature}" {
		return nil
	}
	parts := strings.SplitN(tpl, "{feature}", 2)
	return regexp.MustCompile("^" + regexp.QuoteMeta(parts[0]) + "(.+)" + regexp.QuoteMeta(parts[1]) + "$")
}
