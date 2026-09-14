package workspace

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// FeatureMeta is what cannot be derived about a feature from the filesystem.
type FeatureMeta struct {
	BranchTemplate string    `toml:"branch_template,omitempty"`
	CreatedAt      time.Time `toml:"created_at"`
}

func (w *Workspace) featurePath(name string) string {
	return filepath.Join(w.FeaturesDir(), name+".toml")
}

// LoadFeature reads metadata; a missing file yields a zero value and no error.
func (w *Workspace) LoadFeature(name string) (FeatureMeta, error) {
	var m FeatureMeta
	data, err := os.ReadFile(w.featurePath(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return m, nil
		}
		return m, err
	}
	err = toml.Unmarshal(data, &m)
	return m, err
}

// HasFeatureMeta reports whether a metadata file exists.
func (w *Workspace) HasFeatureMeta(name string) bool {
	_, err := os.Stat(w.featurePath(name))
	return err == nil
}

// SaveFeature writes metadata.
func (w *Workspace) SaveFeature(name string, m FeatureMeta) error {
	if err := os.MkdirAll(w.FeaturesDir(), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(m); err != nil {
		return err
	}
	return os.WriteFile(w.featurePath(name), buf.Bytes(), 0o644)
}

// DeleteFeature removes metadata; missing is not an error.
func (w *Workspace) DeleteFeature(name string) error {
	err := os.Remove(w.featurePath(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// ListFeatureMeta returns names that have a metadata file.
func (w *Workspace) ListFeatureMeta() ([]string, error) {
	entries, err := os.ReadDir(w.FeaturesDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".toml") {
			names = append(names, strings.TrimSuffix(e.Name(), ".toml"))
		}
	}
	return names, nil
}

// FeatureBranch resolves the branch name for a feature: its own template
// override if set, else the workspace template.
func (w *Workspace) FeatureBranch(name string) (string, error) {
	m, err := w.LoadFeature(name)
	if err != nil {
		return "", err
	}
	tpl := m.BranchTemplate
	if tpl == "" {
		cfg, err := w.LoadConfig()
		if err != nil {
			return "", err
		}
		tpl = cfg.BranchTemplate
	}
	return ExpandTemplate(tpl, name), nil
}

// ExpandTemplate substitutes {feature} in tpl.
func ExpandTemplate(tpl, feature string) string {
	return strings.ReplaceAll(tpl, "{feature}", feature)
}
