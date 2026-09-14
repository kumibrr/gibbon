package pathx_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kumibrr/gibbon/internal/pathx"
)

func TestCanonicalThroughSymlink(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	os.MkdirAll(filepath.Join(real, "a"), 0o755)
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	want := pathx.Canonical(filepath.Join(real, "a", "missing", "deep"))
	got := pathx.Canonical(filepath.Join(link, "a", "missing", "deep"))
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if !pathx.Same(filepath.Join(link, "a"), filepath.Join(real, "a")) {
		t.Fatal("Same should see through the symlink")
	}
	if !pathx.Within(link, filepath.Join(real, "a", "gone")) {
		t.Fatal("Within should see through the symlink")
	}
	if pathx.Within(filepath.Join(real, "a"), filepath.Join(real, "ab")) {
		t.Fatal("sibling with shared prefix is not within")
	}
}
