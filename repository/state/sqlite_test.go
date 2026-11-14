package state

import (
	"context"
	"herdr-workspace-enhancer/model"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestSingleTableAutoMigrationAndReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "spaces # 中文")
	s := testStore(t, root)
	put(t, s, Record{ID: "one", HostID: "h1", Status: model.WorkspaceActive, Note: "persistent"})
	var tables []string
	if e := s.db.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables).Error; e != nil || len(tables) != 1 || tables[0] != "workspaces" {
		t.Fatal(tables, e)
	}
	other := testStore(t, root)
	n, e := other.Load("one")
	if e != nil || n.Text != "persistent" {
		t.Fatal(n, e)
	}
}
func TestConcurrentNotesAndMetadataUpdates(t *testing.T) {
	root := t.TempDir()
	a, b := testStore(t, root), testStore(t, root)
	put(t, a, Record{ID: "one", HostID: "h1", Status: model.WorkspaceActive})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 25; i++ {
			if _, e := b.Save("one", "edited"); e != nil {
				t.Error(e)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 25; i++ {
			r, e := a.Get("one")
			if e != nil {
				t.Error(e)
				return
			}
			r.Label = "renamed"
			if e := a.ApplySync(context.Background(), []Record{r}); e != nil {
				t.Error(e)
				return
			}
		}
	}()
	wg.Wait()
	r, e := a.Get("one")
	if e != nil || r.Note != "edited" || r.Label != "renamed" {
		t.Fatal(r, e)
	}
}
func TestNoLegacyJSONImport(t *testing.T) {
	root := t.TempDir()
	s := testStore(t, root)
	path := filepath.Join(s.Directory(), "workspaces.json")
	if e := os.WriteFile(path, []byte("old JSON is not parsed"), 0600); e != nil {
		t.Fatal(e)
	}
	other := testStore(t, root)
	rows, e := other.List()
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
}
