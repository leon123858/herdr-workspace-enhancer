package main

import (
	"fmt"
	"github.com/samber/do/v2"
	"github.com/spf13/cobra"
	"herdr-workspace-enhancer/bootstrap"
	"os"
)

func newCommand() *cobra.Command {
	root := &cobra.Command{Use: "herdr-workspace-enhancer", Short: "Workspace notes and archive manager", SilenceUsage: true, SilenceErrors: true, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }, Args: cobra.NoArgs}
	for _, name := range []string{"open", "sync", "tui"} {
		name := name
		root.AddCommand(&cobra.Command{Use: name, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			i := bootstrap.New()
			controller, e := do.Invoke[*bootstrap.Controller](i)
			if e != nil {
				return e
			}
			return controller.Run(cmd.Context(), name)
		}})
	}
	return root
}
func main() {
	if e := newCommand().Execute(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
