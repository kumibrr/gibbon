package ops

import (
	"fmt"
	"github.com/kumibrr/gibbon/internal/pathx"
	"os"
	"sort"
	"time"

	"github.com/kumibrr/gibbon/internal/discover"
	"github.com/kumibrr/gibbon/internal/git"
	"github.com/kumibrr/gibbon/internal/pool"
	"github.com/kumibrr/gibbon/internal/workspace"
)

// CreateFeatureOptions configures CreateFeature.
type CreateFeatureOptions struct {
	From           string // copy the repo set of this feature
	BranchTemplate string // per-feature override of the branch template
	Workers        int
	Progress       Progress // optional; forwarded to Add
}

// CreateFeature creates the feature directory and metadata. With From set,
// the same repos are added, each branched from base.
func CreateFeature(ws *workspace.Workspace, name string, o CreateFeatureOptions) ([]Result, error) {
	if err := workspace.ValidateFeatureName(name); err != nil {
		return nil, err
	}
	if ws.FeatureExists(name) {
		return nil, fmt.Errorf("feature %q already exists", name)
	}
	var fromRepos []discover.Repo
	if o.From != "" {
		if !ws.FeatureExists(o.From) {
			return nil, fmt.Errorf("feature %q does not exist", o.From)
		}
		frs, err := discover.FeatureRepos(ws, o.From, nil)
		if err != nil {
			return nil, err
		}
		for _, fr := range frs {
			if fr.Registered && fr.HasBase {
				fromRepos = append(fromRepos, discover.Repo{ID: fr.ID, Path: ws.RepoBaseDir(fr.ID)})
			}
		}
	}
	if err := os.MkdirAll(ws.FeatureDir(name), 0o755); err != nil {
		return nil, err
	}
	meta := workspace.FeatureMeta{BranchTemplate: o.BranchTemplate, CreatedAt: time.Now().UTC().Truncate(time.Second)}
	if err := ws.SaveFeature(name, meta); err != nil {
		return nil, err
	}
	if len(fromRepos) == 0 {
		return nil, nil
	}
	return Add(ws, name, fromRepos, AddOptions{Workers: o.Workers, Progress: o.Progress}), nil
}

// FeatureSummary is one row of the feature list.
type FeatureSummary struct {
	Name      string `json:"name"`
	Repos     int    `json:"repos"`
	Dirty     int    `json:"dirty"`
	Branch    string `json:"branch"`
	HasMeta   bool   `json:"has_meta"`
	CreatedAt string `json:"created_at,omitempty"`
}

// ListFeatures summarises every feature directory.
func ListFeatures(ws *workspace.Workspace, workers int) ([]FeatureSummary, error) {
	names, err := discover.Features(ws)
	if err != nil {
		return nil, err
	}
	base, err := discover.Repos(ws.BaseDir())
	if err != nil {
		return nil, err
	}
	out := pool.Map(names, workers, func(name string) FeatureSummary {
		s := FeatureSummary{Name: name, HasMeta: ws.HasFeatureMeta(name)}
		s.Branch, _ = ws.FeatureBranch(name)
		if m, err := ws.LoadFeature(name); err == nil && !m.CreatedAt.IsZero() {
			s.CreatedAt = m.CreatedAt.Format(time.RFC3339)
		}
		frs, err := discover.FeatureRepos(ws, name, base)
		if err != nil {
			return s
		}
		for _, fr := range frs {
			if fr.Registered || fr.DirExists {
				s.Repos++
			}
			if fr.DirExists {
				if d, _ := git.IsDirty(fr.Path); d {
					s.Dirty++
				}
			}
		}
		return s
	})
	return out, nil
}

// SwitchPath returns the directory for `feat switch NAME`. "base" is allowed.
func SwitchPath(ws *workspace.Workspace, name string) (string, error) {
	if name == workspace.BaseDirName {
		return ws.BaseDir(), nil
	}
	if !ws.FeatureExists(name) {
		return "", fmt.Errorf("feature %q does not exist", name)
	}
	return ws.FeatureDir(name), nil
}

// PruneCheck is the pre-flight result for one repo in a feature.
type PruneCheck struct {
	Repo     string   `json:"repo"`
	Branch   string   `json:"branch"`
	Blockers []string `json:"blockers,omitempty"`
}

// PrunePlan is what prune would do and what stands in its way.
type PrunePlan struct {
	Feature string       `json:"feature"`
	Checks  []PruneCheck `json:"checks"`
	Blocked bool         `json:"blocked"`
}

// PlanPrune inspects every repo in the feature and reports blockers.
func PlanPrune(ws *workspace.Workspace, feature string, workers int) (PrunePlan, error) {
	plan := PrunePlan{Feature: feature}
	if feature == workspace.BaseDirName {
		return plan, fmt.Errorf("cannot prune %q", feature)
	}
	if !ws.FeatureExists(feature) {
		return plan, fmt.Errorf("feature %q does not exist", feature)
	}
	frs, err := discover.FeatureRepos(ws, feature, nil)
	if err != nil {
		return plan, err
	}
	bases := map[string]string{}
	for _, fr := range frs {
		if fr.HasBase {
			bases[fr.ID], _ = ws.BaseBranch(fr.ID)
		}
	}
	plan.Checks = pool.Map(frs, workers, func(fr discover.FeatureRepo) PruneCheck {
		c := PruneCheck{Repo: fr.ID, Branch: fr.Branch}
		switch {
		case !fr.HasBase:
			c.Blockers = append(c.Blockers, "no matching repo under base/")
		case fr.DirExists && !fr.Registered:
			c.Blockers = append(c.Blockers, "directory is not a registered worktree")
		default:
			wt := ""
			if fr.DirExists {
				wt = fr.Path
			}
			c.Blockers = blockers(ws.RepoBaseDir(fr.ID), wt, fr.Branch, bases[fr.ID], true)
		}
		return c
	})
	for _, c := range plan.Checks {
		if len(c.Blockers) > 0 {
			plan.Blocked = true
		}
	}
	sort.Slice(plan.Checks, func(i, j int) bool { return plan.Checks[i].Repo < plan.Checks[j].Repo })
	return plan, nil
}

// ExecutePrune removes every worktree and branch in the plan, then the
// feature directory and metadata. It refuses a blocked plan unless force.
func ExecutePrune(ws *workspace.Workspace, plan PrunePlan, force bool, workers int, p Progress) ([]Result, error) {
	if plan.Blocked && !force {
		return nil, fmt.Errorf("feature %q has blockers; re-run with --force to prune anyway", plan.Feature)
	}
	var repos []discover.Repo
	for _, c := range plan.Checks {
		repos = append(repos, discover.Repo{ID: c.Repo, Path: ws.RepoBaseDir(c.Repo)})
	}
	results := Remove(ws, plan.Feature, repos, RmOptions{DeleteBranch: true, Force: true, KeepDir: true, Workers: workers, Progress: p})
	if AnyFailed(results) && !force {
		return results, fmt.Errorf("some repos could not be removed; feature directory kept")
	}
	// Windows refuses to delete a directory that is the process's cwd, and
	// running prune from inside the feature is the normal flow. Step out first.
	if cwd, err := os.Getwd(); err == nil && pathx.Within(ws.FeatureDir(plan.Feature), cwd) {
		if err := os.Chdir(ws.Root); err != nil {
			return results, err
		}
	}
	if err := os.RemoveAll(ws.FeatureDir(plan.Feature)); err != nil {
		return results, err
	}
	if err := ws.DeleteFeature(plan.Feature); err != nil {
		return results, err
	}
	return results, nil
}
