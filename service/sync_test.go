package service

import (
	"context"
	"errors"
	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/model/layout"
	"herdr-workspace-enhancer/repository/herdr"
	"herdr-workspace-enhancer/repository/state"
	"testing"
)

func TestSyncLifecycleAndExplicitArchive(t *testing.T) {
	store := serviceStore(t)
	host := &fakeHost{store: store, live: []herdr.Workspace{{ID: "w1", Label: "Project"}}}
	s := Service{Store: store, Host: host}
	ctx := context.Background()
	rows, e := s.Refresh(ctx)
	if e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	id := rows[0].ID
	store.Save(id, "keep note")
	host.live[0].Label = "Renamed"
	rows, e = s.Refresh(ctx)
	if e != nil || rows[0].Label != "Renamed" {
		t.Fatal(rows, e)
	}
	host.live = []herdr.Workspace{}
	rows, e = s.Refresh(ctx)
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
	r, e := store.Get(id)
	if e != nil || r.Status != model.WorkspaceClosed || r.Note != "keep note" || r.HostID != "w1" {
		t.Fatal(r, e)
	}
	host.live = []herdr.Workspace{{ID: "w1", Label: "Back"}}
	rows, e = s.Refresh(ctx)
	if e != nil || len(rows) != 1 || rows[0].ID != id || rows[0].Status != model.WorkspaceActive {
		t.Fatal(rows, e)
	}
	if e = s.Archive(ctx, id); e != nil {
		t.Fatal(e)
	}
	rows, e = s.Refresh(ctx)
	if e != nil || len(rows) != 1 || rows[0].Status != model.WorkspaceArchived {
		t.Fatal("sync changed explicit archive", rows, e)
	}
	host.live = []herdr.Workspace{{ID: "w1", Label: "Returns"}}
	rows, e = s.Refresh(ctx)
	if e == nil {
		t.Fatal("empty snapshot accepted", rows)
	}
	r, e = store.Get(id)
	if e != nil || r.Status != model.WorkspaceArchived || r.Note != "keep note" {
		t.Fatal(r, e)
	}
}

func TestSyncInvalidScanNeverChangesState(t *testing.T) {
	for _, kind := range []string{"error", "nil"} {
		t.Run(kind, func(t *testing.T) {
			store := serviceStore(t)
			r := model.Record{ID: "one", HostID: "w1", Label: "Original", Note: "note", Status: model.WorkspaceActive}
			saveRecord(t, store, r)
			host := &fakeHost{live: []herdr.Workspace{}}
			switch kind {
			case "error":
				host.scanErr = errors.New("session unavailable")
			case "nil":
				host.live = nil
			case "duplicate":
				host.live = []herdr.Workspace{{ID: "w2"}, {ID: "w2"}}
			case "empty-id":
				host.live = []herdr.Workspace{{ID: "w2"}, {ID: ""}}
			}
			if _, e := (&Service{Store: store, Host: host}).Refresh(context.Background()); e == nil {
				t.Fatal("invalid scan accepted")
			}
			got, e := store.Get("one")
			if e != nil || got.Status != model.WorkspaceActive || got.Label != "Original" || got.Note != "note" {
				t.Fatal(got, e)
			}
			rows, _ := store.ListSortedRecords(context.Background())
			if len(rows) != 1 {
				t.Fatal("scan partly committed", rows)
			}
		})
	}
}

func TestManagersAreExcludedByFlag(t *testing.T) {
	store := serviceStore(t)
	saveRecord(t, store, model.Record{ID: "manager", HostID: "manager-host", ManagerState: model.ManagerDone, Status: model.WorkspaceActive})
	host := &fakeHost{live: []herdr.Workspace{{ID: "manager-host"}, {ID: "project"}}}
	s := Service{Store: store, Host: host}
	rows, e := s.Refresh(context.Background())
	if e != nil || len(rows) != 1 || rows[0].HostID != "project" {
		t.Fatal(rows, e)
	}
	if e = s.Archive(context.Background(), "manager"); e == nil {
		t.Fatal("manager archive allowed")
	}
	if _, e = s.Preview(context.Background(), "manager"); e == nil {
		t.Fatal("manager preview allowed")
	}
}

func TestSyncRebindsLostCreationWithoutLosingNote(t *testing.T) {
	s, h := restoreFixture(t)
	h.live = []herdr.Workspace{{ID: "new", Label: restorePrefix + "record"}, {ID: "other", Label: restorePrefix + "record"}}
	items, err := s.syncItems(context.Background(), h.live)
	if err != nil || len(items) != 2 || items[0].record == nil || items[1].workspace == nil || items[1].record != nil {
		t.Fatal("duplicate label lost workspace", items, err)
	}

	h.live = h.live[:1]
	rows, err := s.Refresh(context.Background())
	if err != nil || len(rows) != 1 || rows[0].ID != "record" || rows[0].Note != "keep note" {
		t.Fatal(rows, err)
	}
}

func TestSyncRepairsFailedArchiveReset(t *testing.T) {
	store := serviceStore(t)
	host := &fakeHost{store: store, live: []herdr.Workspace{{ID: "w1"}}, fail: true}
	s := Service{Store: store, Host: host}
	rows, _ := s.Refresh(context.Background())
	id := rows[0].ID
	s.Store = &failedCheckpointStore{Store: store, failAt: 2}
	if e := s.Archive(context.Background(), id); e == nil {
		t.Fatal("expected failed close/reset")
	}
	r, _ := store.Get(id)
	if r.Status != model.WorkspaceArchiving {
		t.Fatal(r)
	}
	host.fail = false
	if _, e := s.Refresh(context.Background()); e != nil {
		// Close succeeded but the injected DB commit failed; retry from noHost.
		if _, err := s.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	r, _ = store.Get(id)
	if r.Status != model.WorkspaceArchived {
		t.Fatal("sync did not repair failed reset", r)
	}
}

func TestSyncWriteFailureRollsBackTheWholeScan(t *testing.T) {
	store := serviceStore(t)
	saveRecord(t, store, model.Record{ID: "one", HostID: "old", Status: model.WorkspaceActive, Note: "keep"})
	host := &fakeHost{live: []herdr.Workspace{{ID: "new", Label: "New"}}}
	s := Service{Store: failingSyncStore{store}, Host: host}
	if _, e := s.Refresh(context.Background()); e == nil {
		t.Fatal("expected SQL failure")
	}
	rows, e := store.ListSortedRecords(context.Background())
	if e != nil || len(rows) != 1 || rows[0].HostID != "old" || rows[0].Status != model.WorkspaceActive || rows[0].Note != "keep" {
		t.Fatal("partial sync committed", rows, e)
	}
}

func TestSyncOrdinaryLabelsWithInternalPrefixes(t *testing.T) {
	for _, label := range []string{"enhancer-restore-demo", "enhancer-manager-demo"} {
		t.Run(label, func(t *testing.T) {
			store := serviceStore(t)
			saveRecord(t, store, model.Record{ID: "record", HostID: "host", Label: "Original", Status: model.WorkspaceActive, Note: "keep"})
			host := &restoreHost{fakeHost: fakeHost{live: []herdr.Workspace{{ID: "host", Label: label}}}}
			svc := Service{Store: store, Host: host}
			for i := 0; i < 2; i++ {
				rows, err := svc.Refresh(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if len(rows) != 1 || rows[0].Label != label || rows[0].ID != "record" || rows[0].Note != "keep" {
					t.Fatalf("sync %d: ordinary rename was not preserved: %+v", i, rows)
				}
				if host.calls != 0 || host.live[0].Label != label {
					t.Fatal("ordinary name triggered cleanup", host.calls, host.live)
				}
			}
		})
	}
}

func TestSyncBindingBeforeRenameAndRetryCleanup(t *testing.T) {
	s, h := restoreFixture(t)
	h.live = []herdr.Workspace{{ID: "new", Label: restorePrefix + "record"}}
	real := s.Store.(*state.Store)
	s.Store = failingSyncStore{real}
	if _, err := s.Refresh(context.Background()); err == nil {
		t.Fatal("expected write failure")
	}
	if h.calls != 0 {
		t.Fatal("renamed before commit")
	}
	s.Store = real
	h.failAt = 1
	if _, err := s.Refresh(context.Background()); err == nil {
		t.Fatal("expected rename failure")
	}
	r, _ := real.Get("record")
	if r.HostID != "new" || r.Status != model.WorkspaceArchived || h.live[0].Label != restorePrefix+"record" {
		t.Fatal(r, h.live)
	}
	h.failAt = 0
	if _, err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.live[0].Label != "project" {
		t.Fatal(h.live)
	}
	calls := h.calls
	if _, err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.calls != calls {
		t.Fatal("redundant rename")
	}
}

func TestSyncRepairsFailedNewIDCommit(t *testing.T) {
	s, _ := restoreFixture(t)
	real := s.Store.(*state.Store)
	s.Store = &failedCheckpointStore{Store: real, failAt: 1}
	if e := s.Restore(context.Background(), "record"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Refresh(context.Background()); e == nil {
		t.Fatal("expected failed sync binding")
	}
	r, _ := real.Get("record")
	if r.Status != model.WorkspaceArchived || r.HostID != "old" {
		t.Fatal("failed binding committed", r)
	}
	s.Store = real
	rows, e := s.Refresh(context.Background())
	if e != nil || len(rows) != 1 || rows[0].ID != "record" || rows[0].HostID != "new" || rows[0].Note != "keep note" {
		t.Fatal(rows, e)
	}
}

func TestSyncFourStateMatrix(t *testing.T) {
	for _, status := range []model.WorkspaceStatus{model.WorkspaceActive, model.WorkspaceArchiving, model.WorkspaceArchived, model.WorkspaceClosed} {
		for _, exists := range []bool{false, true} {
			name := string(status) + "/absent"
			if exists {
				name = string(status) + "/present"
			}
			t.Run(name, func(t *testing.T) {
				store := serviceStore(t)
				saveRecord(t, store, model.Record{ID: "stable", HostID: "host", Status: status, Note: "retained", Snapshot: &herdr.Snapshot{Tabs: []herdr.SavedTab{{Root: &layout.Node{Type: layout.NodePane, CWD: t.TempDir()}}}}})
				live := []herdr.Workspace{}
				if exists {
					live = append(live, herdr.Workspace{ID: "host", Label: "project"})
				}
				svc := Service{Store: store, Host: &restoreHost{fakeHost: fakeHost{live: live}}}
				if _, err := svc.Refresh(context.Background()); err != nil {
					t.Fatal(err)
				}
				want := status
				if exists && status != model.WorkspaceArchiving {
					want = model.WorkspaceActive
				} else if status == model.WorkspaceActive {
					want = model.WorkspaceClosed
				} else if status == model.WorkspaceArchiving {
					want = model.WorkspaceArchived
				}
				got, err := store.Get("stable")
				if err != nil || got.Status != want || got.Note != "retained" {
					t.Fatal(got, err, "want", want)
				}
			})
		}
	}
}
