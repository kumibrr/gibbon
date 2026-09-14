package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kumibrr/gibbon/internal/shell"
)

func (a *app) shellInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell-init SHELL",
		Short: "Print the shell wrapper; add eval \"$(gibbon shell-init bash)\" to your rc file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			script, err := shell.Script(args[0])
			if err != nil {
				return err
			}
			fmt.Fprint(a.out, script)
			return nil
		},
	}
}
