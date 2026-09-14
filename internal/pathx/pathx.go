// Package pathx normalises filesystem paths so that paths printed by git and
// paths built by gibbon compare equal. git resolves symlinks (macOS /var ->
// /private/var), prints forward slashes and long names on Windows; Go does
// not. Every path that crosses that boundary goes through Canonical.
package pathx

import (
	"os"
	"path/filepath"
	"strings"
)

// Canonical returns an absolute, symlink-resolved, cleaned path. When the path
// does not exist, its longest existing ancestor is resolved and the remaining
// components are re-appended, so paths to deleted worktrees still normalise.
func Canonical(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	dir, rest := abs, ""
	for {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			if rest == "" {
				return resolved
			}
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs
		}
		if rest == "" {
			rest = filepath.Base(dir)
		} else {
			rest = filepath.Join(filepath.Base(dir), rest)
		}
		dir = parent
	}
}

// Same reports whether two paths name the same location.
func Same(a, b string) bool {
	if fa, err := os.Stat(a); err == nil {
		if fb, err := os.Stat(b); err == nil {
			return os.SameFile(fa, fb)
		}
	}
	return Canonical(a) == Canonical(b)
}

// Within reports whether path is inside dir (or equal to it). Both are
// canonicalised first.
func Within(dir, path string) bool {
	rel, err := filepath.Rel(Canonical(dir), Canonical(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
