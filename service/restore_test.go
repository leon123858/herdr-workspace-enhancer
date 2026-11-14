package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/model/layout"
	"herdr-workspace-enhancer/repository/herdr"
	"herdr-workspace-enhancer/repository/state"
)

type restoreHost struct {
	fakeHost
	calls, failAt int
	lostCreate    bool
	layoutHook    func() error
}

func (h *restoreHost) Call(_ context.Context, dst any, args ...string) error {
	h.calls++
	if h.calls == h.failAt {
		return errors.New("injected host failure")
	}
	if args[0]+" "+args[1] != "workspace create" && h.layoutHook != nil {
		if err := h.layoutHook(); err != nil {
			return err
		}
	}
	raw := `{}`
	switch args[0] + " " + args[1] {
	case "workspace create":
		h.live = append(h.live, herdr.Workspace{ID: "new", Label: args[5]})
		if h.lostCreate {
			return errors.New("lost creation reply")
		}
		raw = `{"workspace":{"workspace_id":"new"},"tab":{"tab_id":"t1"},"root_pane":{"pane_id":"p1"}}`
	case "pane split":
		raw = `{"pane":{"pane_id":"p2"}}`
	case "tab create":
		raw = `{"tab":{"tab_id":"t2"},"root_pane":{"pane_id":"p3"}}`
	case "workspace rename":
		h.live[0].Label = args[3]
	}
	if dst == nil {
		return nil
	}
	return json.Unmarshal([]byte(raw), dst)
}
func restoreFixture(t *testing.T) (*Service, *restoreHost) {
	t.Helper()
	store := serviceStore(t)
	dir := t.TempDir()
	leaf := func() *layout.Node { return &layout.Node{Type: layout.NodePane, CWD: dir} }
	snap := &herdr.Snapshot{Label: "project", Tabs: []herdr.SavedTab{{Label: "one", Root: &layout.Node{Type: layout.NodeSplit, Direction: "right", Ratio: .4, First: leaf(), Second: leaf()}}, {Label: "two", Root: leaf()}}}
	saveRecord(t, store, model.Record{ID: "record", HostID: "old", Label: "project", Status: model.WorkspaceArchived, Snapshot: snap, Note: "keep note"})
	host := &restoreHost{fakeHost: fakeHost{live: []herdr.Workspace{}}}
	return &Service{Store: store, Host: host}, host
}
func TestRestoreKeepsArchivedUntilSync(t *testing.T) {
	s, h := restoreFixture(t)
	h.layoutHook = func() error {
		r, e := s.Store.Get("record")
		if e != nil {
			return e
		}
		if r.HostID != "old" || r.Status != model.WorkspaceArchived {
			return errors.New("restore wrote before sync")
		}
		_, e = s.Store.Save("record", "edited during restore")
		return e
	}
	if e := s.Restore(context.Background(), "record"); e != nil {
		t.Fatal(e)
	}
	r, e := s.Store.Get("record")
	if e != nil || r.Status != model.WorkspaceArchived || r.HostID != "old" || r.Note != "edited during restore" || h.calls != 5 {
		t.Fatal(r, e, h.calls)
	}
	h.layoutHook = nil
	rows, e := s.Refresh(context.Background())
	if e != nil || len(rows) != 1 || rows[0].ID != "record" || rows[0].Note != "edited during restore" {
		t.Fatal(rows, e)
	}
}
func TestPartialRestoreRemainsActiveWithoutAutomaticReplay(t *testing.T) {
	s, h := restoreFixture(t)
	h.failAt = 3
	if e := s.Restore(context.Background(), "record"); e == nil {
		t.Fatal("expected failure")
	}
	r, _ := s.Store.Get("record")
	if r.Status != model.WorkspaceActive || r.HostID != "new" || r.Snapshot == nil {
		t.Fatal(r)
	}
	calls := h.calls
	if e := s.Restore(context.Background(), "record"); e == nil || h.calls != calls {
		t.Fatal("partial restoration replayed", e)
	}
	rows, e := s.Refresh(context.Background())
	if e != nil || len(rows) != 1 || rows[0].ID != "record" {
		t.Fatal(rows, e)
	}
}
func TestLostCreationReplyRebindsInsteadOfDuplicating(t *testing.T) {
	s, h := restoreFixture(t)
	h.lostCreate = true
	if e := s.Restore(context.Background(), "record"); e == nil {
		t.Fatal("expected lost reply")
	}
	if _, e := s.Refresh(context.Background()); e != nil {
		t.Fatal(e)
	}
	r, _ := s.Store.Get("record")
	if r.HostID != "new" || r.Status != model.WorkspaceActive {
		t.Fatal(r)
	}
	if e := s.Restore(context.Background(), "record"); e == nil || h.calls != 2 {
		t.Fatal("creation repeated", e, h.calls)
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
