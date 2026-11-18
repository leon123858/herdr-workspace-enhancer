package main

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"herdr-workspace-enhancer/repository/state"
)

func TestPreReleaseConversion(t *testing.T) {
	for _, ordered := range []bool{false, true} {
		t.Run(fmt.Sprint(ordered), func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "sessions", fmt.Sprintf("%x", sha256.Sum256([]byte("test"))))
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "state.sqlite")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			exec := func(q string) {
				t.Helper()
				if _, err := db.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			exec("CREATE TABLE workspaces (" + baseColumns + ")")
			exec(`INSERT INTO workspaces(id,note,snapshot) VALUES('a','中文','{"saved":true}')`)
			if ordered {
				exec("ALTER TABLE workspaces ADD COLUMN sort_order integer")
				exec(trigger)
				exec("UPDATE workspaces SET sort_order=42")
			}
			if _, err := state.Open(root, "test"); err == nil {
				t.Fatal("application adopted legacy DB")
			}
			if err := adopt(path); err != nil {
				t.Fatal(err)
			}
			if v, err := state.DBVersion(root, "test"); err != nil || v != 0 {
				t.Fatal(v, err)
			}
			var n int
			db.QueryRow(`SELECT count(*) FROM pragma_table_info('workspaces') WHERE name='sort_order'`).Scan(&n)
			if n != 0 {
				t.Fatal("not canonical v0")
			}
			if err := adopt(path); err == nil {
				t.Fatal("reset managed database")
			}
			files, _ := filepath.Glob(filepath.Join(dir, "state.pre-v0-*.sqlite"))
			if len(files) != 1 {
				t.Fatal(files)
			}
			b, err := sql.Open("sqlite", files[0])
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			b.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='goose_db_version'`).Scan(&n)
			if n != 0 {
				t.Fatal("backup has version metadata")
			}
			var note string
			if err := b.QueryRow(`SELECT note FROM workspaces`).Scan(&note); err != nil || note != "中文" {
				t.Fatal(note, err)
			}
			s, err := state.Open(root, "test")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			var snapshot string
			if err := db.QueryRow(`SELECT note,snapshot,sort_order IS NULL FROM workspaces`).Scan(&note, &snapshot, &n); err != nil || note != "中文" || snapshot != `{"saved":true}` || n != 1 {
				t.Fatal(note, snapshot, n, err)
			}
		})
	}
}
func TestConversionFailureRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{"CREATE TABLE workspaces (" + baseColumns + ")", "CREATE INDEX idx_workspaces_status ON workspaces(label)"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := adopt(path); err == nil {
		t.Fatal("accepted incompatible index")
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name IN ('goose_db_version','idx_workspaces_host_id')`).Scan(&n)
	if n != 0 {
		t.Fatal("partial conversion")
	}
}
