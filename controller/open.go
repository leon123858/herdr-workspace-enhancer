package controller

import (
	"herdr-workspace-enhancer/repository/herdr"
	"herdr-workspace-enhancer/repository/state"
	"herdr-workspace-enhancer/service"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"
)

func (c *Controller) openCommand() *cobra.Command {
	return &cobra.Command{Use: "open", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		host, err := do.Invoke[*herdr.Client](c.injector)
		if err != nil {
			return err
		}
		store, err := do.Invoke[*state.Store](c.injector)
		if err != nil {
			return err
		}
		return service.OpenManager(cmd.Context(), host, store)
	}}
}
