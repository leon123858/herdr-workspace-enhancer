package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/repository/herdr"
)

type ArchivePreview struct {
	RecordID string
	HostID   string
	Snapshot herdr.Snapshot
}

func (s *Service) Preview(ctx context.Context, id string) (ArchivePreview, error) {
	r, err := s.Store.Get(id)
	if err != nil {
		return ArchivePreview{}, err
	}
	if r.ManagerState != model.ManagerNone || r.HostID == s.ManagerID && s.ManagerID != "" {
		return ArchivePreview{}, fmt.Errorf("cannot archive manager")
	}
	if r.Status != model.WorkspaceActive {
		return ArchivePreview{}, fmt.Errorf("workspace is %s", r.Status)
	}
	snapshot, err := s.Host.Snapshot(ctx, herdr.Workspace{ID: r.HostID, Label: r.Label})
	if err != nil {
		return ArchivePreview{}, err
	}
	return ArchivePreview{RecordID: r.ID, HostID: r.HostID, Snapshot: snapshot}, nil
}

var ErrPreviewChanged = errors.New("workspace has changed; preview it again and confirm archiving")

func (s *Service) Archive(ctx context.Context, id string) error { return s.archive(ctx, id, nil) }
func (s *Service) ArchiveConfirmed(ctx context.Context, p ArchivePreview) error {
	return s.archive(ctx, p.RecordID, &p)
}
func (s *Service) archive(ctx context.Context, id string, preview *ArchivePreview) error {
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
	if r.ManagerState != model.ManagerNone || r.HostID == s.ManagerID && s.ManagerID != "" {
		return fmt.Errorf("cannot archive manager workspace")
	}
	if r.Status != model.WorkspaceActive {
		return fmt.Errorf("workspace is %s", r.Status)
	}
	if preview != nil && preview.HostID != r.HostID {
		return ErrPreviewChanged
	}
	snapshot, err := s.Host.Snapshot(ctx, herdr.Workspace{ID: r.HostID, Label: r.Label})
	if err != nil {
		return err
	}
	if preview != nil && !reflect.DeepEqual(preview.Snapshot, snapshot) {
		return ErrPreviewChanged
	}
	err = s.Store.ArchiveIntent(ctx, id, r.HostID, &snapshot)
	if err != nil {
		return err
	}
	if err = s.Host.Close(ctx, r.HostID); err != nil {
		return s.repairLocked(err)
	}
	return nil
}
