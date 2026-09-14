package workspace_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kumibrr/gibbon/internal/testutil"
	"github.com/kumibrr/gibbon/internal/workspace"
)

func newWS(t *testing.T) *workspace.Workspace {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".gibbon"), 0o755); err != nil {
		t.Fatal(err)
	}
	return workspace.New(root)
}

func TestFind(t *testing.T) {
	ws := newWS(t)
	deep := filepath.Join(ws.Root, "feat", "api", "svc")
	os.MkdirAll(deep, 0o755)
	found, err := workspace.Find(deep)
	if err != nil || found.Root != ws.Root {
		t.Fatalf("got %v %v", found, err)
	}
	if _, err := workspace.Find(t.TempDir()); err == nil {
		t.Fatal("expected not found")
	}
}

func TestFeatureContaining(t *testing.T) {
	ws := newWS(t)
	os.MkdirAll(filepath.Join(ws.Root, "pay", "api"), 0o755)
	os.MkdirAll(filepath.Join(ws.Root, "base", "api"), 0o755)
	if f, ok := ws.FeatureContaining(filepath.Join(ws.Root, "pay", "api")); !ok || f != "pay" {
		t.Fatalf("got %q %v", f, ok)
	}
	if _, ok := ws.FeatureContaining(filepath.Join(ws.Root, "base", "api")); ok {
		t.Fatal("base should not be a feature")
	}
	if _, ok := ws.FeatureContaining(ws.Root); ok {
		t.Fatal("root is not a feature")
	}
	if _, ok := ws.FeatureContaining(filepath.Join(ws.Root, ".gibbon")); ok {
		t.Fatal(".gibbon is not a feature")
	}
}

func TestConfigRoundTrip(t *testing.T) {
	ws := newWS(t)
	cfg, err := ws.LoadConfig()
	if err != nil || cfg.BranchTemplate != "{feature}" || cfg.Workers != 8 {
		t.Fatalf("defaults %+v %v", cfg, err)
	}
	cfg.BranchTemplate = "feat/{feature}"
	cfg.Repos["api/users"] = workspace.RepoConfig{BaseBranch: "develop"}
	if err := ws.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	got, _ := ws.LoadConfig()
	if got.BranchTemplate != "feat/{feature}" || got.Repos["api/users"].BaseBranch != "develop" {
		t.Fatalf("round trip %+v", got)
	}
}

func TestFeatureMeta(t *testing.T) {
	ws := newWS(t)
	if ws.HasFeatureMeta("x") {
		t.Fatal("should not exist")
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := ws.SaveFeature("x", workspace.FeatureMeta{BranchTemplate: "wip/{feature}", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	m, _ := ws.LoadFeature("x")
	if m.BranchTemplate != "wip/{feature}" || !m.CreatedAt.Equal(now) {
		t.Fatalf("%+v", m)
	}
	b, _ := ws.FeatureBranch("x")
	if b != "wip/x" {
		t.Fatalf("branch %q", b)
	}
	ws.SaveFeature("y", workspace.FeatureMeta{CreatedAt: now})
	b, _ = ws.FeatureBranch("y")
	if b != "y" {
		t.Fatalf("branch %q", b)
	}
	names, _ := ws.ListFeatureMeta()
	if len(names) != 2 {
		t.Fatalf("%v", names)
	}
	if err := ws.DeleteFeature("x"); err != nil || ws.HasFeatureMeta("x") {
		t.Fatal("delete failed")
	}
	if err := ws.DeleteFeature("x"); err != nil {
		t.Fatal("double delete should be nil")
	}
}

func TestValidateFeatureName(t *testing.T) {
	bad := []string{"", "base", ".hidden", "a/b", "a..b", "x.lock", "has space", "tilde~", "star*", "end.", "-dash"}
	for _, n := range bad {
		if workspace.ValidateFeatureName(n) == nil {
			t.Errorf("%q should be invalid", n)
		}
	}
	good := []string{"payments-v2", "JIRA_123", "ok.name", "a.b.c"}
	for _, n := range good {
		if err := workspace.ValidateFeatureName(n); err != nil {
			t.Errorf("%q should be valid: %v", n, err)
		}
	}
}

func TestDetectBaseBranch(t *testing.T) {
	// origin/HEAD present
	clone, _ := testutil.NewRepo(t, filepath.Join(t.TempDir(), "r"), "develop")
	if b, _ := workspace.DetectBaseBranch(clone); b != "develop" {
		t.Fatalf("got %q", b)
	}
	// no remote, main present but checked out elsewhere
	local := filepath.Join(t.TempDir(), "l")
	os.MkdirAll(local, 0o755)
	testutil.Git(t, local, "init", "-q", "-b", "main")
	testutil.Commit(t, local, "a", "a", "a")
	testutil.Git(t, local, "checkout", "-qb", "other")
	if b, _ := workspace.DetectBaseBranch(local); b != "main" {
		t.Fatalf("got %q", b)
	}
	// no remote, no main/master -> current
	local2 := filepath.Join(t.TempDir(), "l2")
	os.MkdirAll(local2, 0o755)
	testutil.Git(t, local2, "init", "-q", "-b", "trunk")
	testutil.Commit(t, local2, "a", "a", "a")
	if b, _ := workspace.DetectBaseBranch(local2); b != "trunk" {
		t.Fatalf("got %q", b)
	}
}

func TestBaseBranchPersists(t *testing.T) {
	ws := newWS(t)
	testutil.NewRepo(t, ws.RepoBaseDir("api/users"), "develop")
	b, err := ws.BaseBranch("api/users")
	if err != nil || b != "develop" {
		t.Fatalf("%q %v", b, err)
	}
	cfg, _ := ws.LoadConfig()
	if cfg.Repos["api/users"].BaseBranch != "develop" {
		t.Fatalf("not persisted: %+v", cfg)
	}
	cfg.Repos["api/users"] = workspace.RepoConfig{BaseBranch: "release"}
	ws.SaveConfig(cfg)
	b, _ = ws.BaseBranch("api/users")
	if b != "release" {
		t.Fatalf("config override ignored: %q", b)
	}
}
