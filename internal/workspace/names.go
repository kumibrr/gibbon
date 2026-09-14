package workspace

import (
	"fmt"
	"strings"

	"github.com/kumibrr/gibbon/internal/git"
)

// ValidateFeatureName enforces the naming rules from the spec.
func ValidateFeatureName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("feature name must not be empty")
	case name == BaseDirName:
		return fmt.Errorf("%q is reserved", name)
	case strings.HasPrefix(name, "."):
		return fmt.Errorf("feature name must not start with '.'")
	case strings.Contains(name, "/"):
		return fmt.Errorf("feature name must not contain '/' (use the branch template for namespacing)")
	case strings.Contains(name, ".."):
		return fmt.Errorf("feature name must not contain '..'")
	case strings.HasSuffix(name, ".lock"):
		return fmt.Errorf("feature name must not end with '.lock'")
	case strings.ContainsAny(name, " ~^:?*[\\\t\n"):
		return fmt.Errorf("feature name contains characters not allowed in git refs")
	case strings.HasSuffix(name, ".") || strings.HasPrefix(name, "-"):
		return fmt.Errorf("feature name must not end with '.' or start with '-'")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("feature name contains control characters")
		}
	}
	return nil
}

// DetectBaseBranch picks a repo's base branch: origin/HEAD, main, master, current.
func DetectBaseBranch(repoDir string) (string, error) {
	if b, _ := git.OriginHead(repoDir); b != "" {
		return b, nil
	}
	for _, b := range []string{"main", "master"} {
		if git.BranchExists(repoDir, b) {
			return b, nil
		}
	}
	cur, err := git.CurrentBranch(repoDir)
	if err != nil {
		return "", err
	}
	if cur == "" {
		return "", fmt.Errorf("cannot detect base branch: no origin/HEAD, no main/master, HEAD detached")
	}
	return cur, nil
}

// BaseBranch returns the configured base branch for a repo, detecting and
// persisting it on first use.
func (w *Workspace) BaseBranch(repoID string) (string, error) {
	cfg, err := w.LoadConfig()
	if err != nil {
		return "", err
	}
	if rc, ok := cfg.Repos[repoID]; ok && rc.BaseBranch != "" {
		return rc.BaseBranch, nil
	}
	b, err := DetectBaseBranch(w.RepoBaseDir(repoID))
	if err != nil {
		return "", fmt.Errorf("%s: %w", repoID, err)
	}
	cfg.Repos[repoID] = RepoConfig{BaseBranch: b}
	if err := w.SaveConfig(cfg); err != nil {
		return "", err
	}
	return b, nil
}
