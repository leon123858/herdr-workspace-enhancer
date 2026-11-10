package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"herdr-workspace-enhancer/internal/herdr"
	"herdr-workspace-enhancer/internal/layout"
	"herdr-workspace-enhancer/internal/state"
	"testing"
)

type restoreHost struct {
	fakeHost
	calls  int
	failAt int
	reject bool
}

func (f *restoreHost) Call(_ context.Context, dst any, args ...string) error {
	if args[0] == "pane" && args[1] == "get" {
		return json.Unmarshal([]byte(`{"pane":{"pane_id":"p1","tab_id":"t1","workspace_id":"new"}}`), dst)
	}
	if args[0] == "pane" && args[1] == "list" {
		return json.Unmarshal([]byte(`{"panes":[{"pane_id":"p1","tab_id":"t1","workspace_id":"new"}]}`), dst)
	}
	f.calls++
	if f.calls == f.failAt {
		if f.reject {
			return &herdr.APIError{Code: "invalid_params", Message: "rejected"}
		}
		return errors.New("connection lost")
	}
	raw := `{}`
	switch args[0] + " " + args[1] {
	case "workspace create":
		raw = `{"workspace":{"workspace_id":"new"},"tab":{"tab_id":"t1"},"root_pane":{"pane_id":"p1"}}`
	case "pane split":
		raw = `{"pane":{"pane_id":"p2"}}`
	case "tab create":
		raw = `{"tab":{"tab_id":"t2"},"root_pane":{"pane_id":"p3"}}`
	}
	return json.Unmarshal([]byte(raw), dst)
}
func restoreFixture(t *testing.T) (*Service, *restoreHost) {
	t.Helper()
	store, e := state.Open(t.TempDir(), "test")
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	leaf := func() *layout.Node { return &layout.Node{Type: "pane", CWD: dir} }
	snap := &herdr.Snapshot{Label: "project", Tabs: []herdr.SavedTab{{Label: "one", Root: &layout.Node{Type: "split", Direction: "right", Ratio: 0.4, First: leaf(), Second: leaf()}}, {Label: "two", Root: leaf()}}}
	e = store.WriteDatabase(state.Database{Version: 1, Records: []state.Record{{ID: "record", Label: "project", Status: "archived", Snapshot: snap}}})
	if e != nil {
		t.Fatal(e)
	}
	host := &restoreHost{}
	return &Service{Store: store, Host: host}, host
}
func TestRestore(t *testing.T) {
	s, h := restoreFixture(t)
	if e := s.Restore(context.Background(), "record"); e != nil {
		t.Fatal(e)
	}
	db, e := s.Store.ReadDatabase()
	if e != nil {
		t.Fatal(e)
	}
	r := db.Records[0]
	if r.Status != "active" || r.HostID != "new" || h.calls != 6 || r.Snapshot == nil {
		t.Fatal(r, h.calls)
	}
}
func TestRestoreDoesNotReplayUncertainMutation(t *testing.T) {
	s, h := restoreFixture(t)
	h.failAt = 3
	if e := s.Restore(context.Background(), "record"); e == nil {
		t.Fatal("expected failure")
	}
	calls := h.calls
	if e := s.Restore(context.Background(), "record"); e == nil {
		t.Fatal("must stop at uncertain mutation")
	}
	if h.calls != calls {
		t.Fatal("mutation was retried")
	}
	db, e := s.Store.ReadDatabase()
	if e != nil {
		t.Fatal(e)
	}
	if db.Records[0].Status != "restoring" || len(db.Records[0].Journal) != 3 {
		t.Fatal(db)
	}
}

func TestRestoreRetriesExplicitRejection(t *testing.T) {
	s, h := restoreFixture(t)
	h.failAt = 3
	h.reject = true
	if e := s.Restore(context.Background(), "record"); e == nil {
		t.Fatal("expected rejection")
	}
	if e := s.Restore(context.Background(), "record"); e != nil {
		t.Fatal(e)
	}
	db, e := s.Store.ReadDatabase()
	if e != nil || db.Records[0].Status != "active" {
		t.Fatal(db, e)
	}
}
