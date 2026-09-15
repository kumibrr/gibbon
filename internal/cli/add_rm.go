package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/kumibrr/gibbon/internal/discover"
	"github.com/kumibrr/gibbon/internal/ops"
)

func (a *app) resolveRepos(ws workspaceLike, args []string, all bool) ([]discover.Repo, error) {
	repos, err := discover.Repos(ws.BaseDir())
	if err != nil {
		return nil, err
	}
	if all {
		return repos, nil
	}
	if len(args) == 0 {
		return nil, errors.New("specify at least one repo, or --all")
	}
	return discover.Resolve(repos, args)
}

type workspaceLike interface{ BaseDir() string }

func (a *app) addCmd() *cobra.Command {
	var all, asJSON bool
	var branch, feature string
	cmd := &cobra.Command{
		Use:   "add REPO... [--all]",
		Short: "Add repos to the current feature as worktrees",
		Long: `Add repos to the current feature. REPO may be a repo id (api/users), a bare
name when unique (users), a glob (api/*), or a group folder (api/).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, workers, err := a.workspace()
			if err != nil {
				return err
			}
			feat, err := a.currentFeature(ws, feature)
			if err != nil {
				return err
			}
			repos, err := a.resolveRepos(ws, args, all)
			if err != nil {
				return err
			}
			p := a.newProgress(asJSON)
			defer p.stop()
			rs := ops.Add(ws, feat, repos, ops.AddOptions{Branch: branch, Workers: workers, Progress: p})
			return a.printResults(rs, asJSON, p)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "add every repo in the workspace")
	cmd.Flags().StringVar(&branch, "branch", "", "branch name to use instead of the feature's template")
	cmd.Flags().StringVar(&feature, "feature", "", "target feature (default: the one containing the current directory)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")
	return cmd
}

func (a *app) rmCmd() *cobra.Command {
	var all, deleteBranch, force, asJSON bool
	var feature string
	cmd := &cobra.Command{
		Use:   "rm REPO... [--all]",
		Short: "Remove repos from the current feature (keeps branches unless --delete-branch)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, workers, err := a.workspace()
			if err != nil {
				return err
			}
			feat, err := a.currentFeature(ws, feature)
			if err != nil {
				return err
			}
			repos, err := a.resolveRepos(ws, args, all)
			if err != nil {
				return err
			}
			p := a.newProgress(asJSON)
			defer p.stop()
			rs := ops.Remove(ws, feat, repos, ops.RmOptions{DeleteBranch: deleteBranch, Force: force, Workers: workers, Progress: p})
			return a.printResults(rs, asJSON, p)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "remove every repo from the feature")
	cmd.Flags().BoolVar(&deleteBranch, "delete-branch", false, "also delete the branch (refuses unpushed or unmerged work)")
	cmd.Flags().BoolVar(&force, "force", false, "remove despite uncommitted changes (and unpushed/unmerged with --delete-branch)")
	cmd.Flags().StringVar(&feature, "feature", "", "target feature (default: the one containing the current directory)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")
	return cmd
}
