package git_test

import (
	"path/filepath"
	"testing"

	"github.com/kumibrr/gibbon/internal/git"
	"github.com/kumibrr/gibbon/internal/testutil"
)

func TestBasics(t *testing.T) {
	clone, _ := testutil.NewRepo(t, filepath.Join(t.TempDir(), "r"), "main")
	if !git.IsRepo(clone) || git.IsRepo(t.TempDir()) {
		t.Fatal("IsRepo wrong")
	}
	b, err := git.CurrentBranch(clone)
	if err != nil || b != "main" {
		t.Fatalf("branch %q %v", b, err)
	}
	head, _ := git.OriginHead(clone)
	if head != "main" {
		t.Fatalf("origin head %q", head)
	}
	if !git.BranchExists(clone, "main") || git.BranchExists(clone, "nope") {
		t.Fatal("BranchExists wrong")
	}
	if !git.RemoteBranchExists(clone, "main") {
		t.Fatal("RemoteBranchExists wrong")
	}
	up, _ := git.Upstream(clone, "main")
	if up != "origin/main" {
		t.Fatalf("upstream %q", up)
	}
	testutil.Git(t, clone, "checkout", "-q", "--detach")
	b, _ = git.CurrentBranch(clone)
	if b != "" {
		t.Fatalf("detached should be empty, got %q", b)
	}
}

func TestDirty(t *testing.T) {
	clone, _ := testutil.NewRepo(t, filepath.Join(t.TempDir(), "r"), "main")
	if d, _ := git.IsDirty(clone); d {
		t.Fatal("clean repo reported dirty")
	}
	testutil.WriteFile(t, filepath.Join(clone, ".gitignore"), "ignored.txt\n")
	testutil.Git(t, clone, "add", ".gitignore")
	testutil.Git(t, clone, "commit", "-qm", "ignore")
	testutil.WriteFile(t, filepath.Join(clone, "ignored.txt"), "x")
	if d, _ := git.IsDirty(clone); d {
		t.Fatal("ignored file counted as dirty")
	}
	testutil.WriteFile(t, filepath.Join(clone, "sub", "new.txt"), "x")
	if d, _ := git.IsDirty(clone); !d {
		t.Fatal("untracked file not counted as dirty")
	}
}

func TestAheadBehindMerged(t *testing.T) {
	clone, _ := testutil.NewRepo(t, filepath.Join(t.TempDir(), "r"), "main")
	testutil.Git(t, clone, "checkout", "-qb", "feat")
	testutil.Commit(t, clone, "a.txt", "a", "a")
	ahead, behind, err := git.AheadBehind(clone, "feat", "main")
	if err != nil || ahead != 1 || behind != 0 {
		t.Fatalf("got %d %d %v", ahead, behind, err)
	}
	merged, _ := git.IsMerged(clone, "feat", "main")
	if merged {
		t.Fatal("feat should not be merged")
	}
	merged, _ = git.IsMerged(clone, "main", "feat")
	if !merged {
		t.Fatal("main should be merged into feat")
	}
	branches, _ := git.LocalBranches(clone)
	if len(branches) != 2 {
		t.Fatalf("branches %v", branches)
	}
}

func TestWorktrees(t *testing.T) {
	clone, _ := testutil.NewRepo(t, filepath.Join(t.TempDir(), "r"), "main")
	wt := filepath.Join(t.TempDir(), "wt")
	if err := git.WorktreeAdd(clone, wt, "feat", true, "main"); err != nil {
		t.Fatal(err)
	}
	list, err := git.WorktreeList(clone)
	if err != nil || len(list) != 1 || list[0].Branch != "feat" || list[0].Prunable {
		t.Fatalf("list %+v %v", list, err)
	}
	if err := git.WorktreeRemove(clone, wt, false); err != nil {
		t.Fatal(err)
	}
	// existing branch, no create
	if err := git.WorktreeAdd(clone, wt, "feat", false, ""); err != nil {
		t.Fatal(err)
	}
	// delete directory behind git's back -> prunable
	testutil.Git(t, clone, "worktree", "remove", "--force", wt)
	if err := git.WorktreeAdd(clone, wt, "feat", false, ""); err != nil {
		t.Fatal(err)
	}
	if err := removeAll(wt); err != nil {
		t.Fatal(err)
	}
	list, _ = git.WorktreeList(clone)
	if len(list) != 1 || !list[0].Prunable {
		t.Fatalf("expected prunable, got %+v", list)
	}
	if err := git.WorktreePrune(clone); err != nil {
		t.Fatal(err)
	}
	list, _ = git.WorktreeList(clone)
	if len(list) != 0 {
		t.Fatalf("expected empty, got %+v", list)
	}
	if err := git.DeleteBranch(clone, "feat", false); err != nil {
		t.Fatal(err)
	}
}

func TestFetchTrackFastForward(t *testing.T) {
	clone, origin := testutil.NewRepo(t, filepath.Join(t.TempDir(), "r"), "main")
	other := testutil.Clone(t, origin, filepath.Join(t.TempDir(), "other"))
	testutil.Commit(t, other, "b.txt", "b", "b")
	testutil.Git(t, other, "push", "-q", "origin", "main")
	testutil.Git(t, other, "checkout", "-qb", "remote-only")
	testutil.Git(t, other, "push", "-q", "-u", "origin", "remote-only")
	if err := git.Fetch(clone, true); err != nil {
		t.Fatal(err)
	}
	if !git.RemoteBranchExists(clone, "remote-only") {
		t.Fatal("remote branch not fetched")
	}
	if err := git.CreateTrackingBranch(clone, "remote-only"); err != nil {
		t.Fatal(err)
	}
	if up, _ := git.Upstream(clone, "remote-only"); up != "origin/remote-only" {
		t.Fatalf("upstream %q", up)
	}
	if err := git.FastForward(clone, "main"); err != nil {
		t.Fatal(err)
	}
	if a, b, _ := git.AheadBehind(clone, "main", "origin/main"); a != 0 || b != 0 {
		t.Fatalf("not in sync %d %d", a, b)
	}
}
