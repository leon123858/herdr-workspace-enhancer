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

// Each invocation records intent and creates a workspace; sync initializes its panes.
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
	}
	// Hold the same lock as event reconciliation through intent persistence and workspace creation.
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
		record := model.Record{ID: recordID, Label: "Workspace Enhancer", Status: model.WorkspaceClosed, ManagerState: model.ManagerInit}
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
	return err
}

type ManagerStore interface {
	model.WorkspaceStore
	Directory() string
}
