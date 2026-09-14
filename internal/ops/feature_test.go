package ops_test

import (
	"strings"
	"testing"

	"github.com/kumibrr/gibbon/internal/git"
	"github.com/kumibrr/gibbon/internal/ops"
	"github.com/kumibrr/gibbon/internal/testutil"
)

func TestCreateFeature(t *testing.T) {
	f := newFixture(t, "api/users", "web")
	if _, err := ops.CreateFeature(f.ws, "bad/name", ops.CreateFeatureOptions{}); err == nil {
		t.Fatal("invalid name accepted")
	}
	if _, err := ops.CreateFeature(f.ws, "pay", ops.CreateFeatureOptions{BranchTemplate: "feat/{feature}"}); err != nil {
		t.Fatal(err)
	}
	if !f.ws.FeatureExists("pay") || !f.ws.HasFeatureMeta("pay") {
		t.Fatal("feature not created")
	}
	if b, _ := f.ws.FeatureBranch("pay"); b != "feat/pay" {
		t.Fatalf("branch %q", b)
	}
	if _, err := ops.CreateFeature(f.ws, "pay", ops.CreateFeatureOptions{}); err == nil {
		t.Fatal("duplicate accepted")
	}
	p, err := ops.SwitchPath(f.ws, "pay")
	if err != nil || p != f.ws.FeatureDir("pay") {
		t.Fatal(p, err)
	}
	if p, _ := ops.SwitchPath(f.ws, "base"); p != f.ws.BaseDir() {
		t.Fatal(p)
	}
	if _, err := ops.SwitchPath(f.ws, "nope"); err == nil {
		t.Fatal("missing feature accepted")
	}
}

func TestCreateFeatureFrom(t *testing.T) {
	f := newFixture(t, "api/users", "api/billing", "web")
	f.feature("old")
	f.mustOK(ops.Add(f.ws, "old", f.repos("api/users", "web"), ops.AddOptions{}))
	testutil.Commit(t, f.wt("old", "web"), "wip.txt", "x", "wip")

	rs, err := ops.CreateFeature(f.ws, "new", ops.CreateFeatureOptions{From: "old"})
	if err != nil {
		t.Fatal(err)
	}
	f.mustOK(rs)
	if len(rs) != 2 || !exists(f.wt("new", "api/users")) || !exists(f.wt("new", "web")) || exists(f.wt("new", "api/billing")) {
		t.Fatalf("repo set not copied: %+v", rs)
	}
	// branched from base, not from old's tip
	if ahead, _, _ := git.AheadBehind(f.ws.RepoBaseDir("web"), "new", "main"); ahead != 0 {
		t.Fatalf("new should start at base, ahead=%d", ahead)
	}
	if _, err := ops.CreateFeature(f.ws, "x", ops.CreateFeatureOptions{From: "ghost"}); err == nil {
		t.Fatal("missing --from accepted")
	}
}

func TestListFeatures(t *testing.T) {
	f := newFixture(t, "api/users", "web")
	f.feature("pay")
	f.feature("empty")
	f.mustOK(ops.Add(f.ws, "pay", f.repos("api/users", "web"), ops.AddOptions{}))
	testutil.WriteFile(t, f.wt("pay", "web")+"/dirty.txt", "x")
	list, err := ops.ListFeatures(f.ws, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "empty" || list[0].Repos != 0 || list[1].Name != "pay" || list[1].Repos != 2 || list[1].Dirty != 1 || list[1].Branch != "pay" {
		t.Fatalf("%+v", list)
	}
}

func TestPrune(t *testing.T) {
	f := newFixture(t, "api/users", "web")
	f.feature("pay")
	f.mustOK(ops.Add(f.ws, "pay", f.repos("api/users", "web"), ops.AddOptions{}))

	// clean feature prunes without force
	plan, err := ops.PlanPrune(f.ws, "pay", 4)
	if err != nil || plan.Blocked {
		t.Fatalf("%+v %v", plan, err)
	}
	rs, err := ops.ExecutePrune(f.ws, plan, false, 4)
	if err != nil {
		t.Fatal(err)
	}
	f.mustOK(rs)
	if f.ws.FeatureExists("pay") || f.ws.HasFeatureMeta("pay") || git.BranchExists(f.ws.RepoBaseDir("web"), "pay") {
		t.Fatal("prune left things behind")
	}
	if wts, _ := git.WorktreeList(f.ws.RepoBaseDir("web")); len(wts) != 0 {
		t.Fatalf("worktree still registered: %+v", wts)
	}
}

func TestPruneBlockers(t *testing.T) {
	f := newFixture(t, "a", "b", "c")
	f.feature("pay")
	f.mustOK(ops.Add(f.ws, "pay", f.repos("a", "b", "c"), ops.AddOptions{}))
	testutil.WriteFile(t, f.wt("pay", "a")+"/dirty.txt", "x")                // dirty
	testutil.Commit(t, f.wt("pay", "b"), "b.txt", "x", "unpushed")           // no upstream, ahead of base
	testutil.Commit(t, f.wt("pay", "c"), "c.txt", "x", "pushed")             // pushed but unmerged
	testutil.Git(t, f.wt("pay", "c"), "push", "-q", "-u", "origin", "pay")

	plan, err := ops.PlanPrune(f.ws, "pay", 4)
	if err != nil || !plan.Blocked {
		t.Fatalf("%+v %v", plan, err)
	}
	want := map[string]string{"a": "uncommitted changes", "b": "no upstream", "c": "not merged into main"}
	for _, c := range plan.Checks {
		if !strings.Contains(strings.Join(c.Blockers, ";"), want[c.Repo]) {
			t.Errorf("%s: blockers %v, want %q", c.Repo, c.Blockers, want[c.Repo])
		}
	}
	if c := plan.Checks[2]; strings.Contains(strings.Join(c.Blockers, ";"), "not pushed") {
		t.Errorf("c is pushed; got %v", c.Blockers)
	}
	if _, err := ops.ExecutePrune(f.ws, plan, false, 4); err == nil {
		t.Fatal("blocked plan executed without force")
	}
	if !f.ws.FeatureExists("pay") {
		t.Fatal("refused prune must not touch anything")
	}
	rs, err := ops.ExecutePrune(f.ws, plan, true, 4)
	if err != nil {
		t.Fatal(err)
	}
	f.mustOK(rs)
	if f.ws.FeatureExists("pay") || git.BranchExists(f.ws.RepoBaseDir("b"), "pay") {
		t.Fatal("forced prune incomplete")
	}
}

func TestPruneMergedIntoRemoteBaseIsClean(t *testing.T) {
	f := newFixture(t, "a")
	f.feature("pay")
	f.mustOK(ops.Add(f.ws, "pay", f.repos("a"), ops.AddOptions{}))
	testutil.Commit(t, f.wt("pay", "a"), "a.txt", "x", "work")
	testutil.Git(t, f.wt("pay", "a"), "push", "-q", "-u", "origin", "pay")
	// merge on the "server": push pay to main from another clone
	other := testutil.Clone(t, f.origins["a"], t.TempDir()+"/o")
	testutil.Git(t, other, "fetch", "-q", "origin", "pay")
	testutil.Git(t, other, "merge", "-q", "--ff-only", "origin/pay")
	testutil.Git(t, other, "push", "-q", "origin", "main")
	testutil.Git(t, f.ws.RepoBaseDir("a"), "fetch", "-q")
	plan, _ := ops.PlanPrune(f.ws, "pay", 4)
	if plan.Blocked {
		t.Fatalf("merged into origin/main should not block: %+v", plan.Checks)
	}
}
