package workspace

import (
	"context"
	"errors"
	"herdr-workspace-enhancer/internal/herdr"
	"herdr-workspace-enhancer/internal/layout"
	"herdr-workspace-enhancer/internal/state"
	"os"
	"path/filepath"
	"testing"
)

type fakeHost struct {
	live         []herdr.Workspace
	store        *state.Store
	fail         bool
	closed       bool
	snapshotHook func()
	snapshot     *herdr.Snapshot
}

func (f *fakeHost) Workspaces(context.Context) ([]herdr.Workspace, error) { return f.live, nil }
func (f *fakeHost) Snapshot(context.Context, herdr.Workspace) (herdr.Snapshot, error) {
	if f.snapshotHook != nil {
		f.snapshotHook()
	}
	if f.snapshot != nil {
		return *f.snapshot, nil
	}
	return herdr.Snapshot{Label: "project"}, nil
}
func (f *fakeHost) Close(context.Context, string) error {
	db, e := f.store.ReadDatabase()
	if e != nil {
		return e
	}
	if db.Records[0].Snapshot == nil || db.Records[0].Status != "archiving" {
		return errors.New("snapshot not durable")
	}
	if f.fail {
		return errors.New("close refused")
	}
	f.closed = true
	f.live = nil
	return nil
}
func TestArchive(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "refused"}[fail], func(t *testing.T) {
			store, e := state.Open(t.TempDir(), "test")
			if e != nil {
				t.Fatal(e)
			}
			host := &fakeHost{store: store, live: []herdr.Workspace{{ID: "w1", Label: "project"}}, fail: fail}
			s := Service{Store: store, Host: host, ManagerID: "manager"}
			records, e := s.Refresh(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			e = s.Archive(context.Background(), records[0].ID)
			if (e != nil) != fail {
				t.Fatal(e)
			}
			db, e := store.ReadDatabase()
			if e != nil {
				t.Fatal(e)
			}
			want := "archived"
			if fail {
				want = "archiving"
			}
			if db.Records[0].Status != want {
				t.Fatal(db)
			}
		})
	}
}

func TestArchiveDoesNotCloseWhenSnapshotCannotBeSaved(t *testing.T) {
	store, e := state.Open(t.TempDir(), "test")
	if e != nil {
		t.Fatal(e)
	}
	host := &fakeHost{store: store, live: []herdr.Workspace{{ID: "w1", Label: "project"}}}
	s := Service{Store: store, Host: host, ManagerID: "manager"}
	rows, e := s.Refresh(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	original := store.Dir
	bad := filepath.Join(t.TempDir(), "not-a-directory")
	if e = os.WriteFile(bad, []byte("blocked"), 0600); e != nil {
		t.Fatal(e)
	}
	host.snapshotHook = func() { store.Dir = bad }
	e = s.Archive(context.Background(), rows[0].ID)
	store.Dir = original
	if e == nil || host.closed {
		t.Fatal("closed despite persistence failure", e)
	}
	db, e := store.ReadDatabase()
	if e != nil || db.Records[0].Status != "active" {
		t.Fatal(db, e)
	}
}

func TestRefreshReattachesPendingWorkspaceWithoutDuplicate(t *testing.T) {
	store, e := state.Open(t.TempDir(), "test")
	if e != nil {
		t.Fatal(e)
	}
	if e = store.WriteDatabase(state.Database{Version: 1, Records: []state.Record{{ID: "stable", Label: "Project", Status: "restoring"}}}); e != nil {
		t.Fatal(e)
	}
	host := &fakeHost{live: []herdr.Workspace{{ID: "new", Label: "enhancer-restore-stable"}}}
	s := Service{Store: store, Host: host}
	rows, e := s.Refresh(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if len(rows) != 1 || rows[0].HostID != "new" || rows[0].Label != "Project" || rows[0].Status != "restoring" {
		t.Fatal(rows)
	}
}

func TestArchiveConfirmedRejectsChanges(t *testing.T) {
	for _, change := range []string{"tab-added", "pane-replaced", "host-replaced", "unchanged"} {
		t.Run(change, func(t *testing.T) {
			store, e := state.Open(t.TempDir(), "test")
			if e != nil {
				t.Fatal(e)
			}
			original := herdr.Snapshot{Label: "project", Tabs: []herdr.SavedTab{{SourceTabID: "t1", Label: "tab", Root: &layout.Node{Type: "pane", CWD: t.TempDir(), SourcePaneID: "p1", SourceTerminalID: "term1"}}}}
			host := &fakeHost{store: store, live: []herdr.Workspace{{ID: "w1", Label: "project"}}, snapshot: &original}
			s := Service{Store: store, Host: host, ManagerID: "manager"}
			rows, e := s.Refresh(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			preview, e := s.Preview(context.Background(), rows[0].ID)
			if e != nil {
				t.Fatal(e)
			}
			updated := original
			updated.Tabs = append([]herdr.SavedTab(nil), original.Tabs...)
			switch change {
			case "tab-added":
				updated.Tabs = append(updated.Tabs, herdr.SavedTab{SourceTabID: "t2"})
			case "pane-replaced":
				leaf := *updated.Tabs[0].Root
				leaf.SourceTerminalID = "different"
				updated.Tabs[0].Root = &leaf
			case "host-replaced":
				db, _ := store.ReadDatabase()
				db.Records[0].HostID = "w2"
				if e = store.WriteDatabase(db); e != nil {
					t.Fatal(e)
				}
			}
			host.snapshot = &updated
			e = s.ArchiveConfirmed(context.Background(), preview)
			if change == "unchanged" {
				if e != nil || !host.closed {
					t.Fatal(e)
				}
				return
			}
			if !errors.Is(e, ErrPreviewChanged) || host.closed {
				t.Fatal("stale confirmation accepted", e)
			}
			db, e := store.ReadDatabase()
			if e != nil || db.Records[0].Status != "active" || db.Records[0].Snapshot != nil {
				t.Fatal("stale preview changed database", db, e)
			}
		})
	}
}
