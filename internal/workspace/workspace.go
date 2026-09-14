// Package workspace locates a gibbon workspace and manages its stored state.
package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	// GibbonDirName is the marker directory at the workspace root.
	GibbonDirName = ".gibbon"
	// BaseDirName holds the primary clones.
	BaseDirName = "base"
	configFile  = "config.toml"
	featuresDir = "features"
)

// ErrNotFound is returned when no workspace contains the start path.
var ErrNotFound = errors.New("not inside a gibbon workspace (no .gibbon directory found)")

// Workspace is a directory containing .gibbon/.
type Workspace struct {
	Root string
}

// Find walks up from start until it finds a .gibbon directory.
func Find(start string) (*Workspace, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	for {
		if fi, err := os.Stat(filepath.Join(dir, GibbonDirName)); err == nil && fi.IsDir() {
			return &Workspace{Root: dir}, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, ErrNotFound
		}
		dir = parent
	}
}

// GibbonDir returns <root>/.gibbon.
func (w *Workspace) GibbonDir() string { return filepath.Join(w.Root, GibbonDirName) }

// BaseDir returns <root>/base.
func (w *Workspace) BaseDir() string { return filepath.Join(w.Root, BaseDirName) }

// FeaturesDir returns <root>/.gibbon/features.
func (w *Workspace) FeaturesDir() string { return filepath.Join(w.GibbonDir(), featuresDir) }

// FeatureDir returns <root>/<name>.
func (w *Workspace) FeatureDir(name string) string { return filepath.Join(w.Root, name) }

// RepoBaseDir returns <root>/base/<id>.
func (w *Workspace) RepoBaseDir(id string) string {
	return filepath.Join(w.BaseDir(), filepath.FromSlash(id))
}

// RepoFeatureDir returns <root>/<feature>/<id>.
func (w *Workspace) RepoFeatureDir(feature, id string) string {
	return filepath.Join(w.FeatureDir(feature), filepath.FromSlash(id))
}

// FeatureContaining returns the feature whose directory contains path.
// It returns false for paths outside the workspace, in base/, or in .gibbon/.
func (w *Workspace) FeatureContaining(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(w.Root, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", false
	}
	first := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
	if first == BaseDirName || strings.HasPrefix(first, ".") {
		return "", false
	}
	if fi, err := os.Stat(w.FeatureDir(first)); err != nil || !fi.IsDir() {
		return "", false
	}
	return first, true
}

// FeatureExists reports whether the feature directory exists.
func (w *Workspace) FeatureExists(name string) bool {
	fi, err := os.Stat(w.FeatureDir(name))
	return err == nil && fi.IsDir()
}
