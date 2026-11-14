package controller

import (
	"fmt"
	"herdr-workspace-enhancer/repository/herdr"
	"herdr-workspace-enhancer/service"
	"herdr-workspace-enhancer/tui"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/samber/do/v2"
	"github.com/spf13/cobra"
)

func (c *Controller) tuiCommand() *cobra.Command {
	return &cobra.Command{Use: "tui", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := do.Invoke[*service.Service](c.injector)
		if err != nil {
			return err
		}
		host, err := do.Invoke[*herdr.Client](c.injector)
		if err != nil {
			return err
		}
		manager := os.Getenv("HERDR_WORKSPACE_ID")
		isManager, err := s.Store.IsManager(manager)
		if err != nil {
			return err
		}
		if manager == "" || !isManager {
			return fmt.Errorf("open the manager using the plugin action; this is not its dedicated workspace")
		}
		s.ManagerID = manager
		s.MutationError = host.CheckCapabilities(cmd.Context())
		m := tui.New(s, host.Focus)
		m.Settings, err = do.Invoke[*service.SettingsService](c.injector)
		if err != nil {
			return err
		}
		m.SetRatio()
		_, err = tea.NewProgram(m, tea.WithMouseCellMotion(), tea.WithAltScreen(), tea.WithContext(cmd.Context())).Run()
		return err
	}}
}
