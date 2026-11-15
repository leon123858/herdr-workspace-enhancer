package service

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/repository/herdr"
	"testing"
)

func TestDeleteArchived(t *testing.T) {
	for _, live := range []string{"absent", "host", "temporary"} {
		t.Run(live, func(t *testing.T) {
			s, h := restoreFixture(t)
			r, _ := s.Store.Get("record")
			r.Snapshot = nil // Invalid snapshots must remain deletable.
			if err := s.Store.ApplySync(context.Background(), []model.Record{r}); err != nil {
				t.Fatal(err)
			}
			if live == "host" {
				h.live = []herdr.Workspace{{ID: "old", Label: "project"}}
			}
			if live == "temporary" {
				h.live = []herdr.Workspace{{ID: "new", Label: restorePrefix + "record"}}
			}
			if err := s.DeleteArchived(context.Background(), "record"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Store.Get("record"); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatal(err)
			}
			if h.closed != (live != "absent") || h.calls != 0 {
				t.Fatal(h.closed, h.calls)
			}
			rows, err := s.Refresh(context.Background())
			if err != nil || len(rows) != 0 {
				t.Fatal(rows, err)
			}
		})
	}
}
func TestDeleteArchivedFailuresPreserveRecord(t *testing.T) {
	for _, kind := range []string{"active", "archiving", "closed", "manager", "scan", "ambiguous", "close", "lost close"} {
		t.Run(kind, func(t *testing.T) {
			s, h := restoreFixture(t)
			h.live = []herdr.Workspace{{ID: "old"}}
			r, _ := s.Store.Get("record")
			switch kind {
			case "active", "archiving", "closed":
				r.Status = model.WorkspaceStatus(kind)
			case "manager":
				r.ManagerState = model.ManagerDone
			case "scan":
				h.scanErr = errors.New("scan failed")
			case "ambiguous":
				h.live = append(h.live, herdr.Workspace{ID: "new", Label: restorePrefix + "record"})
			case "close":
				h.fail = true
			case "lost close":
				h.fail = true
				h.closeHook = func() { h.live = []herdr.Workspace{} }
			}
			if err := s.Store.ApplySync(context.Background(), []model.Record{r}); err != nil {
				t.Fatal(err)
			}
			if err := s.DeleteArchived(context.Background(), "record"); err == nil {
				t.Fatal("expected failure")
			}
			got, err := s.Store.Get("record")
			if err != nil || got.Note != r.Note || got.Status != r.Status || h.calls != 0 {
				t.Fatal(got, err, h.calls)
			}
			if kind == "close" || kind == "lost close" {
				h.fail = false
				h.closeHook = nil
				if err := s.DeleteArchived(context.Background(), "record"); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

type failedDeleteStore struct{ Store }

func (s failedDeleteStore) DeleteArchived(context.Context, string, string) error {
	return errors.New("delete failed")
}

func TestDeleteArchivedDatabaseFailureRetry(t *testing.T) {
	s, h := restoreFixture(t)
	h.live = []herdr.Workspace{{ID: "old"}}
	real := s.Store
	s.Store = failedDeleteStore{real}
	if err := s.DeleteArchived(context.Background(), "record"); err == nil {
		t.Fatal("expected database failure")
	}
	r, err := real.Get("record")
	if err != nil || r.Status != model.WorkspaceArchived || !h.closed {
		t.Fatal(r, err, h.closed)
	}
	s.Store = real
	if err := s.DeleteArchived(context.Background(), "record"); err != nil {
		t.Fatal(err)
	}
}
