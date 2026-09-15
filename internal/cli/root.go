// Package cli wires cobra commands to operations. It owns flag parsing and
// rendering; all behaviour lives in internal/ops.
package cli

import (
	"errors"
	"fmt"
	"github.com/kumibrr/gibbon/internal/pathx"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/kumibrr/gibbon/internal/ops"
	"github.com/kumibrr/gibbon/internal/output"
	"github.com/kumibrr/gibbon/internal/workspace"
)

// version is set at build time via -ldflags "-X github.com/kumibrr/gibbon/internal/cli.version=v1.2.3".
var version = "dev"

// errFailed signals a non-zero exit after results were already printed.
var errFailed = errors.New("some repos failed")

// app carries shared state for one invocation.
type app struct {
	out    io.Writer
	err    io.Writer
	colour output.Colour
	jobs   int
	cwd    string
}

// Main runs the CLI and returns the process exit code.
func Main(args []string) int {
	return Run(args, os.Stdout, os.Stderr)
}

// Run executes the CLI with explicit streams; used by tests.
func Run(args []string, stdout, stderr io.Writer) int {
	cwd, _ := os.Getwd()
	cwd = pathx.Canonical(cwd)
	a := &app{out: stdout, err: stderr, colour: output.DetectColour(stdout), cwd: cwd}
	root := a.rootCmd()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if err := root.Execute(); err != nil {
		if !errors.Is(err, errFailed) {
			fmt.Fprintln(stderr, a.colour.Red("error: ")+err.Error())
		}
		return 1
	}
	return 0
}

func (a *app) rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "gibbon",
		Version:       version,
		Short:         "Manage multirepo feature worktrees",
		Long:          "gibbon lays a workspace out as <feature>/<repo> worktrees over primary clones in base/.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().IntVarP(&a.jobs, "jobs", "j", 0, "parallel git operations (default: workspace config, 8)")
	root.AddCommand(a.initCmd(), a.featCmd(), a.addCmd(), a.rmCmd(), a.statusCmd(), a.syncCmd(), a.doctorCmd(), a.shellInitCmd())
	return root
}

// workspace locates the workspace from cwd and resolves the worker count.
func (a *app) workspace() (*workspace.Workspace, int, error) {
	ws, err := workspace.Find(a.cwd)
	if err != nil {
		return nil, 0, err
	}
	workers := a.jobs
	if workers < 1 {
		cfg, err := ws.LoadConfig()
		if err != nil {
			return nil, 0, err
		}
		workers = cfg.Workers
	}
	return ws, workers, nil
}

// currentFeature returns the feature from --feature or from cwd.
func (a *app) currentFeature(ws *workspace.Workspace, flag string) (string, error) {
	if flag != "" {
		if !ws.FeatureExists(flag) {
			return "", fmt.Errorf("feature %q does not exist", flag)
		}
		return flag, nil
	}
	if f, ok := ws.FeatureContaining(a.cwd); ok {
		return f, nil
	}
	return "", errors.New("not inside a feature directory; pass --feature NAME")
}

// printResults renders per-repo results and returns errFailed if any failed.
// When p is non-nil the results were already streamed live, so only the
// summary is printed and the table is skipped.
func (a *app) printResults(rs []ops.Result, asJSON bool, p *reporter) error {
	switch {
	case asJSON:
		if err := output.JSON(a.out, rs); err != nil {
			return err
		}
	case p != nil:
		p.finish()
	case len(rs) > 0:
		rows := make([][]string, 0, len(rs))
		for _, r := range rs {
			detail := ""
			action := r.Action
			switch {
			case r.Err != nil:
				action = a.colour.Red(action)
				detail = a.colour.Red(r.Err.Error())
			case len(r.Warnings) > 0:
				action = a.colour.Yellow(action)
				detail = a.colour.Yellow(joinWarnings(r.Warnings))
			default:
				action = a.colour.Green(action)
			}
			rows = append(rows, []string{r.Repo, action, detail})
		}
		output.Table(a.out, a.colour, []string{"REPO", "ACTION", "DETAIL"}, rows)
	}
	if ops.AnyFailed(rs) {
		return errFailed
	}
	return nil
}

func joinWarnings(ws []string) string {
	s := ""
	for i, w := range ws {
		if i > 0 {
			s += "; "
		}
		s += w
	}
	return s
}
