package state

import (
	"context"
	"database/sql"

	"path/filepath"

	"sync"
	"testing"

	"herdr-workspace-enhancer/repository/state/migrations"
)

const legacyDDL = `CREATE TABLE workspaces (id text, host_id text, label text, status text NOT NULL DEFAULT "closed", note text NOT NULL DEFAULT "", manager_state text NOT NULL DEFAULT "NONE", snapshot text, PRIMARY KEY (id))`

func fixture(t *testing.T, ddl string) (string, *Store) {
	t.Helper()
	root := t.TempDir()
	s, err := openDatabase(root, "test", false, func(s *Store) error { return s.db.Exec(ddl).Error })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return root, s
}
func TestMigrationTargetAndConcurrentInitialization(t *testing.T) {
	var max int64
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	provider, err := migrations.Provider(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range provider.ListSources() {
		if m.Version > max {
			max = m.Version
		}
	}
	if max != CurrentDBVersion {
		t.Fatal(max)
	}
	root := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := Open(root, "test")
			if err != nil {
				t.Error(err)
				return
			}
			s.Close()
		}()
	}
	wg.Wait()
	if v, err := DBVersion(root, "test"); err != nil || v != 1 {
		t.Fatal(v, err)
	}
}

func TestRejectExistingUnmanagedDB(t *testing.T) {
	for _, ddl := range []string{legacyDDL, "SELECT 1"} {
		root, s := fixture(t, ddl)
		if _, err := Open(root, "test"); err == nil {
			t.Fatal("accepted unversioned existing DB")
		}
		if _, err := MigrateDB(root, "test", 1); err == nil {
			t.Fatal("maintenance adopted DB")
		}
		if _, err := DBVersion(root, "test"); err == nil {
			t.Fatal("reported unversioned DB as v0")
		}
		var n int
		s.db.Raw(`SELECT count(*) FROM sqlite_master WHERE name='goose_db_version'`).Scan(&n)
		if n != 0 {
			t.Fatal("created Goose metadata")
		}
	}
}
func TestManagedRoundTripBackupAndRollback(t *testing.T) {
	root := t.TempDir()
	if _, err := MigrateDB(root, "test", 0); err != nil {
		t.Fatal(err)
	}
	if v, err := DBVersion(root, "test"); err != nil || v != 0 {
		t.Fatal(v, err)
	}
	s, err := Open(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Save("a", "中文"); err != nil {
		t.Fatal(err)
	}
	backup, err := MigrateDB(root, "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := sql.Open("sqlite", backup)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if v, err := databaseVersion(context.Background(), b); err != nil || v != 1 {
		t.Fatal(v, err)
	}
	var note string
	if err := b.QueryRow(`SELECT note FROM workspaces WHERE id='a'`).Scan(&note); err != nil || note != "中文" {
		t.Fatal(note, err)
	}
	// Fail version registration after schema changes to verify transactional DDL.
	if err := s.db.Exec(`CREATE TRIGGER fail_version BEFORE INSERT ON goose_db_version WHEN NEW.version_id = 1 BEGIN SELECT RAISE(ABORT,'failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateDB(root, "test", 1); err == nil {
		t.Fatal("expected migration failure")
	}
	if v, err := DBVersion(root, "test"); err != nil || v != 0 {
		t.Fatal(v, err)
	}
	var n int
	s.db.Raw(`SELECT count(*) FROM pragma_table_info('workspaces') WHERE name='sort_order'`).Scan(&n)
	if n != 0 {
		t.Fatal("partial schema migration")
	}
	if err := s.db.Exec(`DROP TRIGGER fail_version`).Error; err != nil {
		t.Fatal(err)
	}
	dir := s.dir
	s.dir = filepath.Join(dir, "missing", "dir")
	if _, err := s.migrate(context.Background(), 1); err == nil {
		t.Fatal("backup failure ignored")
	}
	s.dir = dir
	reopened, err := Open(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	reopened.Close()
	if note, err := s.Load("a"); err != nil || note.Text != "中文" {
		t.Fatal(note, err)
	}
	var order *int
	s.db.Raw(`SELECT sort_order FROM workspaces WHERE id='a'`).Scan(&order)
	if order != nil {
		t.Fatal(order)
	}
	s.db.Raw(`SELECT count(*) FROM sqlite_master WHERE type='trigger'`).Scan(&n)
	if n != 0 {
		t.Fatal("unexpected migration trigger")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "state.backup-*.sqlite"))
	reopened, err = Open(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	reopened.Close()
	again, _ := filepath.Glob(filepath.Join(dir, "state.backup-*.sqlite"))
	if len(files) != len(again) {
		t.Fatal("reopen migrated again")
	}
	s.db.Exec(`INSERT INTO goose_db_version(version_id,is_applied) VALUES(2,1)`)
	if _, err := Open(root, "test"); err == nil {
		t.Fatal("accepted newer DB")
	}
	s.db.Exec(`DROP TABLE goose_db_version`)
	s.db.Exec(`CREATE TABLE goose_db_version(bad text)`)
	if _, err := Open(root, "test"); err == nil {
		t.Fatal("ignored invalid version metadata")
	}
}
