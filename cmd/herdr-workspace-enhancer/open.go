package main

import (
	"context"
	"fmt"
	"herdr-workspace-enhancer/internal/herdr"
	"herdr-workspace-enhancer/internal/state"
	"path/filepath"
	"runtime"
)

// openManager creates a dedicated host workspace once per session. The stored
// identifier, never a user-controlled label, is used to recognize it later.
func openManager(ctx context.Context, client *herdr.Client, store *state.Store) error {
	unlock, e := store.Lock("manager")
	if e != nil {
		return e
	}
	defer unlock()
	note, e := store.Load("manager-workspace-id")
	if e != nil {
		return e
	}
	live, e := client.Workspaces(ctx)
	if e != nil {
		return e
	}
	id := ""
	for _, w := range live {
		if w.ID == note.Text {
			id = w.ID
			break
		}
	}
	if id == "" {
		var result struct {
			Workspace herdr.Workspace `json:"workspace"`
		}
		if e = client.Call(ctx, &result, "workspace", "create", "--cwd", filepath.Clean(store.Dir), "--label", "Workspace Enhancer", "--no-focus"); e != nil {
			return e
		}
		id = result.Workspace.ID
		if id == "" {
			return fmt.Errorf("manager creation returned no workspace ID")
		}
		if _, e = store.Save("manager-workspace-id", note.Revision, id); e != nil {
			return e
		}
	}
	entry := "manager"
	if runtime.GOOS == "windows" {
		entry = "manager-windows"
	}
	return client.Call(ctx, nil, "plugin", "pane", "open", "--plugin", "workspace.enhancer", "--entrypoint", entry, "--workspace", id, "--placement", "tab", "--focus")
}
