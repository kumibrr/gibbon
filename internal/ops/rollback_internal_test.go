package ops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kumibrr/gibbon/internal/git"
	"github.com/kumibrr/gibbon/internal/testutil"
	"github.com/kumibrr/gibbon/internal/workspace"
)

// TestRollbackInitRestoresMovedRepos exercises rollbackInit directly against
// a real partially-moved layout. Unlike TestInitRollsBackOnMidLoopFailure,
// it doesn't rely on denying directory write access to induce the failure,
// so it also runs on Windows, where that permission trick doesn't apply.
func TestRollbackInitRestoresMovedRepos(t *testing.T) {
	root := t.TempDir()
	orig := filepath.Join(root, "a")
	testutil.NewRepo(t, orig, "main")

	ws := workspace.New(root)
	dest := ws.RepoBaseDir("a")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(orig, dest); err != nil {
		t.Fatal(err)
	}

	problems := rollbackInit(ws, []movedRepo{{id: "a", orig: orig, dest: dest}}, false)
	if len(problems) != 0 {
		t.Fatalf("rollback reported problems: %v", problems)
	}
	if !git.IsRepo(orig) {
		t.Error("repo was not restored to its original path")
	}
	if testutil.Exists(ws.BaseDir()) {
		t.Error("base/ was not removed after rollback")
	}
}
