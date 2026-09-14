// Package discover derives repo and feature models from the filesystem.
package discover

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kumibrr/gibbon/internal/git"
)

// Repo is a git repository found under a root, identified by its relative path.
type Repo struct {
	ID   string // slash-separated path relative to the root
	Path string // absolute path
}

// Walk describes everything found while walking a root.
type Walk struct {
	Repos   []Repo
	Skipped []string // relative paths of files and repo-less leaf directories
}

// WalkRoot walks root recursively. A directory containing .git is a repo and
// is not descended into. Dot-directories and dot-files are ignored. Entries
// named in skipTopLevel at the top level are ignored.
func WalkRoot(root string, skipTopLevel ...string) (Walk, error) {
	var w Walk
	skip := map[string]bool{}
	for _, s := range skipTopLevel {
		skip[s] = true
	}
	err := walk(root, root, skip, &w)
	sort.Slice(w.Repos, func(i, j int) bool { return w.Repos[i].ID < w.Repos[j].ID })
	sort.Strings(w.Skipped)
	return w, err
}

func walk(root, dir string, skipTop map[string]bool, w *Walk) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if dir == root && skipTop[name] {
			continue
		}
		full := filepath.Join(dir, name)
		rel, _ := filepath.Rel(root, full)
		rel = filepath.ToSlash(rel)
		if !e.IsDir() {
			w.Skipped = append(w.Skipped, rel)
			continue
		}
		if git.IsRepo(full) {
			w.Repos = append(w.Repos, Repo{ID: rel, Path: full})
			continue
		}
		before := len(w.Repos)
		if err := walk(root, full, skipTop, w); err != nil {
			return err
		}
		if len(w.Repos) == before {
			w.Skipped = append(w.Skipped, rel+"/")
		}
	}
	return nil
}

// Repos returns the repos under root, without the skipped entries.
func Repos(root string, skipTopLevel ...string) ([]Repo, error) {
	w, err := WalkRoot(root, skipTopLevel...)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return w.Repos, nil
}
