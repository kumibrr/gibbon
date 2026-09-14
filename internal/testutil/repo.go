// Package testutil builds real git repositories for tests.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Git runs git in dir and fails the test on error. Returns trimmed stdout.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// NewOrigin creates a bare repo with one commit on `branch` and HEAD pointing at it.
func NewOrigin(t testing.TB, branch string) string {
	t.Helper()
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	bare := filepath.Join(root, "origin.git")
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	Git(t, seed, "init", "-q", "-b", branch)
	WriteFile(t, filepath.Join(seed, "README.md"), "seed\n")
	Git(t, seed, "add", ".")
	Git(t, seed, "commit", "-qm", "init")
	Git(t, root, "clone", "-q", "--bare", seed, bare)
	Git(t, bare, "symbolic-ref", "HEAD", "refs/heads/"+branch)
	return bare
}

// Clone clones origin into dest (parents created) and returns dest.
func Clone(t testing.TB, origin, dest string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	Git(t, filepath.Dir(dest), "clone", "-q", origin, dest)
	return dest
}

// NewRepo creates an origin on `branch` and a clone of it at dest.
func NewRepo(t testing.TB, dest, branch string) (clone, origin string) {
	t.Helper()
	origin = NewOrigin(t, branch)
	return Clone(t, origin, dest), origin
}

// WriteFile writes content, creating parent directories.
func WriteFile(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Commit writes a file and commits it in dir.
func Commit(t testing.TB, dir, file, content, msg string) {
	t.Helper()
	WriteFile(t, filepath.Join(dir, file), content)
	Git(t, dir, "add", "-A")
	Git(t, dir, "commit", "-qm", msg)
}

// Exists reports whether path exists.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
