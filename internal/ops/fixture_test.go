package ops_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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

// recProgress records ops.Progress events in call order.
type recProgress struct {
	mu     sync.Mutex
	events []string
}

func (r *recProgress) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, s)
}
func (r *recProgress) Plan(n int)        { r.add(fmt.Sprintf("plan %d", n)) }
func (r *recProgress) Start(repo string) { r.add("start " + repo) }
func (r *recProgress) Warn(msg string)   { r.add("warn " + msg) }
func (r *recProgress) Finish(res ops.Result) {
	s := "finish " + res.Repo
	if res.Err != nil {
		s += " err"
	}
	r.add(s)
}

// index returns the position of the first event with prefix, or -1.
func (r *recProgress) index(prefix string) int {
	for i, e := range r.events {
		if strings.HasPrefix(e, prefix) {
			return i
		}
	}
	return -1
}

// assertProgress checks that exactly one "plan total" was recorded first,
// and that every id has exactly one start followed later by one finish.
func assertProgress(t *testing.T, r *recProgress, total int, ids ...string) {
	t.Helper()
	if len(r.events) == 0 || r.events[0] != fmt.Sprintf("plan %d", total) {
		t.Fatalf("first event should be %q, got %v", fmt.Sprintf("plan %d", total), r.events)
	}
	plans := 0
	for _, e := range r.events {
		if strings.HasPrefix(e, "plan ") {
			plans++
		}
	}
	if plans != 1 {
		t.Fatalf("plan called %d times: %v", plans, r.events)
	}
	sort.Strings(ids)
	for _, id := range ids {
		starts, finishes := 0, 0
		for _, e := range r.events {
			if e == "start "+id {
				starts++
			}
			if e == "finish "+id || e == "finish "+id+" err" {
				finishes++
			}
		}
		if starts != 1 || finishes != 1 {
			t.Fatalf("%s: %d starts, %d finishes: %v", id, starts, finishes, r.events)
		}
		if r.index("start "+id) > r.index("finish "+id) {
			t.Fatalf("%s finished before it started: %v", id, r.events)
		}
	}
}
