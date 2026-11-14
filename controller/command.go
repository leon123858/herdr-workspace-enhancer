package controller

import "github.com/spf13/cobra"

// Command builds the CLI entrypoint for this controller.
func (c *Controller) Command() *cobra.Command {
	root := &cobra.Command{Use: "herdr-workspace-enhancer", Short: "Workspace notes and archive manager", SilenceUsage: true, SilenceErrors: true, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }, Args: cobra.NoArgs}
	for _, name := range []string{"open", "sync", "tui"} {
		name := name
		root.AddCommand(&cobra.Command{Use: name, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			return c.Run(cmd.Context(), name)
		}})
	}
	return root
}
