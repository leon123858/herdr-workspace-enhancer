package service

import (
	"context"
	"errors"
	"fmt"
	"herdr-workspace-enhancer/model"
	domain "herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/model/layout"
	"herdr-workspace-enhancer/repository/herdr"
	"herdr-workspace-enhancer/repository/state"
	"os"
	"sync"
	"testing"
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

type failingSyncStore struct{ *state.Store }

func (s failingSyncStore) ApplySync(ctx context.Context, records []model.Record) error {
	return errors.New("injected SQL write failure")
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

// Run only against a disposable server selected explicitly by HERDR_SOCKET_PATH.

func TestHerdrIntegration(t *testing.T) {
	if os.Getenv("ENHANCER_INTEGRATION") != "1" {
		t.Skip("requires a disposable Herdr session")
	}
	if os.Getenv("HERDR_SOCKET_PATH") == "" {
		t.Fatal("explicit socket required")
	}
	for _, fault := range []string{"none", "workspace create", "tab create", "pane split", "workspace rename"} {
		t.Run(fault, func(t *testing.T) { runHerdrIntegration(t, fault) })
	}
}

type lostReplyHost struct {
	*herdr.Client
	fault    string
	injected bool
}

func (h *lostReplyHost) Call(ctx context.Context, dst any, args ...string) error {
	err := h.Client.Call(ctx, dst, args...)
	if err == nil && !h.injected && len(args) > 1 && args[0]+" "+args[1] == h.fault {
		h.injected = true
		return fmt.Errorf("injected lost reply after host mutation")
	}
	return err
}

func runHerdrIntegration(t *testing.T, fault string) {
	ctx := context.Background()
	client := herdr.New("herdr")
	if e := client.CheckCapabilities(ctx); e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	var created creation
	if e := client.Call(ctx, &created, "workspace", "create", "--cwd", dir, "--label", "enhancer-integration", "--no-focus"); e != nil {
		t.Fatal(e)
	}
	original := created.Workspace.ID
	defer func() { _ = client.Close(ctx, original) }()
	var split creation
	if e := client.Call(ctx, &split, "pane", "split", created.Root.ID, "--direction", "right", "--ratio", "0.4", "--cwd", dir, "--no-focus"); e != nil {
		t.Fatal(e)
	}
	if e := client.Call(ctx, nil, "pane", "split", split.Pane.ID, "--direction", "down", "--ratio", "0.6", "--cwd", dir, "--no-focus"); e != nil {
		t.Fatal(e)
	}
	if e := client.Call(ctx, nil, "tab", "create", "--workspace", original, "--cwd", dir, "--label", "中文 tab", "--no-focus"); e != nil {
		t.Fatal(e)
	}
	store, e := state.Open(t.TempDir(), "integration")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = store.Close() })
	s := Service{Store: store, Host: client, ManagerID: "manager-not-in-test"}
	records, e := s.Refresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	id := ""
	for _, r := range records {
		if r.HostID == original {
			id = r.ID
		}
	}
	if id == "" {
		t.Fatal("missing record")
	}
	if _, e = store.Save(id, "中文筆記\nrestored"); e != nil {
		t.Fatal(e)
	}
	if e = s.Archive(ctx, id); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Refresh(ctx); e != nil {
		t.Fatal(e)
	}
	host := &lostReplyHost{Client: client, fault: fault}
	s.Host = host
	e = s.Restore(ctx, id)
	if e == nil {
		_, e = s.Refresh(ctx)
	}
	if fault != "none" {
		if e == nil || !host.injected {
			t.Fatal("expected lost reply", e)
		}
		if _, err := s.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
		r, err := store.Get(id)
		if err != nil || r.HostID == original || r.Status != domain.WorkspaceActive {
			t.Fatal(r, err)
		}
	}
	if fault == "none" && e != nil {
		t.Fatal(e)
	}
	records, e = store.List()
	if e != nil {
		t.Fatal(e)
	}
	for _, r := range records {
		if r.ID != id {
			continue
		}
		defer client.Close(ctx, r.HostID)
		if r.Status != domain.WorkspaceActive || r.HostID == original {
			t.Fatal(r)
		}
		snap, e := client.Snapshot(ctx, herdr.Workspace{ID: r.HostID, Label: r.Label})
		if e != nil {
			t.Fatal(e)
		}
		if len(snap.Tabs) != 2 || snap.Tabs[1].Label != "中文 tab" || snap.Tabs[0].Root.Type != layout.NodeSplit || snap.Tabs[0].Root.Second.Type != layout.NodeSplit {
			t.Fatalf("unexpected snapshot: %+v", snap)
		}
	}
	note, e := store.Load(id)
	if e != nil || note.Text != "中文筆記\nrestored" {
		t.Fatal(note, e)
	}
	entries, e := os.ReadDir(dir)
	if e != nil || len(entries) != 0 {
		t.Fatal("project directory was modified", entries, e)
	}
}
