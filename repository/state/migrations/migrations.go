// Package migrations owns historical database schemas.
package migrations

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
)

// Every new CurrentDBVersion in state
// should add new migration sql in this file
//
// goose.WithGoMigrations(
//     goose.NewGoMigration(1,
//         &goose.GoFunc{RunTx: upWorkspaceOrder},
//         &goose.GoFunc{RunTx: downWorkspaceOrder}),
//     goose.NewGoMigration(2,
//         &goose.GoFunc{RunTx: upWorkspaceColor},
//         &goose.GoFunc{RunTx: downWorkspaceColor}),
// )
func Provider(db *sql.DB) (*goose.Provider, error) {
	return goose.NewProvider(goose.DialectSQLite3, db, nil,
		goose.WithDisableGlobalRegistry(true),
		goose.WithGoMigrations(
			goose.NewGoMigration(1, &goose.GoFunc{RunTx: upWorkspaceOrder}, &goose.GoFunc{RunTx: downWorkspaceOrder}),
			goose.NewGoMigration(2, &goose.GoFunc{RunTx: upClearInactiveHostID}, &goose.GoFunc{RunTx: downClearInactiveHostID}),
		))
}

func upWorkspaceOrder(ctx context.Context, tx *sql.Tx) error {
	steps := []struct{ name, query string }{
		{"create workspaces", `CREATE TABLE IF NOT EXISTS workspaces (
   id text, host_id text, label text,
   status text NOT NULL DEFAULT 'closed', note text NOT NULL DEFAULT '',
   manager_state text NOT NULL DEFAULT 'NONE', snapshot text, PRIMARY KEY (id)
  )`},
		{"index host", `CREATE UNIQUE INDEX IF NOT EXISTS idx_workspaces_host_id ON workspaces(host_id)`},
		{"index status", `CREATE INDEX IF NOT EXISTS idx_workspaces_status ON workspaces(status)`},
		{"index manager", `CREATE INDEX IF NOT EXISTS idx_workspaces_manager_state ON workspaces(manager_state)`},
		{"add ordering", `ALTER TABLE workspaces ADD COLUMN sort_order integer`},
	}
	for _, step := range steps {
		if _, err := tx.ExecContext(ctx, step.query); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}
	return nil
}

func downWorkspaceOrder(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `ALTER TABLE workspaces DROP COLUMN sort_order`)
	if err != nil {
		return fmt.Errorf("remove ordering: %w", err)
	}
	return nil
}

func upClearInactiveHostID(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `UPDATE workspaces SET host_id = NULL WHERE status IN ('closed', 'archived')`); err != nil {
		return fmt.Errorf("clear inactive host_id: %w", err)
	}
	return nil
}

func downClearInactiveHostID(_ context.Context, _ *sql.Tx) error {
	return nil
}
