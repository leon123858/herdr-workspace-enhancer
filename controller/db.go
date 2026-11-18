package controller

import (
	"fmt"
	"github.com/spf13/cobra"
	"herdr-workspace-enhancer/repository/state"
	"os"
)

func (c *Controller) dbCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "db", Short: "Inspect or migrate the session database (stop other session processes first)"}
	cmd.AddCommand(&cobra.Command{Use: "version", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		version, err := state.DBVersion(os.Getenv("HERDR_PLUGIN_STATE_DIR"), os.Getenv("HERDR_SOCKET_PATH"))
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), version)
		return nil
	}})
	var target int64
	migrate := &cobra.Command{Use: "migrate --to VERSION", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		backup, err := state.MigrateDB(os.Getenv("HERDR_PLUGIN_STATE_DIR"), os.Getenv("HERDR_SOCKET_PATH"), target)
		if backup != "" {
			fmt.Fprintln(cmd.OutOrStdout(), "Backup:", backup)
		}
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Database version:", target)
		return nil
	}}
	migrate.Flags().Int64Var(&target, "to", 0, "Target integer database version; downgrading to 0 discards custom ordering")
	_ = migrate.MarkFlagRequired("to")
	cmd.AddCommand(migrate)
	return cmd
}
