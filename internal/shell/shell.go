// Package shell provides the shell wrapper that lets gibbon change directories.
package shell

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed wrapper.sh
var wrapper string

// Script returns the wrapper for the given shell. bash and zsh share one script.
func Script(shell string) (string, error) {
	switch shell {
	case "bash", "zsh":
		return wrapper, nil
	default:
		return "", fmt.Errorf("unsupported shell %q (supported: bash, zsh)", shell)
	}
}

// CDFileEnv names the environment variable the wrapper sets.
const CDFileEnv = "GIBBON_CD_FILE"

// RequestCD asks the wrapper (if present) to cd into dir after the command.
// It reports whether a wrapper was present.
func RequestCD(dir string) (bool, error) {
	path := os.Getenv(CDFileEnv)
	if path == "" {
		return false, nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return true, err
	}
	return true, os.WriteFile(path, []byte(abs+"\n"), 0o600)
}
