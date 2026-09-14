package shell_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kumibrr/gibbon/internal/shell"
)

func TestWrapperChangesDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX wrapper is not exercised on windows")
	}
	script, err := shell.Script("bash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := shell.Script("fish"); err == nil {
		t.Fatal("fish should be unsupported")
	}
	bin := t.TempDir()
	target := t.TempDir()
	// stub gibbon: writes its first arg to the cd file and exits with $2
	stub := "#!/bin/sh\nprintf '%s\\n' \"$1\" > \"$GIBBON_CD_FILE\"\nexit \"${2:-0}\"\n"
	if err := os.WriteFile(filepath.Join(bin, "gibbon"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	wrapperFile := filepath.Join(t.TempDir(), "w.sh")
	os.WriteFile(wrapperFile, []byte(script), 0o644)

	for _, sh := range []string{"bash", "zsh"} {
		if _, err := exec.LookPath(sh); err != nil {
			t.Logf("%s not installed, skipping", sh)
			continue
		}
		t.Run(sh, func(t *testing.T) {
			cmd := exec.Command(sh, "-c", `. "$1"; gibbon "$2"; echo "rc=$?"; pwd; gibbon "$2" 3; echo "rc=$?"`, sh, wrapperFile, target)
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
			cmd.Dir = t.TempDir()
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) != 3 || lines[0] != "rc=0" || lines[2] != "rc=3" {
				t.Fatalf("unexpected output:\n%s", out)
			}
			got, _ := filepath.EvalSymlinks(lines[1])
			want, _ := filepath.EvalSymlinks(target)
			if got != want {
				t.Fatalf("pwd %q want %q", got, want)
			}
		})
	}
}
