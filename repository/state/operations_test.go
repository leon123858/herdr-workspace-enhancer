package state

import (
	"context"
	"herdr-workspace-enhancer/model"
	"testing"
)

func TestArchiveIntentConditionalWrite(t *testing.T) {
	s := testStore(t, t.TempDir())
	ctx := context.Background()
	put(t, s, Record{ID: "a", HostID: "h", Status: model.WorkspaceActive, Note: "keep"})
	if err := s.ArchiveIntent(ctx, "a", "stale", &model.Snapshot{}); err == nil {
		t.Fatal("accepted stale host")
	}
	r, _ := s.Get("a")
	if r.Status != model.WorkspaceActive || r.Snapshot != nil {
		t.Fatal(r)
	}
	if err := s.ArchiveIntent(ctx, "a", "h", &model.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Get("a")
	if r.Status != model.WorkspaceArchiving || r.Snapshot == nil || r.Note != "keep" {
		t.Fatal(r)
	}
	if err := s.ArchiveIntent(ctx, "a", "h", &model.Snapshot{}); err == nil {
		t.Fatal("accepted repeated intent")
	}
}

func TestApplySyncAtomicAndPreservesLatestNote(t *testing.T) {
	s := testStore(t, t.TempDir())
	ctx := context.Background()
	put(t, s, Record{ID: "a", HostID: "h", Status: model.WorkspaceActive})
	put(t, s, Record{ID: "b", HostID: "other", Status: model.WorkspaceActive})
	records, err := s.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Save("a", "new note"); err != nil {
		t.Fatal(err)
	}
	records[0].Label = "updated"
	records[1].HostID = "h"
	if err = s.ApplySync(ctx, records); err == nil {
		t.Fatal("accepted duplicate host")
	}
	r, _ := s.Get("a")
	if r.Label != "" || r.Note != "new note" {
		t.Fatal("partial write", r)
	}
	records[1].HostID = "other"
	if err = s.ApplySync(ctx, records); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Get("a")
	if r.Label != "updated" || r.Note != "new note" {
		t.Fatal(r)
	}
}

func TestCompleteRestoreConditionalWrite(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  model.WorkspaceStatus
		manager model.ManagerState
		host    string
		success bool
	}{
		{"valid", model.WorkspaceArchived, model.ManagerNone, "h", true},
		{"wrong host", model.WorkspaceArchived, model.ManagerNone, "other", false},
		{"active", model.WorkspaceActive, model.ManagerNone, "h", false},
		{"manager", model.WorkspaceArchived, model.ManagerInit, "h", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t, t.TempDir())
			put(t, s, Record{ID: "r", HostID: "h", Status: tc.status, ManagerState: tc.manager, Label: "label", Note: "note", Snapshot: &model.Snapshot{Label: "saved"}})
			err := s.CompleteRestore(context.Background(), "r", tc.host)
			if (err == nil) != tc.success {
				t.Fatal(err)
			}
			r, _ := s.Get("r")
			want := tc.status
			if tc.success {
				want = model.WorkspaceActive
			}
			if r.Status != want || r.Note != "note" || r.Label != "label" || r.Snapshot.Label != "saved" || r.HostID != "h" {
				t.Fatal(r)
			}
		})
	}
}

func TestDeleteArchivedConditional(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  model.WorkspaceStatus
		manager model.ManagerState
		host    string
		ok      bool
	}{
		{"archived", model.WorkspaceArchived, model.ManagerNone, "h", true},
		{"active", model.WorkspaceActive, model.ManagerNone, "h", false},
		{"manager", model.WorkspaceArchived, model.ManagerInit, "h", false},
		{"stale host", model.WorkspaceArchived, model.ManagerNone, "old", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t, t.TempDir())
			put(t, s, Record{ID: "r", HostID: "h", Status: tc.status, ManagerState: tc.manager, Note: "note"})
			err := s.DeleteArchived(context.Background(), "r", tc.host)
			if (err == nil) != tc.ok {
				t.Fatal(err)
			}
			rows, err := s.All(context.Background())
			if err != nil || tc.ok && len(rows) != 0 || !tc.ok && (len(rows) != 1 || rows[0].Note != "note") {
				t.Fatal(rows, err)
			}
		})
	}
}
