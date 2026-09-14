package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kumibrr/gibbon/internal/ops"
	"github.com/kumibrr/gibbon/internal/output"
)

func (a *app) initCmd() *cobra.Command {
	var baseBranch string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "init [DIR]",
		Short: "Turn a directory of repos into a gibbon workspace (moves repos under base/)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := a.cwd
			if len(args) == 1 {
				dir = args[0]
			}
			rep, err := ops.Init(dir, ops.InitOptions{BaseBranch: baseBranch})
			if asJSON {
				output.JSON(a.out, rep)
				return err
			}
			if len(rep.Moved) > 0 {
				rows := [][]string{}
				for _, r := range rep.Moved {
					rows = append(rows, []string{r.Repo, a.colour.Green(r.Action), a.colour.Yellow(joinWarnings(r.Warnings))})
				}
				output.Table(a.out, a.colour, []string{"REPO", "ACTION", "DETAIL"}, rows)
			}
			if len(rep.NotMoved) > 0 {
				fmt.Fprintln(a.out, a.colour.Red("\nNot moved:"))
				for _, id := range rep.NotMoved {
					fmt.Fprintln(a.out, "  "+id)
				}
			}
			if len(rep.Skipped) > 0 {
				fmt.Fprintln(a.out, a.colour.Dim("\nLeft in place (not git repositories):"))
				for _, s := range rep.Skipped {
					fmt.Fprintln(a.out, a.colour.Dim("  "+s))
				}
			}
			if len(rep.Warnings) > 0 {
				fmt.Fprintln(a.out, a.colour.Yellow("\nWarnings:"))
				for _, w := range rep.Warnings {
					fmt.Fprintln(a.out, a.colour.Yellow("  "+w))
				}
			}
			if err == nil {
				fmt.Fprintf(a.out, "\nInitialised workspace with %d repos. Next: eval \"$(gibbon shell-init bash)\" and gibbon feat -c NAME\n", len(rep.Moved))
			}
			return err
		},
	}
	cmd.Flags().StringVar(&baseBranch, "base-branch", "", "record this base branch for every repo instead of detecting")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON")
	return cmd
}
