package cli_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kumibrr/gibbon/internal/testutil"
)

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gibbon-bin")
	if err != nil {
		panic(err)
	}
	binary = filepath.Join(dir, "gibbon")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "../../cmd/gibbon")
	if out, err := build.CombinedOutput(); err != nil {
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type run struct {
	out  string
	code int
}

func gibbon(t *testing.T, dir string, env []string, args ...string) run {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return run{string(out), code}
}

func TestEndToEnd(t *testing.T) {
	root := t.TempDir()
	testutil.NewRepo(t, filepath.Join(root, "api", "users"), "main")
	testutil.NewRepo(t, filepath.Join(root, "api", "billing"), "main")
	testutil.NewRepo(t, filepath.Join(root, "web"), "main")
	testutil.WriteFile(t, filepath.Join(root, "notes.md"), "x")

	r := gibbon(t, root, nil, "init")
	if r.code != 0 || !strings.Contains(r.out, "3 repos") || !strings.Contains(r.out, "notes.md") {
		t.Fatalf("init: %d\n%s", r.code, r.out)
	}
	if r := gibbon(t, root, nil, "init"); r.code == 0 {
		t.Fatal("second init should fail")
	}

	// create with wrapper cd file
	cdFile := filepath.Join(t.TempDir(), "cd")
	r = gibbon(t, root, []string{"GIBBON_CD_FILE=" + cdFile}, "feat", "-c", "pay")
	if r.code != 0 {
		t.Fatalf("feat -c: %s", r.out)
	}
	if got, _ := os.ReadFile(cdFile); strings.TrimSpace(string(got)) != filepath.Join(root, "pay") {
		t.Fatalf("cd file %q", got)
	}
	if r := gibbon(t, root, nil, "feat", "-c", "bad/name"); r.code == 0 {
		t.Fatal("bad name accepted")
	}

	feat := filepath.Join(root, "pay")
	// add from inside the feature: group + bare name
	r = gibbon(t, feat, nil, "add", "api/", "web")
	if r.code != 0 || strings.Count(r.out, "created") != 3 {
		t.Fatalf("add: %d\n%s", r.code, r.out)
	}
	// ambiguous name fails cleanly
	testutil.NewRepo(t, filepath.Join(root, "base", "legacy", "users"), "main")
	if r := gibbon(t, feat, nil, "add", "users"); r.code == 0 || !strings.Contains(r.out, "ambiguous") {
		t.Fatalf("ambiguous: %d\n%s", r.code, r.out)
	}
	// add from outside needs --feature
	if r := gibbon(t, root, nil, "add", "legacy/users"); r.code == 0 {
		t.Fatal("add outside feature should fail")
	}
	if r := gibbon(t, root, nil, "add", "--feature", "pay", "legacy/users"); r.code != 0 {
		t.Fatalf("add --feature: %s", r.out)
	}

	r = gibbon(t, feat, nil, "feat")
	if r.code != 0 || !strings.Contains(r.out, "pay") || !strings.Contains(r.out, "4") {
		t.Fatalf("feat list: %s", r.out)
	}

	testutil.WriteFile(t, filepath.Join(feat, "web", "dirty.txt"), "x")
	r = gibbon(t, feat, nil, "status", "--json")
	if r.code != 0 {
		t.Fatalf("status: %s", r.out)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(r.out), &rows); err != nil {
		t.Fatalf("status json: %v\n%s", err, r.out)
	}
	if len(rows) != 4 {
		t.Fatalf("status rows %d", len(rows))
	}
	for _, row := range rows {
		if row["repo"] == "web" && row["dirty"] != true {
			t.Fatalf("web should be dirty: %v", row)
		}
	}

	// rm dirty refuses, then keeps branch
	if r := gibbon(t, feat, nil, "rm", "web"); r.code == 0 {
		t.Fatalf("rm dirty should fail:\n%s", r.out)
	}
	if r := gibbon(t, feat, nil, "rm", "--force", "web"); r.code != 0 {
		t.Fatalf("rm --force: %s", r.out)
	}
	if testutil.Git(t, filepath.Join(root, "base", "web"), "branch", "--list", "pay") == "" {
		t.Fatal("branch should be kept after rm")
	}

	if r := gibbon(t, root, nil, "sync"); r.code != 0 || strings.Count(r.out, "up-to-date") != 4 {
		t.Fatalf("sync: %d\n%s", r.code, r.out)
	}

	// doctor: orphan branch pay in web
	r = gibbon(t, root, nil, "doctor")
	if r.code == 0 || !strings.Contains(r.out, "orphan-branch") {
		t.Fatalf("doctor: %d\n%s", r.code, r.out)
	}

	// switch prints path
	r = gibbon(t, root, nil, "feat", "switch", "pay")
	if r.code != 0 || strings.TrimSpace(r.out) != feat {
		t.Fatalf("switch: %s", r.out)
	}
	if r := gibbon(t, root, nil, "feat", "switch", "nope"); r.code == 0 {
		t.Fatal("switch missing should fail")
	}

	// prune from inside relocates the shell to root
	cdFile2 := filepath.Join(t.TempDir(), "cd")
	r = gibbon(t, feat, []string{"GIBBON_CD_FILE=" + cdFile2}, "feat", "prune")
	if r.code != 0 {
		t.Fatalf("prune: %s", r.out)
	}
	if got, _ := os.ReadFile(cdFile2); strings.TrimSpace(string(got)) != root {
		t.Fatalf("prune cd %q", got)
	}
	if testutil.Exists(feat) {
		t.Fatal("feature dir remains")
	}
	if r := gibbon(t, root, nil, "feat", "prune", "base"); r.code == 0 {
		t.Fatal("pruning base must fail")
	}

	// outside a workspace
	if r := gibbon(t, t.TempDir(), nil, "feat"); r.code == 0 || !strings.Contains(r.out, "not inside") {
		t.Fatalf("outside: %s", r.out)
	}
	if r := gibbon(t, root, nil, "shell-init", "bash"); r.code != 0 || !strings.Contains(r.out, "gibbon()") {
		t.Fatalf("shell-init: %s", r.out)
	}
}

func TestVersionFlag(t *testing.T) {
	r := gibbon(t, t.TempDir(), nil, "--version")
	if r.code != 0 || !strings.Contains(r.out, "gibbon version dev") {
		t.Fatalf("--version: code=%d out=%q", r.code, r.out)
	}
}
