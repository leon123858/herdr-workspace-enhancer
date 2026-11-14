package service

import (
	"context"
	"errors"
	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/repository/herdr"
	"herdr-workspace-enhancer/repository/state"
	"testing"
	"time"
)

func TestActionWriteLimits(t *testing.T) {
	s, h := restoreFixture(t)
	counted := &failedCheckpointStore{Store: s.Store.(*state.Store)}
	s.Store = counted
	if err := s.Restore(context.Background(), "record"); err != nil {
		t.Fatal(err)
	}
	if counted.calls != 0 {
		t.Fatal("restore wrote", counted.calls)
	}
	if _, err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	counted.calls = 0
	h.store = counted.Store
	if err := s.Archive(context.Background(), "record"); err != nil {
		t.Fatal(err)
	}
	if counted.calls != 1 {
		t.Fatal("archive writes", counted.calls)
	}
	r, _ := s.Store.Get("record")
	if r.Status != model.WorkspaceArchiving {
		t.Fatal(r)
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
			want := model.WorkspaceActive
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
			if !absent && r.Status != model.WorkspaceClosed {
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
	if r.Status != model.WorkspaceActive {
		t.Fatal(r)
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
	if r.HostID != "new" || r.Status != model.WorkspaceActive || h.live[0].Label != restorePrefix+"record" {
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

func TestRestoreRetryBeforeEventAndNoCreationFailure(t *testing.T) {
	s, h := restoreFixture(t)
	h.failAt = 1
	if err := s.Restore(context.Background(), "record"); err == nil {
		t.Fatal("expected create failure")
	}
	r, _ := s.Store.Get("record")
	if r.Status != model.WorkspaceArchived || len(h.live) != 0 {
		t.Fatal(r, h.live)
	}
	h.failAt = 0
	if err := s.Restore(context.Background(), "record"); err != nil {
		t.Fatal(err)
	}
	calls := h.calls
	// No event has run: the next action must discover and bind the existing result.
	if err := s.Restore(context.Background(), "record"); err == nil {
		t.Fatal("expected already-exists result")
	}
	if h.calls != calls+1 || len(h.live) != 1 {
		t.Fatal("duplicate creation", h.calls, h.live)
	}
	r, _ = s.Store.Get("record")
	if r.Status != model.WorkspaceActive || r.HostID != "new" {
		t.Fatal(r)
	}
}
