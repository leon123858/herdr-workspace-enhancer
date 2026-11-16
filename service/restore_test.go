package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/model/layout"
	"herdr-workspace-enhancer/repository/herdr"
	"sync"
	"testing"
)

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
	if err := s.Restore(context.Background(), "record"); err != nil {
		t.Fatal(err)
	}
	if h.calls != calls || len(h.live) != 1 {
		t.Fatal("duplicate creation", h.calls, h.live)
	}
	r, _ = s.Store.Get("record")
	if r.Status != model.WorkspaceArchived || r.HostID != "old" {
		t.Fatal(r)
	}
}

type restoreHost struct {
	mu sync.Mutex
	fakeHost
	calls, failAt int
	lostCreate    bool
	layoutHook    func() error
}

func (h *restoreHost) Workspaces(ctx context.Context) ([]herdr.Workspace, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.fakeHost.Workspaces(ctx)
}

func (h *restoreHost) Call(_ context.Context, dst any, args ...string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
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
	case "tab list":
		raw = `{"tabs":[{"tab_id":"old-tab"}]}`
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
	if e := s.Restore(context.Background(), "record"); e != nil {
		t.Fatal(e)
	}
	r, e := s.Store.Get("record")
	if e != nil || r.Status != model.WorkspaceArchived || r.HostID != "old" || h.calls != 1 {
		t.Fatal(r, e, h.calls)
	}
	h.layoutHook = func() error { _, err := s.Store.Save("record", "edited during restore"); return err }
	rows, e := s.Refresh(context.Background())
	if e != nil || len(rows) != 1 || rows[0].ID != "record" || rows[0].Note != "edited during restore" {
		t.Fatal(rows, e)
	}
}

func TestPartialRestoreRetries(t *testing.T) {
	for failAt := 2; failAt <= 7; failAt++ {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			s, h := restoreFixture(t)
			if err := s.Restore(context.Background(), "record"); err != nil {
				t.Fatal(err)
			}
			h.failAt = failAt
			if _, err := s.Refresh(context.Background()); err == nil {
				t.Fatal("expected failure")
			}
			r, _ := s.Store.Get("record")
			if r.Status != model.WorkspaceArchived {
				t.Fatal(r)
			}
			if _, err := s.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}
			r, _ = s.Store.Get("record")
			if r.Status != model.WorkspaceActive {
				t.Fatal(r)
			}
			calls := h.calls
			if _, err := s.Refresh(context.Background()); err != nil || h.calls != calls {
				t.Fatal(err, h.calls)
			}
		})
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
	calls := h.calls
	if e := s.Restore(context.Background(), "record"); e == nil || h.calls != calls {
		t.Fatal("creation repeated", e, h.calls)
	}
}

type restoreCommitFailure struct {
	Store
	fail bool
}

func (s *restoreCommitFailure) CompleteRestore(ctx context.Context, id, host string) error {
	if s.fail {
		s.fail = false
		return errors.New("commit failed")
	}
	return s.Store.CompleteRestore(ctx, id, host)
}

func TestRestoreCommitFailureRetriesAfterRename(t *testing.T) {
	s, h := restoreFixture(t)
	s.Store = &restoreCommitFailure{Store: s.Store, fail: true}
	if err := s.Restore(context.Background(), "record"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Refresh(context.Background()); err == nil {
		t.Fatal("expected commit failure")
	}
	r, _ := s.Store.Get("record")
	if r.Status != model.WorkspaceArchived || h.live[0].Label != "project" {
		t.Fatal(r, h.live)
	}
	if _, err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidRestoreDoesNotBlockOtherItems(t *testing.T) {
	s, h := restoreFixture(t)
	r, _ := s.Store.Get("record")
	r.Snapshot = nil
	if err := s.Store.ApplySync(context.Background(), []model.Record{r}); err != nil {
		t.Fatal(err)
	}
	h.live = []herdr.Workspace{{ID: "old"}, {ID: "unrelated", Label: "other"}}
	if _, err := s.Refresh(context.Background()); err == nil {
		t.Fatal("expected invalid snapshot")
	}
	if h.calls != 0 {
		t.Fatal("invalid snapshot mutated host")
	}
	rows, err := s.Store.List()
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
}

func TestConcurrentRestoreCreatesOnce(t *testing.T) {
	s, h := restoreFixture(t)
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { done <- s.Restore(context.Background(), "record") }()
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if h.calls != 1 || len(h.live) != 1 {
		t.Fatal(h.calls, h.live)
	}
}
