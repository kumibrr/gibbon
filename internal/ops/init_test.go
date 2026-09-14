package ops_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kumibrr/gibbon/internal/git"
	"github.com/kumibrr/gibbon/internal/ops"
	"github.com/kumibrr/gibbon/internal/testutil"
	"github.com/kumibrr/gibbon/internal/workspace"
)

func TestInitNestedTree(t *testing.T) {
	root := t.TempDir()
	users, _ := testutil.NewRepo(t, filepath.Join(root, "api", "users"), "develop")
	testutil.NewRepo(t, filepath.Join(root, "api", "billing"), "main")
	web, _ := testutil.NewRepo(t, filepath.Join(root, "web"), "main")
	testutil.WriteFile(t, filepath.Join(root, "docker-compose.yml"), "x")
	testutil.WriteFile(t, filepath.Join(root, "api", "README.md"), "group readme")
	os.MkdirAll(filepath.Join(root, "empty-group"), 0o755)

	// legacy worktree inside a repo
	legacy := filepath.Join(users, ".worktrees", "old")
	testutil.Git(t, users, "worktree", "add", "-q", "-b", "old", legacy, "develop")
	// dirty + off-base repo
	testutil.WriteFile(t, filepath.Join(web, "wip.txt"), "x")
	testutil.Git(t, web, "checkout", "-qb", "somework")

	rep, err := ops.Init(root, ops.InitOptions{})
	if err != nil {
		t.Fatalf("%v\n%+v", err, rep)
	}
	ws := workspace.New(root)
	for _, id := range []string{"api/users", "api/billing", "web"} {
		if !git.IsRepo(ws.RepoBaseDir(id)) {
			t.Errorf("%s not moved", id)
		}
	}
	if len(rep.Moved) != 3 {
		t.Errorf("moved %+v", rep.Moved)
	}
	if !reflect.DeepEqual(rep.Skipped, []string{"api/README.md", "docker-compose.yml", "empty-group/"}) {
		t.Errorf("skipped %v", rep.Skipped)
	}
	// group folder with a loose file stays; empty group is removed
	if !testutil.Exists(filepath.Join(root, "api", "README.md")) {
		t.Error("loose file in group was removed")
	}
	if testutil.Exists(filepath.Join(root, "empty-group")) {
		t.Error("empty group not removed")
	}
	// legacy worktree still works after repair
	wts, _ := git.WorktreeList(ws.RepoBaseDir("api/users"))
	if len(wts) != 1 || wts[0].Prunable || wts[0].Branch != "old" {
		t.Errorf("legacy worktree broken: %+v", wts)
	}
	if out := testutil.Git(t, filepath.Join(ws.RepoBaseDir("api/users"), ".worktrees", "old"), "rev-parse", "--abbrev-ref", "HEAD"); out != "old" {
		t.Errorf("legacy worktree HEAD %q", out)
	}
	// config
	cfg, _ := ws.LoadConfig()
	if cfg.Repos["api/users"].BaseBranch != "develop" || cfg.Repos["web"].BaseBranch != "main" {
		t.Errorf("config %+v", cfg)
	}
	joined := strings.Join(rep.Warnings, "\n")
	if !strings.Contains(joined, "web: has uncommitted changes") || !strings.Contains(joined, "web: on somework, base branch is main") {
		t.Errorf("warnings %v", rep.Warnings)
	}
	if !testutil.Exists(ws.FeaturesDir()) {
		t.Error("features dir missing")
	}
}

func TestInitBaseBranchFlag(t *testing.T) {
	root := t.TempDir()
	testutil.NewRepo(t, filepath.Join(root, "a"), "main")
	if _, err := ops.Init(root, ops.InitOptions{BaseBranch: "release"}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := workspace.New(root).LoadConfig()
	if cfg.Repos["a"].BaseBranch != "release" {
		t.Fatalf("%+v", cfg)
	}
}

func TestInitRefusals(t *testing.T) {
	t.Run("already initialized", func(t *testing.T) {
		root := t.TempDir()
		testutil.NewRepo(t, filepath.Join(root, "a"), "main")
		os.MkdirAll(filepath.Join(root, ".gibbon"), 0o755)
		if _, err := ops.Init(root, ops.InitOptions{}); err == nil {
			t.Fatal("expected refusal")
		}
	})
	t.Run("base non-empty", func(t *testing.T) {
		root := t.TempDir()
		testutil.NewRepo(t, filepath.Join(root, "a"), "main")
		testutil.WriteFile(t, filepath.Join(root, "base", "x"), "x")
		if _, err := ops.Init(root, ops.InitOptions{}); err == nil {
			t.Fatal("expected refusal")
		}
	})
	t.Run("repo named base", func(t *testing.T) {
		root := t.TempDir()
		testutil.NewRepo(t, filepath.Join(root, "a"), "main")
		testutil.NewRepo(t, filepath.Join(root, "grp", "base"), "main")
		if _, err := ops.Init(root, ops.InitOptions{}); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("expected reserved error, got %v", err)
		}
		if !git.IsRepo(filepath.Join(root, "a")) {
			t.Fatal("nothing should have moved")
		}
	})
	t.Run("no repos", func(t *testing.T) {
		if _, err := ops.Init(t.TempDir(), ops.InitOptions{}); err == nil {
			t.Fatal("expected error")
		}
	})
}
