package service

import (
	"context"
	"errors"
	"fmt"
	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/repository/herdr"
	"time"

	"path/filepath"
)

// Each invocation owns a fresh workspace and replaces only its initial shell.
func OpenManager(ctx context.Context, client *herdr.Client, store ManagerStore) (result error) {
	svc := &Service{Store: store, Host: client}
	repairNeeded := false
	defer func() {
		if result != nil && repairNeeded {
			repairCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if _, err := svc.Refresh(repairCtx); err != nil {
				result = errors.Join(result, fmt.Errorf("repair sync failed: %w", err))
			}
		}
	}()
	var created struct {
		Workspace herdr.Workspace `json:"workspace"`
		Tab       herdr.Tab       `json:"tab"`
		Root      herdr.Pane      `json:"root_pane"`
	}
	// Hold the same lock as event reconciliation until registration is durable.
	// The create event can arrive before the create command returns.
	err := func() error {
		unlock, err := store.WaitLock(ctx, "session")
		if err != nil {
			return err
		}
		defer unlock()
		recordID, err := newID()
		if err != nil {
			return err
		}
		record := model.Record{ID: recordID, Label: "Workspace Enhancer", Status: model.WorkspaceClosed, IsManager: true}
		if err = store.CreateManager(ctx, record); err != nil {
			return err
		}
		repairNeeded = true
		if err := client.Call(ctx, &created, "workspace", "create", "--cwd", filepath.Clean(store.Directory()), "--label", managerPrefix+recordID, "--no-focus"); err != nil {
			return fmt.Errorf("create manager workspace: %w", err)
		}
		if created.Workspace.ID == "" {
			return fmt.Errorf("manager creation returned no workspace ID")
		}
		return nil
	}()
	if err != nil {
		return err
	}
	repairNeeded = false
	if _, err := svc.Refresh(ctx); err != nil {
		return err
	}
	repairNeeded = true
	id := created.Workspace.ID
	registered, err := store.IsManager(id)
	if err != nil {
		return err
	}
	if !registered {
		return fmt.Errorf("manager identity not registered")
	}
	if created.Tab.ID == "" || created.Root.ID == "" {
		return fmt.Errorf("manager creation returned incomplete initial tab or pane IDs")
	}
	var opened struct {
		PluginPane struct {
			Pane herdr.Pane `json:"pane"`
		} `json:"plugin_pane"`
	}
	if err := client.Call(ctx, &opened, "plugin", "pane", "open", "--plugin", "workspace.enhancer", "--entrypoint", "manager", "--placement", "split", "--target-pane", created.Root.ID, "--focus"); err != nil {
		return fmt.Errorf("start manager in workspace %s: %w", id, err)
	}
	if opened.PluginPane.Pane.ID == "" || opened.PluginPane.Pane.ID == created.Root.ID {
		return fmt.Errorf("manager startup returned no distinct pane ID; initial shell retained")
	}
	if err := client.Call(ctx, nil, "pane", "close", created.Root.ID); err != nil {
		return fmt.Errorf("manager started but initial shell cleanup failed: %w", err)
	}
	if err := client.Focus(ctx, id); err != nil {
		return fmt.Errorf("focus manager pane: %w", err)
	}
	return nil
}

type ManagerStore interface {
	model.WorkspaceStore
	Directory() string
}
