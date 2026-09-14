package ops_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kumibrr/gibbon/internal/ops"
	"github.com/kumibrr/gibbon/internal/testutil"
	"github.com/kumibrr/gibbon/internal/workspace"
)

func findings(fs []ops.Finding, check string) []ops.Finding {
	var out []ops.Finding
	for _, f := range fs {
		if f.Check == check {
			out = append(out, f)
		}
	}
	return out
}

func TestDoctorClean(t *testing.T) {
	f := newFixture(t, "a")
	f.feature("pay")
	f.mustOK(ops.Add(f.ws, "pay", f.repos("a"), ops.AddOptions{}))
	fs, err := ops.Doctor(f.ws, false)
	if err != nil || len(fs) != 0 {
		t.Fatalf("%+v %v", fs, err)
	}
}

func TestDoctorChecksAndFix(t *testing.T) {
	f := newFixture(t, "a", "b", "c", "d", "e", "g")
	f.feature("pay")
	f.mustOK(ops.Add(f.ws, "pay", f.repos("a", "b", "c", "d", "g"), ops.AddOptions{}))

	// 1. legacy .worktrees in base repo e (excluded from status, as in real setups)
	testutil.WriteFile(t, filepath.Join(f.ws.RepoBaseDir("e"), ".git", "info", "exclude"), ".worktrees/\n")
	testutil.Git(t, f.ws.RepoBaseDir("e"), "worktree", "add", "-q", "-b", "old", f.ws.RepoBaseDir("e")+"/.worktrees/old", "main")
	// 2. stale registration: a's feature dir deleted
	os.RemoveAll(f.wt("pay", "a"))
	// 3. unregistered dir: stray clone as b2, and a broken link for g (registration pruned)
	testutil.NewRepo(t, f.wt("pay", "b2"), "main")
	// 4. no-base: b2 also has no base -> covered by same stray
	// 5. orphan branch: remove c's worktree via git, keep branch
	testutil.Git(t, f.ws.RepoBaseDir("c"), "worktree", "remove", f.wt("pay", "c"))
	// a branch matching no feature is not an orphan under the bare template
	testutil.Git(t, f.ws.RepoBaseDir("d"), "branch", "unrelated", "main")
	// 6. metadata without dir, dir without metadata
	f.ws.SaveFeature("ghost", workspace.FeatureMeta{})
	os.MkdirAll(f.ws.FeatureDir("nometa"), 0o755)
	// 7. stale config
	cfg, _ := f.ws.LoadConfig()
	cfg.Repos["gone/repo"] = workspace.RepoConfig{BaseBranch: "main"}
	f.ws.SaveConfig(cfg)
	// 8. base dirty / off-branch
	testutil.WriteFile(t, f.ws.RepoBaseDir("d")+"/wip", "x")
	testutil.Git(t, f.ws.RepoBaseDir("b"), "checkout", "-q", "--detach")
	// broken link for g: the registration's gitdir pointer is wrong (as after a
	// manual move); the worktree's .git file is intact -> repairable
	testutil.WriteFile(t, filepath.Join(f.ws.RepoBaseDir("g"), ".git", "worktrees", "g", "gitdir"), "/nonexistent/g/.git\n")

	fs, err := ops.Doctor(f.ws, false)
	if err != nil {
		t.Fatal(err)
	}
	expect := map[string]int{
		"legacy-worktrees": 1, "stale-registration": 2, "unregistered-dir": 1, "no-base": 1,
		"orphan-branch": 3, "orphan-metadata": 1, "missing-metadata": 1, "stale-config": 1,
		"base-dirty": 1, "base-off-branch": 1,
	}
	for check, n := range expect {
		if got := len(findings(fs, check)); got != n {
			t.Errorf("%s: got %d want %d: %+v", check, got, n, findings(fs, check))
		}
	}
	for _, fd := range fs {
		if fd.Fixed {
			t.Errorf("nothing should be fixed without --fix: %+v", fd)
		}
	}

	fs, err = ops.Doctor(f.ws, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []string{"stale-registration", "missing-metadata", "stale-config"} {
		for _, fd := range findings(fs, check) {
			if !fd.Fixable || !fd.Fixed {
				t.Errorf("%s not fixed: %+v", check, fd)
			}
		}
	}
	for _, fd := range findings(fs, "unregistered-dir") {
		if fd.Repo == "g" && !fd.Fixed {
			t.Errorf("g not repaired: %+v", fd)
		}
	}
	for _, fd := range findings(fs, "no-base") {
		if fd.Fixable {
			t.Errorf("stray clone must not be fixable: %+v", fd)
		}
	}
	for _, check := range []string{"orphan-branch", "legacy-worktrees", "base-dirty", "no-base", "orphan-metadata"} {
		for _, fd := range findings(fs, check) {
			if fd.Fixed {
				t.Errorf("%s must never be auto-fixed", check)
			}
		}
	}

	// third run: fixed things are gone
	fs, _ = ops.Doctor(f.ws, false)
	for _, check := range []string{"stale-registration", "missing-metadata", "stale-config"} {
		if n := len(findings(fs, check)); n != 0 {
			t.Errorf("%s still reported after fix: %+v", check, findings(fs, check))
		}
	}
	if n := len(findings(fs, "unregistered-dir")); n != 0 {
		t.Errorf("g still unregistered after repair: %+v", findings(fs, "unregistered-dir"))
	}
	if n := len(findings(fs, "orphan-branch")); n != 2 {
		t.Errorf("expected orphans a and c, got %+v", findings(fs, "orphan-branch"))
	}
}
