package service

import (
	"context"
	"errors"
	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/model/layout"
	"herdr-workspace-enhancer/repository/herdr"
	"testing"
	"time"
)

func TestArchiveCloseFailureRemainsArchiving(t *testing.T) {
	store := serviceStore(t)
	host := &fakeHost{store: store, live: []herdr.Workspace{{ID: "w1"}}, fail: true}
	s := Service{Store: store, Host: host}
	rows, _ := s.Refresh(context.Background())
	if e := s.Archive(context.Background(), rows[0].ID); e == nil {
		t.Fatal("expected close failure")
	}
	r, e := store.Get(rows[0].ID)
	if e != nil || r.Status != model.WorkspaceArchiving || r.Snapshot == nil {
		t.Fatal(r, e)
	}
}

func TestArchiveDoesNotCloseBeforeCommit(t *testing.T) {
	store := serviceStore(t)
	host := &fakeHost{store: store, live: []herdr.Workspace{{ID: "w1"}}}
	s := Service{Store: store, Host: host}
	rows, _ := s.Refresh(context.Background())
	failing := &failedCheckpointStore{Store: store, fail: true}
	s.Store = failing
	if e := s.Archive(context.Background(), rows[0].ID); e == nil || host.closed {
		t.Fatal("closed before commit", e)
	}
	r, _ := store.Get(rows[0].ID)
	if r.Status != model.WorkspaceActive || r.Snapshot != nil {
		t.Fatal(r)
	}
}

func TestArchiveCanWriteNotesDuringHerdrCallAndRecoverLostScan(t *testing.T) {
	store := serviceStore(t)
	host := &fakeHost{store: store, live: []herdr.Workspace{{ID: "w1"}}}
	s := Service{Store: store, Host: host}
	rows, _ := s.Refresh(context.Background())
	id := rows[0].ID
	host.closeHook = func() {
		if _, e := store.Save(id, "edited during close"); e != nil {
			t.Error(e)
		}
		host.scanErr = errors.New("lost response")
	}
	if e := s.Archive(context.Background(), id); e != nil {
		t.Fatal(e)
	}
	r, _ := store.Get(id)
	if r.Status != model.WorkspaceArchiving || r.Note != "edited during close" {
		t.Fatal(r)
	}
	host.scanErr = nil
	rows, e := s.Refresh(context.Background())
	if e != nil || len(rows) != 1 || rows[0].Status != model.WorkspaceArchived {
		t.Fatal(rows, e)
	}
}

func TestArchiveConfirmedRejectsChanges(t *testing.T) {
	for _, change := range []string{"tab-added", "pane-replaced", "host-replaced", "unchanged"} {
		t.Run(change, func(t *testing.T) {
			store := serviceStore(t)
			original := herdr.Snapshot{Label: "project", Tabs: []herdr.SavedTab{{Label: "tab", Root: &layout.Node{Type: layout.NodePane, CWD: t.TempDir(), SourceTerminalID: "term"}}}}
			host := &fakeHost{store: store, live: []herdr.Workspace{{ID: "w1", Label: "project"}}, snapshot: &original}
			s := Service{Store: store, Host: host}
			rows, _ := s.Refresh(context.Background())
			id := rows[0].ID
			preview, e := s.Preview(context.Background(), id)
			if e != nil {
				t.Fatal(e)
			}
			updated := original
			updated.Tabs = append([]herdr.SavedTab(nil), original.Tabs...)
			switch change {
			case "tab-added":
				updated.Tabs = append(updated.Tabs, herdr.SavedTab{Label: "extra"})
			case "pane-replaced":
				leaf := *updated.Tabs[0].Root
				leaf.SourceTerminalID = "other"
				updated.Tabs[0].Root = &leaf
			case "host-replaced":
				r, _ := store.Get(id)
				r.HostID = "w2"
				saveRecord(t, store, r)
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
				t.Fatal("stale preview accepted", e)
			}
		})
	}
}

func TestArchiveFailureAndDelayedClose(t *testing.T) {
	for _, absent := range []bool{false, true} {
		t.Run(map[bool]string{false: "late-close", true: "lost-reply"}[absent], func(t *testing.T) {
			store := serviceStore(t)
			saveRecord(t, store, model.Record{ID: "r", HostID: "h", Status: model.WorkspaceActive})
			host := &fakeHost{live: []herdr.Workspace{{ID: "h"}}, fail: true}
			host.closeHook = func() {
				if absent {
					host.live = []herdr.Workspace{}
				}
			}
			svc := Service{Store: store, Host: host}
			if err := svc.Archive(context.Background(), "r"); err == nil {
				t.Fatal("lost original failure")
			}
			r, _ := store.Get("r")
			want := model.WorkspaceArchiving
			if absent {
				want = model.WorkspaceArchived
			}
			if r.Status != want {
				t.Fatal(r, want)
			}
			host.live = []herdr.Workspace{}
			if _, err := svc.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}
			r, _ = store.Get("r")
			if !absent && r.Status != model.WorkspaceArchived {
				t.Fatal(r)
			}
		})
	}
}

func TestRepairUsesIndependentContext(t *testing.T) {
	store := serviceStore(t)
	saveRecord(t, store, model.Record{ID: "r", HostID: "h", Status: model.WorkspaceActive})
	ctx, cancel := context.WithCancel(context.Background())
	host := &fakeHost{live: []herdr.Workspace{{ID: "h"}}, fail: true, closeHook: cancel}
	svc := Service{Store: store, Host: host}
	err := svc.Archive(ctx, "r")
	if err == nil || errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	r, _ := store.Get("r")
	if r.Status != model.WorkspaceArchiving {
		t.Fatal(r)
	}
}

func TestArchiveEventWaitsUntilCloseCompletes(t *testing.T) {
	store := serviceStore(t)
	saveRecord(t, store, model.Record{ID: "r", HostID: "h", Status: model.WorkspaceActive})
	host := &fakeHost{live: []herdr.Workspace{{ID: "h"}}}
	svc := Service{Store: store, Host: host}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	host.closeHook = func() {
		go func() { _, err := svc.Refresh(ctx); done <- err }()
		select {
		case err := <-done:
			t.Errorf("sync completed before close: %v", err)
		case <-time.After(40 * time.Millisecond):
		}
		r, _ := store.Get("r")
		if r.Status != model.WorkspaceArchiving {
			t.Error("premature transition", r)
		}
	}
	if err := svc.Archive(ctx, "r"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("event deadlocked")
	}
	r, _ := store.Get("r")
	if r.Status != model.WorkspaceArchived {
		t.Fatal(r)
	}
}
