// Package workspace coordinates durable state with Herdr operations.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"herdr-workspace-enhancer/internal/herdr"
	"herdr-workspace-enhancer/internal/state"
	"reflect"
)

type Host interface {
	Workspaces(context.Context) ([]herdr.Workspace, error)
	Snapshot(context.Context, herdr.Workspace) (herdr.Snapshot, error)
	Close(context.Context, string) error
}
type Service struct {
	Store         *state.Store
	Host          Host
	MutationError error
	ManagerID     string
}

func (s *Service) Refresh(ctx context.Context) ([]state.Record, error) {
	unlock, e := s.Store.Lock("session")
	if e != nil {
		return nil, e
	}
	defer unlock()
	db, e := s.Store.ReadDatabase()
	if e != nil {
		return nil, e
	}
	live, e := s.Host.Workspaces(ctx)
	if e != nil {
		return nil, e
	}
	// Reattach a restoration whose create reply was lost before HostID saved.
	for i := range db.Records {
		r := &db.Records[i]
		if r.Status != "restoring" || r.HostID != "" {
			continue
		}
		var candidates []herdr.Workspace
		for _, w := range live {
			if w.Label == "enhancer-restore-"+r.ID {
				candidates = append(candidates, w)
			}
		}
		if len(candidates) == 1 {
			r.HostID = candidates[0].ID
		}
	}
	for _, w := range live {
		if w.ID == s.ManagerID {
			continue
		}
		found := false
		for i := range db.Records {
			r := &db.Records[i]
			if r.HostID == w.ID && r.Status != "archived" {
				if r.Status != "restoring" {
					r.Label = w.Label
				}
				found = true
				break
			}
		}
		if !found {
			id, e := state.NewID()
			if e != nil {
				return nil, e
			}
			db.Records = append(db.Records, state.Record{ID: id, HostID: w.ID, Label: w.Label, Status: "active"})
		}
	}
	for i := range db.Records {
		r := &db.Records[i]
		exists := false
		for _, w := range live {
			if w.ID == r.HostID {
				exists = true
				break
			}
		}
		if !exists {
			if r.Status == "archiving" {
				r.Status = "archived"
				r.HostID = ""
			} else if r.Status == "active" {
				r.Status = "missing"
			}
		}
	}
	if e = s.Store.WriteDatabase(db); e != nil {
		return nil, e
	}
	return db.Records, nil
}

var ErrPreviewChanged = errors.New("workspace 已變更，請重新預覽並確認封存")

func (s *Service) Archive(ctx context.Context, id string) error { return s.archive(ctx, id, nil) }
func (s *Service) ArchiveConfirmed(ctx context.Context, preview ArchivePreview) error {
	return s.archive(ctx, preview.RecordID, &preview)
}
func (s *Service) archive(ctx context.Context, id string, preview *ArchivePreview) error {
	if s.MutationError != nil {
		return s.MutationError
	}
	unlock, e := s.Store.Lock("session")
	if e != nil {
		return e
	}
	defer unlock()
	db, e := s.Store.ReadDatabase()
	if e != nil {
		return e
	}
	for i := range db.Records {
		r := &db.Records[i]
		if r.ID != id {
			continue
		}
		if preview != nil && preview.HostID != r.HostID {
			return ErrPreviewChanged
		}
		if r.HostID == s.ManagerID {
			return fmt.Errorf("cannot archive the manager workspace")
		}
		if r.Status != "active" && r.Status != "archiving" {
			return fmt.Errorf("workspace is %s", r.Status)
		}
		// Reconcile a previous uncertain close before making another request.
		live, e := s.Host.Workspaces(ctx)
		if e != nil {
			return e
		}
		var target *herdr.Workspace
		for j := range live {
			if live[j].ID == r.HostID {
				target = &live[j]
				break
			}
		}
		if target == nil {
			if r.Status == "archiving" && r.Snapshot != nil {
				r.Status = "archived"
				r.HostID = ""
				return s.Store.WriteDatabase(db)
			}
			return fmt.Errorf("workspace no longer exists")
		}
		snapshot, e := s.Host.Snapshot(ctx, *target)
		if e != nil {
			return e
		}
		if preview != nil && !reflect.DeepEqual(preview.Snapshot, snapshot) {
			return ErrPreviewChanged
		}
		r.Snapshot = &snapshot
		r.Status = "archiving"
		if e = s.Store.WriteDatabase(db); e != nil {
			return e
		}
		closeErr := s.Host.Close(ctx, r.HostID)
		live, e = s.Host.Workspaces(ctx)
		if e != nil {
			return fmt.Errorf("archive outcome is uncertain; refresh to reconcile: %w", e)
		}
		for _, w := range live {
			if w.ID == r.HostID {
				if closeErr != nil {
					return closeErr
				}
				return fmt.Errorf("workspace remains open")
			}
		}
		r.HostID = ""
		r.Status = "archived"
		return s.Store.WriteDatabase(db)
	}
	return fmt.Errorf("workspace record not found")
}
