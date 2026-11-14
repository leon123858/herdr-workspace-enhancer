package controller

import (
	"context"
	"herdr-workspace-enhancer/service"
	"time"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"
)

func (c *Controller) syncCommand() *cobra.Command {
	return &cobra.Command{Use: "sync", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := do.Invoke[*service.Service](c.injector)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
		defer cancel()
		_, err = s.Refresh(ctx)
		return err
	}}
}
