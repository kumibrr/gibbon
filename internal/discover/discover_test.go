package discover_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kumibrr/gibbon/internal/discover"
	"github.com/kumibrr/gibbon/internal/testutil"
	"github.com/kumibrr/gibbon/internal/workspace"
)

func ids(rs []discover.Repo) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.ID)
	}
	return out
}

func TestWalkRoot(t *testing.T) {
	root := t.TempDir()
	testutil.NewRepo(t, filepath.Join(root, "api", "users"), "main")
	testutil.NewRepo(t, filepath.Join(root, "api", "billing"), "main")
	testutil.NewRepo(t, filepath.Join(root, "web"), "main")
	// nested .git inside a repo must not be discovered
	os.MkdirAll(filepath.Join(root, "web", "vendor", "x", ".git"), 0o755)
	// dot dir, loose file, empty group
	os.MkdirAll(filepath.Join(root, ".hidden", "repo", ".git"), 0o755)
	testutil.WriteFile(t, filepath.Join(root, "docker-compose.yml"), "x")
	os.MkdirAll(filepath.Join(root, "docs"), 0o755)
	os.MkdirAll(filepath.Join(root, "skipme", "y", ".git"), 0o755)

	w, err := discover.WalkRoot(root, "skipme")
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(w.Repos); !reflect.DeepEqual(got, []string{"api/billing", "api/users", "web"}) {
		t.Fatalf("repos %v", got)
	}
	if !reflect.DeepEqual(w.Skipped, []string{"docker-compose.yml", "docs/"}) {
		t.Fatalf("skipped %v", w.Skipped)
	}
	if rs, err := discover.Repos(filepath.Join(root, "nonexistent")); err != nil || len(rs) != 0 {
		t.Fatalf("missing root should be empty: %v %v", rs, err)
	}
}

func TestResolve(t *testing.T) {
	repos := []discover.Repo{{ID: "api/users"}, {ID: "api/billing"}, {ID: "web"}, {ID: "legacy/users"}}
	cases := []struct {
		args []string
		want []string
		err  bool
	}{
		{[]string{"web"}, []string{"web"}, false},
		{[]string{"api/users"}, []string{"api/users"}, false},
		{[]string{"billing"}, []string{"api/billing"}, false},
		{[]string{"users"}, nil, true},
		{[]string{"api/"}, []string{"api/billing", "api/users"}, false},
		{[]string{"*/users"}, []string{"api/users", "legacy/users"}, false},
		{[]string{"nope"}, nil, true},
		{[]string{"zzz/"}, nil, true},
		{[]string{"web", "web", "base/web"}, []string{"web"}, false},
	}
	for _, c := range cases {
		got, err := discover.Resolve(repos, c.args)
		if (err != nil) != c.err {
			t.Errorf("%v: err %v", c.args, err)
			continue
		}
		if !c.err && !reflect.DeepEqual(ids(got), c.want) {
			t.Errorf("%v: got %v want %v", c.args, ids(got), c.want)
		}
	}
}

func TestFeaturesAndFeatureRepos(t *testing.T) {
	root := t.TempDir()
	ws := workspace.New(root)
	os.MkdirAll(ws.GibbonDir(), 0o755)
	users, _ := testutil.NewRepo(t, ws.RepoBaseDir("api/users"), "main")
	testutil.NewRepo(t, ws.RepoBaseDir("web"), "main")
	os.MkdirAll(ws.FeatureDir("pay"), 0o755)
	os.MkdirAll(ws.FeatureDir("other"), 0o755)

	feats, _ := discover.Features(ws)
	if !reflect.DeepEqual(feats, []string{"other", "pay"}) {
		t.Fatalf("features %v", feats)
	}

	// registered + on disk
	testutil.Git(t, users, "worktree", "add", "-q", "-b", "pay", ws.RepoFeatureDir("pay", "api/users"), "main")
	// on disk but unregistered (a stray clone)
	testutil.NewRepo(t, ws.RepoFeatureDir("pay", "web"), "main")
	// registered but directory removed
	base3, _ := testutil.NewRepo(t, ws.RepoBaseDir("gone"), "main")
	testutil.Git(t, base3, "worktree", "add", "-q", "-b", "pay", ws.RepoFeatureDir("pay", "gone"), "main")
	os.RemoveAll(ws.RepoFeatureDir("pay", "gone"))

	frs, err := discover.FeatureRepos(ws, "pay", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(frs) != 3 {
		t.Fatalf("got %+v", frs)
	}
	byID := map[string]discover.FeatureRepo{}
	for _, f := range frs {
		byID[f.ID] = f
	}
	u := byID["api/users"]
	if !u.DirExists || !u.Registered || u.Branch != "pay" || !u.HasBase {
		t.Errorf("users %+v", u)
	}
	w := byID["web"]
	if !w.DirExists || w.Registered || !w.HasBase {
		t.Errorf("web %+v", w)
	}
	g := byID["gone"]
	if g.DirExists || !g.Registered || !g.Prunable {
		t.Errorf("gone %+v", g)
	}
}
