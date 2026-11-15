// Package service coordinates plugin state with Herdr operations.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"time"

	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/repository/herdr"
)

type Host interface {
	Workspaces(context.Context) ([]herdr.Workspace, error)
	Snapshot(context.Context, herdr.Workspace) (herdr.Snapshot, error)
	Close(context.Context, string) error
}
type Store = model.WorkspaceStore

type Service struct {
	Store         Store
	Host          Host
	MutationError error
	ManagerID     string
}

const restorePrefix = "enhancer-restore-"
const managerPrefix = "enhancer-manager-"

// Refresh accepts only a successful complete scan. Herdr's session must already
// be restored by the caller: an actual successful empty list means no workspaces.
// The session lock excludes our open/archive/restore operations while scanning;
// each item commits its own transitions without holding a SQL transaction.
func (s *Service) Refresh(ctx context.Context) ([]model.Record, error) {
	unlock, err := s.Store.WaitLock(ctx, "session")
	if err != nil {
		return nil, err
	}
	defer unlock()
	return s.refreshLocked(ctx)
}

func (s *Service) refreshLocked(ctx context.Context) ([]model.Record, error) {
	live, err := s.Host.Workspaces(ctx)
	if err != nil {
		return nil, err
	}
	if live == nil {
		return nil, fmt.Errorf("Herdr workspace scan is incomplete")
	}
	items, err := s.syncItems(ctx, live)
	if err != nil {
		return nil, err
	}
	var failures []error
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		if err := s.workspaceFSM(ctx, item); err != nil {
			failures = append(failures, err)
		}
	}
	if err := errors.Join(failures...); err != nil {
		return nil, err
	}
	return s.Store.List()
}

func (s *Service) StoredRows() ([]model.Record, error)      { return s.Store.List() }
func (s *Service) Load(id string) (model.Note, error)       { return s.Store.Load(id) }
func (s *Service) Save(id, text string) (model.Note, error) { return s.Store.Save(id, text) }

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

// Failure repair is a separate logical operation with its own bounded context.
// The caller already owns the session lock, so it must not acquire it again.
func (s *Service) repairLocked(original error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.refreshLocked(ctx)
	if err != nil {
		return errors.Join(original, fmt.Errorf("repair sync failed: %w", err))
	}
	return original
}

func newID() (string, error) {
	var b [16]byte
	_, err := rand.Read(b[:])
	return hex.EncodeToString(b[:]), err
}

// Focus has no action writes; a failed host call triggers one independent sync.
func (s *Service) Focus(ctx context.Context, host string, focus func(context.Context, string) error) error {
	original := focus(ctx, host)
	if original == nil {
		return nil
	}
	repairCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.Refresh(repairCtx)
	if err != nil {
		return errors.Join(original, fmt.Errorf("repair sync failed: %w", err))
	}
	return original
}
