package controller

import "github.com/spf13/cobra"

// Command builds the CLI entrypoint for this controller.
func (c *Controller) Command() *cobra.Command {
	root := &cobra.Command{
		Use:           "herdr-workspace-enhancer",
		Short:         "Workspace notes and archive manager",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		Args: cobra.NoArgs}
	root.AddCommand(c.openCommand(), c.syncCommand(), c.tuiCommand())
	return root
}
