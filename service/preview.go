package service

import (
	"context"
	"fmt"
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
	if r.IsManager || r.HostID == s.ManagerID && s.ManagerID != "" {
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
