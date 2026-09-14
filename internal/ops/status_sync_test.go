package ops_test

import (
	"os"
	"strings"
	"testing"

	"github.com/kumibrr/gibbon/internal/ops"
	"github.com/kumibrr/gibbon/internal/testutil"
)

func TestStatus(t *testing.T) {
	f := newFixture(t, "a", "b", "c")
	f.feature("pay")
	f.mustOK(ops.Add(f.ws, "pay", f.repos("a", "b"), ops.AddOptions{}))
	testutil.Commit(t, f.wt("pay", "a"), "a.txt", "x", "work")
	testutil.Git(t, f.wt("pay", "a"), "push", "-q", "-u", "origin", "pay")
	testutil.Commit(t, f.wt("pay", "a"), "a2.txt", "x", "more")
	testutil.WriteFile(t, f.wt("pay", "a")+"/dirty", "x")
	f.pushFromOther("b", "main", "newer.txt")
	testutil.Git(t, f.ws.RepoBaseDir("b"), "fetch", "-q")
	testutil.NewRepo(t, f.wt("pay", "c"), "main") // unregistered stray
	os.RemoveAll(f.wt("pay", "b"))                // missing

	rows, err := ops.Status(f.ws, []string{"pay"}, 4)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]ops.RepoStatus{}
	for _, r := range rows {
		by[r.Repo] = r
	}
	a := by["a"]
	if a.Branch != "pay" || a.AheadBase != 2 || a.BehindBase != 0 || a.Upstream != "origin/pay" || a.AheadUp != 1 || !a.Dirty {
		t.Errorf("a %+v", a)
	}
	b := by["b"]
	if !b.Missing || b.BehindBase != 1 {
		t.Errorf("b %+v", b)
	}
	if c := by["c"]; !c.Unregistered {
		t.Errorf("c %+v", c)
	}
}

func TestSync(t *testing.T) {
	f := newFixture(t, "ff", "dirty", "off", "ahead")
	f.pushFromOther("ff", "main", "n.txt")
	f.pushFromOther("dirty", "main", "n.txt")
	f.pushFromOther("off", "main", "n.txt")
	testutil.WriteFile(t, f.ws.RepoBaseDir("dirty")+"/wip", "x")
	testutil.Git(t, f.ws.RepoBaseDir("off"), "checkout", "-qb", "other")
	testutil.Commit(t, f.ws.RepoBaseDir("ahead"), "local.txt", "x", "local")

	rs, err := ops.Sync(f.ws, ops.SyncOptions{Prune: true, Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	f.mustOK(rs)
	by := map[string]string{}
	for _, r := range rs {
		by[r.Repo] = r.Action
	}
	if by["ff"] != "fast-forwarded 1 commit(s)" || !exists(f.ws.RepoBaseDir("ff")+"/n.txt") {
		t.Errorf("ff: %q", by["ff"])
	}
	if !strings.Contains(by["dirty"], "dirty") || exists(f.ws.RepoBaseDir("dirty")+"/n.txt") {
		t.Errorf("dirty: %q", by["dirty"])
	}
	if !strings.Contains(by["off"], "on other") {
		t.Errorf("off: %q", by["off"])
	}
	if by["ahead"] != "up-to-date" {
		t.Errorf("ahead: %q", by["ahead"])
	}
	rs, _ = ops.Sync(f.ws, ops.SyncOptions{Workers: 4})
	for _, r := range rs {
		if r.Repo == "ff" && r.Action != "up-to-date" {
			t.Errorf("second sync: %q", r.Action)
		}
	}
}
