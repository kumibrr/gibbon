package ops_test

import (
	"strings"
	"testing"

	"github.com/kumibrr/gibbon/internal/git"
	"github.com/kumibrr/gibbon/internal/ops"
	"github.com/kumibrr/gibbon/internal/pathx"
	"github.com/kumibrr/gibbon/internal/testutil"
)

func TestAddBranchSources(t *testing.T) {
	f := newFixture(t, "fresh", "local", "remote")
	f.feature("pay")
	// local branch pre-exists in base clone
	testutil.Git(t, f.ws.RepoBaseDir("local"), "branch", "pay", "main")
	// remote-only branch
	f.pushFromOther("remote", "pay", "r.txt")

	rec := &recProgress{}
	rs := ops.Add(f.ws, "pay", f.repos("fresh", "local", "remote"), ops.AddOptions{Workers: 3, Progress: rec})
	f.mustOK(rs)
	assertProgress(t, rec, 3, "fresh", "local", "remote")
	actions := map[string]string{}
	for _, r := range rs {
		actions[r.Repo] = r.Action
	}
	if actions["fresh"] != "created" || actions["local"] != "checked-out-local" || actions["remote"] != "tracked-remote" {
		t.Fatalf("%v", actions)
	}
	for _, id := range []string{"fresh", "local", "remote"} {
		if b := testutil.Git(t, f.wt("pay", id), "rev-parse", "--abbrev-ref", "HEAD"); b != "pay" {
			t.Errorf("%s on %q", id, b)
		}
	}
	if up, _ := git.Upstream(f.ws.RepoBaseDir("remote"), "pay"); up != "origin/pay" {
		t.Errorf("remote branch not tracking: %q", up)
	}
	if !exists(f.wt("pay", "remote") + "/r.txt") {
		t.Error("remote content missing")
	}
	// idempotent
	rs = ops.Add(f.ws, "pay", f.repos("fresh"), ops.AddOptions{})
	if rs[0].Action != "exists" || rs[0].Err != nil {
		t.Fatalf("%+v", rs[0])
	}
}

func TestAddCreatesFromRemoteBaseAndGroupFolders(t *testing.T) {
	f := newFixture(t, "api/users")
	f.feature("pay")
	f.pushFromOther("api/users", "main", "newer.txt") // base clone is stale
	rs := ops.Add(f.ws, "pay", f.repos("api/users"), ops.AddOptions{Branch: "custom"})
	f.mustOK(rs)
	if !exists(f.wt("pay", "api/users") + "/newer.txt") {
		t.Fatal("branch should start from fetched origin/main")
	}
	if b := testutil.Git(t, f.wt("pay", "api/users"), "rev-parse", "--abbrev-ref", "HEAD"); b != "custom" {
		t.Fatalf("branch override ignored: %q", b)
	}
}

func TestAddFailuresDoNotAbortOthers(t *testing.T) {
	f := newFixture(t, "a", "b")
	f.feature("pay")
	// stray unregistered dir for a
	testutil.WriteFile(t, f.wt("pay", "a")+"/x", "x")
	rec := &recProgress{}
	rs := ops.Add(f.ws, "pay", f.repos("a", "b"), ops.AddOptions{Progress: rec})
	if rs[0].Err == nil || rs[1].Err != nil || rs[1].Action != "created" {
		t.Fatalf("%+v", rs)
	}
	if !ops.AnyFailed(rs) {
		t.Fatal("AnyFailed")
	}
	failingID := "a"
	if rec.index("finish "+failingID+" err") < 0 {
		t.Fatalf("expected an error finish for %s: %v", failingID, rec.events)
	}
	if rs := ops.Add(f.ws, "ghost", f.repos("a"), ops.AddOptions{}); !ops.AnyFailed(rs) {
		t.Fatal("missing feature accepted")
	}
}

func TestRemoveKeepsBranch(t *testing.T) {
	f := newFixture(t, "grp/a", "b")
	f.feature("pay")
	f.mustOK(ops.Add(f.ws, "pay", f.repos("grp/a", "b"), ops.AddOptions{}))
	testutil.Commit(t, f.wt("pay", "grp/a"), "a.txt", "x", "unpushed work")

	rec := &recProgress{}
	rs := ops.Remove(f.ws, "pay", f.repos("grp/a"), ops.RmOptions{Progress: rec})
	f.mustOK(rs)
	assertProgress(t, rec, 1, "grp/a")
	if rs[0].Action != "removed" {
		t.Fatalf("%+v", rs[0])
	}
	if exists(f.wt("pay", "grp/a")) || exists(f.ws.FeatureDir("pay")+"/grp") {
		t.Fatal("dir or empty group folder left behind")
	}
	if !git.BranchExists(f.ws.RepoBaseDir("grp/a"), "pay") {
		t.Fatal("branch should be kept")
	}
	if !exists(f.wt("pay", "b")) {
		t.Fatal("other repo touched")
	}
	// removing again is a no-op
	rs = ops.Remove(f.ws, "pay", f.repos("grp/a"), ops.RmOptions{})
	if rs[0].Action != "not-in-feature" || rs[0].Err != nil {
		t.Fatalf("%+v", rs[0])
	}
}

func TestRemoveRefusesDirty(t *testing.T) {
	f := newFixture(t, "a")
	f.feature("pay")
	f.mustOK(ops.Add(f.ws, "pay", f.repos("a"), ops.AddOptions{}))
	testutil.WriteFile(t, f.wt("pay", "a")+"/dirty", "x")
	rs := ops.Remove(f.ws, "pay", f.repos("a"), ops.RmOptions{})
	if rs[0].Err == nil || !strings.Contains(rs[0].Err.Error(), "uncommitted") {
		t.Fatalf("%+v", rs[0])
	}
	rs = ops.Remove(f.ws, "pay", f.repos("a"), ops.RmOptions{Force: true})
	f.mustOK(rs)
}

func TestRemoveDeleteBranch(t *testing.T) {
	f := newFixture(t, "a")
	f.feature("pay")
	f.mustOK(ops.Add(f.ws, "pay", f.repos("a"), ops.AddOptions{}))
	testutil.Commit(t, f.wt("pay", "a"), "a.txt", "x", "work")
	rs := ops.Remove(f.ws, "pay", f.repos("a"), ops.RmOptions{DeleteBranch: true})
	if rs[0].Err == nil || !strings.Contains(rs[0].Err.Error(), "not on main") {
		t.Fatalf("unmerged should block: %+v", rs[0])
	}
	// merge locally into main, then delete works
	testutil.Git(t, f.ws.RepoBaseDir("a"), "merge", "-q", "--ff-only", "pay")
	rs = ops.Remove(f.ws, "pay", f.repos("a"), ops.RmOptions{DeleteBranch: true})
	f.mustOK(rs)
	if git.BranchExists(f.ws.RepoBaseDir("a"), "pay") {
		t.Fatal("branch not deleted")
	}
}

func TestRemoveUnregisteredDirErrors(t *testing.T) {
	f := newFixture(t, "a")
	f.feature("pay")
	testutil.NewRepo(t, f.wt("pay", "a"), "main")
	rs := ops.Remove(f.ws, "pay", f.repos("a"), ops.RmOptions{Force: true})
	if rs[0].Err == nil {
		t.Fatal("unregistered dir must not be removed")
	}
	if !exists(f.wt("pay", "a")) {
		t.Fatal("dir was removed")
	}
}

func TestAddOrphanAdoptsWorktree(t *testing.T) {
	f := newFixture(t, "legacy", "other")
	f.feature("pay")
	// branch pay checked out in a stray legacy worktree, with committed and dirty work
	legacyWT := f.ws.RepoBaseDir("legacy") + "/.worktrees/pay"
	testutil.Git(t, f.ws.RepoBaseDir("legacy"), "worktree", "add", "-q", "-b", "pay", legacyWT, "main")
	testutil.Commit(t, legacyWT, "work.txt", "x", "wip")
	testutil.WriteFile(t, legacyWT+"/dirty.txt", "x")

	// Without --orphan the checked-out branch cannot be added.
	if rs := ops.Add(f.ws, "pay", f.repos("legacy"), ops.AddOptions{}); rs[0].Err == nil {
		t.Fatal("expected failure: branch already checked out elsewhere")
	}
	// PATH must be a worktree of the named repo.
	if rs := ops.Add(f.ws, "pay", f.repos("other"), ops.AddOptions{Orphan: legacyWT}); rs[0].Err == nil {
		t.Fatal("worktree of another repo accepted")
	}
	if rs := ops.Add(f.ws, "pay", f.repos("legacy"), ops.AddOptions{Orphan: t.TempDir()}); rs[0].Err == nil {
		t.Fatal("non-worktree path accepted")
	}
	// Only one repo per adoption.
	if rs := ops.Add(f.ws, "pay", f.repos("legacy", "other"), ops.AddOptions{Orphan: legacyWT}); len(rs) != 1 || rs[0].Err == nil {
		t.Fatalf("multiple repos accepted: %+v", rs)
	}
	// --branch must agree with what the orphan has checked out.
	if rs := ops.Add(f.ws, "pay", f.repos("legacy"), ops.AddOptions{Orphan: legacyWT, Branch: "x"}); rs[0].Err == nil {
		t.Fatal("branch mismatch accepted")
	}
	if !exists(legacyWT + "/dirty.txt") {
		t.Fatal("refused adoption must leave the worktree in place")
	}

	rec := &recProgress{}
	rs := ops.Add(f.ws, "pay", f.repos("legacy"), ops.AddOptions{Orphan: legacyWT, Progress: rec})
	f.mustOK(rs)
	assertProgress(t, rec, 1, "legacy")
	if rs[0].Action != "adopted" {
		t.Fatalf("%+v", rs[0])
	}
	if exists(legacyWT) {
		t.Fatal("orphan directory should have moved")
	}
	if !exists(f.wt("pay", "legacy")+"/work.txt") || !exists(f.wt("pay", "legacy")+"/dirty.txt") {
		t.Fatal("adopted content missing")
	}
	if b := testutil.Git(t, f.wt("pay", "legacy"), "rev-parse", "--abbrev-ref", "HEAD"); b != "pay" {
		t.Fatalf("on %q", b)
	}
	wts, _ := git.WorktreeList(f.ws.RepoBaseDir("legacy"))
	registered := false
	for _, wt := range wts {
		registered = registered || pathx.Same(wt.Path, f.wt("pay", "legacy"))
	}
	if !registered {
		t.Fatalf("worktree not registered at new path: %+v", wts)
	}
	// destination already occupied
	if rs := ops.Add(f.ws, "pay", f.repos("legacy"), ops.AddOptions{Orphan: f.wt("pay", "legacy")}); rs[0].Err == nil {
		t.Fatalf("adopting into an occupied destination should fail: %+v", rs[0])
	}
}
