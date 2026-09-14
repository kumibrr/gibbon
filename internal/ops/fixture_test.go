package ops_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kumibrr/gibbon/internal/discover"
	"github.com/kumibrr/gibbon/internal/ops"
	"github.com/kumibrr/gibbon/internal/testutil"
	"github.com/kumibrr/gibbon/internal/workspace"
)

// fixture is an initialised workspace with base repos on "main".
type fixture struct {
	t       *testing.T
	ws      *workspace.Workspace
	origins map[string]string
}

func newFixture(t *testing.T, ids ...string) *fixture {
	root := t.TempDir()
	f := &fixture{t: t, ws: workspace.New(root), origins: map[string]string{}}
	for _, id := range ids {
		_, origin := testutil.NewRepo(t, filepath.Join(root, filepath.FromSlash(id)), "main")
		f.origins[id] = origin
	}
	if _, err := ops.Init(root, ops.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) repo(id string) discover.Repo {
	return discover.Repo{ID: id, Path: f.ws.RepoBaseDir(id)}
}

func (f *fixture) repos(ids ...string) []discover.Repo {
	var out []discover.Repo
	for _, id := range ids {
		out = append(out, f.repo(id))
	}
	return out
}

func (f *fixture) feature(name string) {
	if _, err := ops.CreateFeature(f.ws, name, ops.CreateFeatureOptions{}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) wt(feature, id string) string { return f.ws.RepoFeatureDir(feature, id) }

func (f *fixture) mustOK(rs []ops.Result) {
	f.t.Helper()
	for _, r := range rs {
		if r.Err != nil {
			f.t.Fatalf("%s: %v", r.Repo, r.Err)
		}
	}
}

// pushFromOther clones origin elsewhere, commits, and pushes branch.
func (f *fixture) pushFromOther(id, branch, file string) {
	other := testutil.Clone(f.t, f.origins[id], filepath.Join(f.t.TempDir(), "o"))
	if branch != "main" {
		testutil.Git(f.t, other, "checkout", "-qb", branch)
	}
	testutil.Commit(f.t, other, file, "x", "x")
	testutil.Git(f.t, other, "push", "-q", "origin", branch)
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }
