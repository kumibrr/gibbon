package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kumibrr/gibbon/internal/discover"
	"github.com/kumibrr/gibbon/internal/ops"
	"github.com/kumibrr/gibbon/internal/output"
)

func (a *app) statusCmd() *cobra.Command {
	var all, asJSON bool
	var feature string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show each repo in the current feature: branch, ahead/behind, dirty, drift",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, workers, err := a.workspace()
			if err != nil {
				return err
			}
			var features []string
			if all {
				features, err = discover.Features(ws)
				if err != nil {
					return err
				}
			} else {
				f, err := a.currentFeature(ws, feature)
				if err != nil {
					return fmt.Errorf("%w (or use --all)", err)
				}
				features = []string{f}
			}
			rows, err := ops.Status(ws, features, workers)
			if err != nil {
				return err
			}
			if asJSON {
				return output.JSON(a.out, rows)
			}
			if len(rows) == 0 {
				fmt.Fprintln(a.out, a.colour.Dim("No repos in feature. Add some with: gibbon add REPO..."))
				return nil
			}
			headers := []string{"REPO", "BRANCH", "BASE", "UPSTREAM", "STATE"}
			if all {
				headers = append([]string{"FEATURE"}, headers...)
			}
			table := [][]string{}
			for _, r := range rows {
				row := []string{r.Repo, r.Branch, a.plusMinus(r.AheadBase, r.BehindBase, r.BaseBranch), a.upstream(r), a.state(r)}
				if all {
					row = append([]string{r.Feature}, row...)
				}
				table = append(table, row)
			}
			output.Table(a.out, a.colour, headers, table)
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "show every feature")
	cmd.Flags().StringVar(&feature, "feature", "", "feature to show (default: the one containing the current directory)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")
	return cmd
}

func (a *app) plusMinus(ahead, behind int, ref string) string {
	if ref == "" {
		return a.colour.Dim("-")
	}
	s := ref
	if ahead > 0 {
		s += a.colour.Green(fmt.Sprintf(" +%d", ahead))
	}
	if behind > 0 {
		s += a.colour.Yellow(fmt.Sprintf(" -%d", behind))
	}
	return s
}

func (a *app) upstream(r ops.RepoStatus) string {
	if r.Upstream == "" {
		return a.colour.Dim("none")
	}
	return a.plusMinus(r.AheadUp, r.BehindUp, r.Upstream)
}

func (a *app) state(r ops.RepoStatus) string {
	var parts []string
	if r.NoBase {
		parts = append(parts, a.colour.Red("no base repo"))
	}
	if r.Unregistered {
		parts = append(parts, a.colour.Red("not a registered worktree"))
	}
	if r.Missing {
		parts = append(parts, a.colour.Red("directory missing"))
	}
	if r.Dirty {
		parts = append(parts, a.colour.Yellow("dirty"))
	}
	if r.Error != "" {
		parts = append(parts, a.colour.Red(r.Error))
	}
	if len(parts) == 0 {
		return a.colour.Green("clean")
	}
	return strings.Join(parts, ", ")
}

func (a *app) syncCmd() *cobra.Command {
	var prune, asJSON bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Fetch every base clone and fast-forward clean base branches",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, workers, err := a.workspace()
			if err != nil {
				return err
			}
			p := a.newProgress(asJSON)
			defer p.stop()
			rs, err := ops.Sync(ws, ops.SyncOptions{Prune: prune, Workers: workers, Progress: p})
			if err != nil {
				return err
			}
			return a.printResults(rs, asJSON, p)
		},
	}
	cmd.Flags().BoolVar(&prune, "prune", false, "prune stale remote-tracking refs while fetching")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")
	return cmd
}

func (a *app) doctorCmd() *cobra.Command {
	var fix, asJSON bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Audit the workspace for orphan branches, broken worktrees and stale state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, _, err := a.workspace()
			if err != nil {
				return err
			}
			fs, err := ops.Doctor(ws, fix)
			if err != nil {
				return err
			}
			if asJSON {
				if err := output.JSON(a.out, fs); err != nil {
					return err
				}
			} else if len(fs) == 0 {
				fmt.Fprintln(a.out, a.colour.Green("Workspace is healthy."))
				return nil
			} else {
				rows := [][]string{}
				for _, f := range fs {
					where := f.Repo
					if f.Feature != "" {
						where = f.Feature + "/" + f.Repo
						if f.Repo == "" {
							where = f.Feature + "/"
						}
					}
					action := ""
					switch {
					case f.Fixed:
						action = a.colour.Green("fixed")
					case f.Fixable:
						action = a.colour.Yellow("run with --fix")
					case f.Suggest != "":
						action = a.colour.Dim(f.Suggest)
					}
					rows = append(rows, []string{f.Check, where, f.Message, action})
				}
				output.Table(a.out, a.colour, []string{"CHECK", "WHERE", "PROBLEM", "ACTION"}, rows)
			}
			remaining := 0
			for _, f := range fs {
				if !f.Fixed {
					remaining++
				}
			}
			if remaining > 0 {
				return errFailed
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&fix, "fix", false, "apply non-destructive repairs (worktree prune/repair, metadata, stale config)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")
	return cmd
}
