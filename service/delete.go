package service

import (
	"context"
	"fmt"
	"herdr-workspace-enhancer/model"
)

// DeleteArchived removes a saved workspace only after any live remnant is closed.
// It deliberately does not repair via Refresh: that would restart restoration.
func (s *Service) DeleteArchived(ctx context.Context, id string) error {
	if s.MutationError != nil {
		return s.MutationError
	}
	unlock, err := s.Store.WaitLock(ctx, "session")
	if err != nil {
		return err
	}
	defer unlock()
	r, err := s.Store.Get(id)
	if err != nil {
		return err
	}
	if r.Status != model.WorkspaceArchived || r.ManagerState != model.ManagerNone || s.ManagerID != "" && r.HostID == s.ManagerID {
		return fmt.Errorf("only archived non-manager workspaces can be deleted")
	}
	live, err := s.Host.Workspaces(ctx)
	if err != nil {
		return err
	}
	if live == nil {
		return fmt.Errorf("Herdr workspace scan is incomplete")
	}
	items, err := s.syncItems(ctx, live)
	if err != nil {
		return err
	}
	for _, item := range items {
		record := item.record
		if record != nil && record.ID == id && item.workspace != nil {
			if err := s.Host.Close(ctx, item.workspace.ID); err != nil {
				return fmt.Errorf("close archived workspace: %w", err)
			}
		}
	}
	return s.Store.DeleteArchived(ctx, id, r.HostID)
}
