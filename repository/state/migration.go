package state

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"herdr-workspace-enhancer/repository/state/migrations"
)

// Main DB version, incremented for each schema change. See migrations.Provider.
const CurrentDBVersion int64 = 1

// databaseVersion deliberately does not call Provider.GetDBVersion, which can
// initialize Goose's table. Existing databases must already be managed.
func databaseVersion(ctx context.Context, db *sql.DB) (int64, error) {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name='goose_db_version'`).Scan(&n); err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, fmt.Errorf("database has no goose_db_version; pre-release users must manually run go run ./scripts/pre-release-db-add-to-goose-v0.go --db /path/to/state.sqlite")
	}
	var version int64
	err := db.QueryRowContext(ctx, `SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1`).Scan(&version)
	return version, err
}

// DBVersion uses a read-only SQLite connection and never initializes Goose.
func DBVersion(root, session string) (version int64, err error) {
	s, err := openDatabase(root, session, true, func(s *Store) error {
		db, e := s.db.DB()
		if e != nil {
			return e
		}
		version, e = databaseVersion(context.Background(), db)
		return e
	})
	if err != nil {
		return 0, err
	}
	return version, s.Close()
}

// MigrateDB is a maintenance entry point. Stop other session processes first.
func MigrateDB(root, session string, target int64) (backup string, err error) {
	if target < 0 || target > CurrentDBVersion {
		return "", fmt.Errorf("unsupported target database version %d", target)
	}
	s, err := openDatabase(root, session, false, func(s *Store) error { var e error; backup, e = s.migrate(context.Background(), target); return e })
	if err != nil {
		return backup, err
	}
	return backup, s.Close()
}
func (s *Store) migrate(ctx context.Context, target int64) (string, error) {
	db, err := s.db.DB()
	if err != nil {
		return "", err
	}
	provider, err := migrations.Provider(db)
	if err != nil {
		return "", err
	}
	if s.newDB {
		if _, err = provider.GetDBVersion(ctx); err != nil {
			return "", err
		}
		s.newDB = false
	}
	version, err := databaseVersion(ctx, db)
	if err != nil {
		return "", fmt.Errorf("read database version: %w", err)
	}
	if version > CurrentDBVersion {
		return "", fmt.Errorf("database version %d is newer than supported version %d; use a newer application", version, CurrentDBVersion)
	}
	if version == target {
		return "", nil
	}
	backup, err := backupDatabase(ctx, db, s.dir)
	if err != nil {
		return "", fmt.Errorf("backup before migration %d -> %d: %w", version, target, err)
	}

	if version < target {
		_, err = provider.UpTo(ctx, target)
	} else {
		_, err = provider.DownTo(ctx, target)
	}
	if err != nil {
		return backup, fmt.Errorf("migrate database %d -> %d (backup %q): %w", version, target, backup, err)
	}
	return backup, nil
}
func backupDatabase(ctx context.Context, db *sql.DB, dir string) (string, error) {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name != 'goose_db_version'`).Scan(&count); err != nil {
		return "", err
	}
	if count == 0 {
		return "", nil
	}
	// Reserve a unique, private empty file; SQLite VACUUM INTO accepts empty files.
	f, err := os.CreateTemp(dir, "state.backup-*.sqlite")
	if err != nil {
		return "", err
	}
	path := filepath.Clean(f.Name())
	if err = f.Close(); err != nil {
		return "", err
	}
	if _, err = db.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}
