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
