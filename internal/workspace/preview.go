package workspace

import (
	"context"
	"fmt"
	"herdr-workspace-enhancer/internal/herdr"
)

type ArchivePreview struct {
	RecordID string
	HostID   string
	Snapshot herdr.Snapshot
}

func (s *Service) Preview(ctx context.Context, id string) (ArchivePreview, error) {
	db, e := s.Store.ReadDatabase()
	if e != nil {
		return ArchivePreview{}, e
	}
	for _, r := range db.Records {
		if r.ID == id {
			managers, err := s.managerIDs()
			if err != nil {
				return ArchivePreview{}, err
			}
			if managers[r.HostID] {
				return ArchivePreview{}, fmt.Errorf("cannot archive manager")
			}
			snapshot, err := s.Host.Snapshot(ctx, herdr.Workspace{ID: r.HostID, Label: r.Label})
			if err != nil {
				return ArchivePreview{}, err
			}
			return ArchivePreview{RecordID: r.ID, HostID: r.HostID, Snapshot: snapshot}, nil
		}
	}
	return ArchivePreview{}, fmt.Errorf("workspace not found")
}
