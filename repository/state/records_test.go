package state

import (
	"context"
	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/model/layout"
	"math"
	"testing"
)

func testStore(t *testing.T, root string) *Store {
	t.Helper()
	s, e := Open(root, "test")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func put(t *testing.T, s *Store, r Record) {
	t.Helper()
	if e := s.ApplySync(context.Background(), []model.Record{r}); e != nil {
		t.Fatal(e)
	}
}
func TestMetadataPreservesNoteAndRejectsInvalidSnapshot(t *testing.T) {
	s := testStore(t, t.TempDir())
	r := Record{ID: "one", HostID: "h1", Status: model.WorkspaceActive, Label: "Original", Note: "note"}
	put(t, s, r)
	r.Label = "changed"
	if _, e := s.Save("one", "edited"); e != nil {
		t.Fatal(e)
	}
	r.Note = "stale note"
	put(t, s, r)
	got, e := s.Get("one")
	if e != nil || got.Note != "edited" || got.Label != "changed" {
		t.Fatal("metadata update overwrote note", got, e)
	}
	r.Snapshot = &model.Snapshot{Tabs: []model.SavedTab{{Root: &layout.Node{Type: layout.NodeSplit, Ratio: math.NaN()}}}}
	if e := s.ApplySync(context.Background(), []model.Record{r}); e == nil {
		t.Fatal("invalid snapshot committed")
	}
	got, e = s.Get("one")
	if e != nil || got.Snapshot != nil || got.Note != "edited" {
		t.Fatal("invalid snapshot changed data", got, e)
	}
}
func TestListExcludesManagersAndClosedButKeepsTheirNotes(t *testing.T) {
	s := testStore(t, t.TempDir())
	for _, r := range []Record{{ID: "active", Status: model.WorkspaceActive}, {ID: "archived", Status: model.WorkspaceArchived}, {ID: "closed", Status: model.WorkspaceClosed, Note: "keep"}, {ID: "manager", HostID: "manager-host", Status: model.WorkspaceActive, IsManager: true}} {
		put(t, s, r)
	}
	rows, e := s.List()
	if e != nil || len(rows) != 2 {
		t.Fatal(rows, e)
	}
	manager, e := s.IsManager("manager-host")
	if e != nil || !manager {
		t.Fatal(manager, e)
	}
	n, e := s.Load("closed")
	if e != nil || n.Text != "keep" {
		t.Fatal(n, e)
	}
}
