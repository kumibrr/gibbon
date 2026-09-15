package cli

import (
	"fmt"
	"github.com/kumibrr/gibbon/internal/pathx"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kumibrr/gibbon/internal/ops"
	"github.com/kumibrr/gibbon/internal/output"
	"github.com/kumibrr/gibbon/internal/shell"
)

func (a *app) featCmd() *cobra.Command {
	var create, from, template string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "feat [-c NAME]",
		Short: "List features, or create one with -c",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if create != "" {
				return a.featCreate(create, from, template, asJSON)
			}
			if from != "" || template != "" {
				return fmt.Errorf("--from and --branch-template require -c NAME")
			}
			return a.featList(asJSON)
		},
	}
	cmd.Flags().StringVarP(&create, "create", "c", "", "create a feature with this name")
	cmd.Flags().StringVar(&from, "from", "", "with -c: add the same repos as this feature (branched from base)")
	cmd.Flags().StringVar(&template, "branch-template", "", "with -c: branch name template for this feature, e.g. feat/{feature}")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")
	cmd.AddCommand(a.featListCmd(), a.featSwitchCmd(), a.featPruneCmd())
	return cmd
}

func (a *app) featCreate(name, from, template string, asJSON bool) error {
	ws, workers, err := a.workspace()
	if err != nil {
		return err
	}
	dir := ws.FeatureDir(name)
	p := a.newProgress(asJSON)
	defer p.stop()
	if p != nil {
		fmt.Fprintf(a.out, "Creating feature %s at %s\n", a.colour.Bold(name), dir)
	}
	rs, err := ops.CreateFeature(ws, name, ops.CreateFeatureOptions{From: from, BranchTemplate: template, Workers: workers, Progress: p})
	if err != nil {
		return err
	}
	wrapped, cdErr := shell.RequestCD(dir)
	if cdErr != nil {
		return cdErr
	}
	if asJSON {
		return a.printResults(rs, true, nil)
	}
	if p == nil {
		fmt.Fprintf(a.out, "Created feature %s at %s\n", a.colour.Bold(name), dir)
	}
	if err := a.printResults(rs, false, p); err != nil {
		return err
	}
	if p != nil {
		fmt.Fprintf(a.out, "Created feature %s\n", a.colour.Bold(name))
	}
	if !wrapped {
		fmt.Fprintln(a.out, a.colour.Dim("cd "+dir))
	}
	return nil
}

func (a *app) featListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List features",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return a.featList(asJSON) },
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")
	return cmd
}

func (a *app) featList(asJSON bool) error {
	ws, workers, err := a.workspace()
	if err != nil {
		return err
	}
	list, err := ops.ListFeatures(ws, workers)
	if err != nil {
		return err
	}
	if asJSON {
		return output.JSON(a.out, list)
	}
	if len(list) == 0 {
		fmt.Fprintln(a.out, a.colour.Dim("No features. Create one with: gibbon feat -c NAME"))
		return nil
	}
	current, _ := ws.FeatureContaining(a.cwd)
	rows := [][]string{}
	for _, f := range list {
		mark := " "
		if f.Name == current {
			mark = "*"
		}
		dirty := ""
		if f.Dirty > 0 {
			dirty = a.colour.Yellow(strconv.Itoa(f.Dirty))
		}
		created := ""
		if f.CreatedAt != "" {
			created = a.colour.Dim(strings.SplitN(f.CreatedAt, "T", 2)[0])
		}
		rows = append(rows, []string{mark, f.Name, strconv.Itoa(f.Repos), dirty, f.Branch, created})
	}
	output.Table(a.out, a.colour, []string{"", "FEATURE", "REPOS", "DIRTY", "BRANCH", "CREATED"}, rows)
	return nil
}

func (a *app) featSwitchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "switch NAME",
		Short: "cd to a feature (or base) via the shell wrapper; prints the path otherwise",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, _, err := a.workspace()
			if err != nil {
				return err
			}
			dir, err := ops.SwitchPath(ws, args[0])
			if err != nil {
				return err
			}
			if _, err := shell.RequestCD(dir); err != nil {
				return err
			}
			fmt.Fprintln(a.out, dir)
			return nil
		},
	}
}

func (a *app) featPruneCmd() *cobra.Command {
	var force, asJSON bool
	cmd := &cobra.Command{
		Use:   "prune [NAME]",
		Short: "Remove a feature: its worktrees, branches, directory and metadata",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, workers, err := a.workspace()
			if err != nil {
				return err
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			feature, err := a.currentFeature(ws, name)
			if err != nil {
				return err
			}
			plan, err := ops.PlanPrune(ws, feature, workers)
			if err != nil {
				return err
			}
			if !asJSON && len(plan.Checks) > 0 {
				rows := [][]string{}
				for _, c := range plan.Checks {
					bl := a.colour.Green("ok")
					if len(c.Blockers) > 0 {
						bl = a.colour.Red(strings.Join(c.Blockers, "; "))
					}
					rows = append(rows, []string{c.Repo, c.Branch, bl})
				}
				output.Table(a.out, a.colour, []string{"REPO", "BRANCH", "CHECK"}, rows)
			}
			if plan.Blocked && !force {
				if asJSON {
					output.JSON(a.out, plan)
				}
				return fmt.Errorf("feature %q has blockers; nothing was removed (use --force to override)", feature)
			}
			p := a.newProgress(asJSON)
			defer p.stop()
			rs, execErr := ops.ExecutePrune(ws, plan, force, workers, p)
			if asJSON {
				output.JSON(a.out, map[string]any{"plan": plan, "results": rs, "error": errString(execErr)})
			} else if p != nil {
				p.finish()
			} else if len(rs) > 0 {
				fmt.Fprintln(a.out)
				a.printResults(rs, false, nil)
			}
			if execErr != nil {
				return execErr
			}
			if pathx.Within(ws.FeatureDir(feature), a.cwd) {
				wrapped, _ := shell.RequestCD(ws.Root)
				if !wrapped && !asJSON {
					fmt.Fprintln(a.out, a.colour.Yellow("Your shell is inside the removed directory; cd "+ws.Root))
				}
			}
			if !asJSON {
				fmt.Fprintf(a.out, "Pruned feature %s\n", a.colour.Bold(feature))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "prune even with uncommitted, unpushed or unmerged work")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")
	return cmd
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
