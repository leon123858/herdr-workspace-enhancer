// pre-release-db-add-to-goose-v0 is an explicit, one-time pre-release database conversion tool.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	_ "github.com/glebarez/go-sqlite"
	"github.com/gofrs/flock"
	"github.com/pressly/goose/v3/database"
)

func main() {
	path := flag.String("db", "", "Existing pre-release state.sqlite; stop all session processes first")
	flag.Parse()
	if *path == "" || flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := adopt(*path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func adopt(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular database file")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	lock := flock.New(filepath.Join(filepath.Dir(path), "schema.lock"))
	defer lock.Close()
	ok, err := lock.TryLockContext(ctx, 25*time.Millisecond)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("schema lock unavailable")
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	if len(u.Path) > 1 && u.Path[1] == ':' {
		u.Path = "/" + u.Path
	}
	q := url.Values{"mode": {"rw"}, "_txlock": {"immediate"}, "_pragma": {"busy_timeout(5000)", "foreign_keys(1)"}}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var n int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name='goose_db_version'`).Scan(&n); err != nil {
		return err
	}
	if n != 0 {
		return fmt.Errorf("database already has Goose metadata; refusing to reset its version")
	}
	backup, err := os.CreateTemp(filepath.Dir(path), "state.pre-v0-*.sqlite")
	if err != nil {
		return err
	}
	if err = backup.Close(); err != nil {
		return err
	}
	if _, err = db.ExecContext(ctx, "VACUUM INTO ?", backup.Name()); err != nil {
		os.Remove(backup.Name())
		return err
	}
	fmt.Println("Backup:", backup.Name())
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = normalizeV0(ctx, tx); err != nil {
		return err
	}
	store, err := database.NewStore(database.DialectSQLite3, "goose_db_version")
	if err != nil {
		return err
	}
	if err = store.CreateVersionTable(ctx, tx); err != nil {
		return err
	}
	if err = store.Insert(ctx, tx, database.InsertRequest{Version: 0}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	fmt.Println("Database is now Goose v0. Custom ordering was removed; normal startup will migrate to v1.")
	return nil
}

const baseColumns = `id text, host_id text, label text, status text NOT NULL DEFAULT "closed", note text NOT NULL DEFAULT "", manager_state text NOT NULL DEFAULT "NONE", snapshot text, PRIMARY KEY (id)`
const trigger = `CREATE TRIGGER workspace_append_order AFTER INSERT ON workspaces WHEN NEW.sort_order IS NULL BEGIN UPDATE workspaces SET sort_order = (SELECT COALESCE(MAX(sort_order), -1) + 1 FROM workspaces) WHERE id = NEW.id; END`

type column struct {
	Type                string
	NotNull, PK, Hidden int
	Default             sql.NullString
}

func columns(ctx context.Context, tx *sql.Tx, table string) (map[string]column, error) {
	rows, err := tx.QueryContext(ctx, "PRAGMA table_xinfo("+table+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]column{}
	for rows.Next() {
		var cid int
		var name string
		var c column
		if err := rows.Scan(&cid, &name, &c.Type, &c.NotNull, &c.Default, &c.PK, &c.Hidden); err != nil {
			return nil, err
		}
		c.Type = strings.ToLower(c.Type)
		if c.Default.Valid {
			c.Default.String = strings.Trim(c.Default.String, "\"'")
		}
		result[name] = c
	}
	return result, rows.Err()
}
func normalized(s string) string {
	s = strings.NewReplacer("`", "", "\"", "", "[", "", "]", "", "IF NOT EXISTS", "").Replace(s)
	return strings.ToLower(strings.Join(strings.Fields(s), ""))
}
func ensureObject(ctx context.Context, tx *sql.Tx, kind, name, ddl string) error {
	var got string
	err := tx.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name = ?", name).Scan(&got)
	if err == sql.ErrNoRows {
		_, err = tx.ExecContext(ctx, ddl)
		return err
	}
	if err != nil {
		return err
	}
	if normalized(got) != normalized(ddl) {
		return fmt.Errorf("incompatible %s %s", kind, name)
	}
	return nil
}
func normalizeV0(ctx context.Context, tx *sql.Tx) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name='workspaces'`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("missing legacy workspaces table")
	}
	// Compare against frozen v0 metadata, independently of current GORM models.
	if _, err := tx.ExecContext(ctx, "CREATE TEMP TABLE migration_v0_reference ("+baseColumns+")"); err != nil {
		return err
	}
	expected, err := columns(ctx, tx, "migration_v0_reference")
	if err != nil {
		return err
	}
	actual, err := columns(ctx, tx, "workspaces")
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DROP TABLE migration_v0_reference"); err != nil {
		return err
	}
	order, hasOrder := actual["sort_order"]
	delete(actual, "sort_order")
	if !reflect.DeepEqual(expected, actual) || (hasOrder && !reflect.DeepEqual(order, column{Type: "integer"})) {
		return fmt.Errorf("incompatible workspaces columns")
	}
	// Reject additional table constraints (including WITHOUT ROWID) rather than silently adopting them.
	var tableSQL string
	if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name='workspaces'`).Scan(&tableSQL); err != nil {
		return err
	}
	for _, keyword := range []string{"check", "references", "unique", "withoutrowid", "generated", "collate"} {
		if strings.Contains(normalized(tableSQL), keyword) {
			return fmt.Errorf("unsupported workspaces constraint: %s", keyword)
		}
	}

	for _, idx := range []struct{ name, ddl string }{
		{"idx_workspaces_host_id", `CREATE UNIQUE INDEX idx_workspaces_host_id ON workspaces(host_id)`},
		{"idx_workspaces_status", `CREATE INDEX idx_workspaces_status ON workspaces(status)`},
		{"idx_workspaces_manager_state", `CREATE INDEX idx_workspaces_manager_state ON workspaces(manager_state)`},
	} {
		if err := ensureObject(ctx, tx, "index", idx.name, idx.ddl); err != nil {
			return err
		}
	}
	var unexpectedTriggers int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='trigger' AND tbl_name='workspaces' AND (name != 'workspace_append_order' OR ? = 0)`, hasOrder).Scan(&unexpectedTriggers); err != nil {
		return err
	}
	if unexpectedTriggers != 0 {
		return fmt.Errorf("unsupported legacy workspace triggers")
	}
	if hasOrder {
		if err := ensureObject(ctx, tx, "trigger", "workspace_append_order", trigger); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DROP TRIGGER workspace_append_order"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "ALTER TABLE workspaces DROP COLUMN sort_order"); err != nil {
			return err
		}
	}
	return nil
}
