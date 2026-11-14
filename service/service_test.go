package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/model/layout"
	"herdr-workspace-enhancer/repository/herdr"
	"herdr-workspace-enhancer/repository/state"
)

type fakeHost struct {
	mu           sync.Mutex
	live         []herdr.Workspace
	scanErr      error
	store        *state.Store
	fail, closed bool
	snapshotHook func()
	snapshot     *herdr.Snapshot
	closeHook    func()
}

func (f *fakeHost) Workspaces(context.Context) ([]herdr.Workspace, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.live == nil {
		return nil, f.scanErr
	}
	return append([]herdr.Workspace{}, f.live...), f.scanErr
}
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
	if f.store != nil {
		rows, e := f.store.List()
		if e != nil {
			return e
		}
		if len(rows) == 0 || rows[0].Snapshot == nil || rows[0].Status != model.WorkspaceArchiving {
			return errors.New("archive intent not durable")
		}
	}
	if f.closeHook != nil {
		f.closeHook()
	}
	if f.fail {
		return errors.New("close refused")
	}
	f.mu.Lock()
	f.closed = true
	f.live = []herdr.Workspace{}
	f.mu.Unlock()
	return nil
}
func serviceStore(t *testing.T) *state.Store {
	t.Helper()
	s, e := state.Open(t.TempDir(), "test")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func saveRecord(t *testing.T, s *state.Store, r model.Record) {
	t.Helper()
	if e := s.ApplySync(context.Background(), []model.Record{r}); e != nil {
		t.Fatal(e)
	}
}

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
	if e != nil || rows[0].Status != model.WorkspaceActive || rows[0].Note != "keep note" {
		t.Fatal(rows, e)
	}
}
func TestSyncInvalidScanNeverChangesState(t *testing.T) {
	for _, kind := range []string{"error", "nil", "duplicate", "empty-id"} {
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
			rows, _ := store.List()
			if len(rows) != 1 {
				t.Fatal("scan partly committed", rows)
			}
		})
	}
}
func TestManagersAreExcludedByFlag(t *testing.T) {
	store := serviceStore(t)
	saveRecord(t, store, model.Record{ID: "manager", HostID: "manager-host", IsManager: true, Status: model.WorkspaceActive})
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
func TestArchiveCloseFailureIsReconciledActive(t *testing.T) {
	store := serviceStore(t)
	host := &fakeHost{store: store, live: []herdr.Workspace{{ID: "w1"}}, fail: true}
	s := Service{Store: store, Host: host}
	rows, _ := s.Refresh(context.Background())
	if e := s.Archive(context.Background(), rows[0].ID); e == nil {
		t.Fatal("expected close failure")
	}
	r, e := store.Get(rows[0].ID)
	if e != nil || r.Status != model.WorkspaceActive || r.Snapshot == nil {
		t.Fatal(r, e)
	}
}

type failedCheckpointStore struct {
	*state.Store
	fail          bool
	failAt, calls int
}

func (s *failedCheckpointStore) checkpoint() error {
	s.calls++
	if s.fail || s.failAt == s.calls {
		return errors.New("injected checkpoint failure")
	}
	return nil
}
func (s *failedCheckpointStore) ArchiveIntent(ctx context.Context, id, host string, snapshot *model.Snapshot) error {
	if err := s.checkpoint(); err != nil {
		return err
	}
	return s.Store.ArchiveIntent(ctx, id, host, snapshot)
}
func (s *failedCheckpointStore) ApplySync(ctx context.Context, records []model.Record) error {
	if err := s.checkpoint(); err != nil {
		return err
	}
	return s.Store.ApplySync(ctx, records)
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
func TestSyncRebindsLostCreationWithoutLosingNote(t *testing.T) {
	store := serviceStore(t)
	saveRecord(t, store, model.Record{ID: "one", HostID: "old", Label: "Original", Status: model.WorkspaceArchived, Note: "note"})
	host := &fakeHost{live: []herdr.Workspace{{ID: "new", Label: restorePrefix + "one"}}}
	s := Service{Store: store, Host: host}
	rows, e := s.Refresh(context.Background())
	if e != nil || len(rows) != 1 || rows[0].ID != "one" || rows[0].HostID != "new" || rows[0].Note != "note" {
		t.Fatal(rows, e)
	}
	host.live = append(host.live, herdr.Workspace{ID: "other", Label: restorePrefix + "one"})
	if _, e = s.Refresh(context.Background()); e == nil {
		t.Fatal("ambiguous ID binding accepted")
	}
	r, _ := store.Get("one")
	if r.HostID != "new" {
		t.Fatal("failed sync partly committed", r)
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
	if _, e := s.Refresh(context.Background()); e != nil {
		t.Fatal(e)
	}
	r, _ = store.Get(id)
	if r.Status != model.WorkspaceActive {
		t.Fatal("sync did not repair failed reset", r)
	}
}

type failingSyncStore struct{ *state.Store }

func (s failingSyncStore) ApplySync(ctx context.Context, records []model.Record) error {
	return errors.New("injected SQL write failure")
}
func TestSyncWriteFailureRollsBackTheWholeScan(t *testing.T) {
	store := serviceStore(t)
	saveRecord(t, store, model.Record{ID: "one", HostID: "old", Status: model.WorkspaceActive, Note: "keep"})
	host := &fakeHost{live: []herdr.Workspace{{ID: "new", Label: "New"}}}
	s := Service{Store: failingSyncStore{store}, Host: host}
	if _, e := s.Refresh(context.Background()); e == nil {
		t.Fatal("expected SQL failure")
	}
	rows, e := store.List()
	if e != nil || len(rows) != 1 || rows[0].HostID != "old" || rows[0].Status != model.WorkspaceActive || rows[0].Note != "keep" {
		t.Fatal("partial sync committed", rows, e)
	}
}

func (f *fakeHost) Call(_ context.Context, _ any, args ...string) error {
	if len(args) == 4 && args[0] == "workspace" && args[1] == "rename" {
		for i := range f.live {
			if f.live[i].ID == args[2] {
				f.live[i].Label = args[3]
				return nil
			}
		}
	}
	return errors.New("unexpected host call")
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
