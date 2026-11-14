package service

import (
	"context"
	"fmt"
	domain "herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/model/layout"
	"herdr-workspace-enhancer/repository/herdr"
	"herdr-workspace-enhancer/repository/state"
	"os"
	"testing"
)

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
		defer client.Close(ctx, r.HostID)
		return
	}
	if e != nil {
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
