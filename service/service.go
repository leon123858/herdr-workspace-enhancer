// Package service coordinates plugin state with Herdr operations.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
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
// one SQL statement atomically applies all computed synchronization results.
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
	seen := map[string]bool{}
	for _, w := range live {
		if w.ID == "" || seen[w.ID] {
			return nil, fmt.Errorf("invalid Herdr workspace identity")
		}
		seen[w.ID] = true
	}
	records, err := s.Store.All(ctx)
	if err != nil {
		return nil, err
	}
	var renames []model.Record
	err = func() error {
		results := make([]model.Record, 0, len(records)+len(live))
		byHost := map[string]model.Record{}
		byID := map[string]model.Record{}
		for _, r := range records {
			byID[r.ID] = r
			if r.HostID != "" {
				byHost[r.HostID] = r
			}
		}
		for _, w := range live {
			r, found := byHost[w.ID]
			if !found {
				// A creation response can be lost before its new ID is persisted. The
				// temporary label ties it back to the original note without a journal.
				prefix := restorePrefix
				if strings.HasPrefix(w.Label, managerPrefix) {
					prefix = managerPrefix
				}
				if candidate, ok := byID[strings.TrimPrefix(w.Label, prefix)]; ok && strings.HasPrefix(w.Label, prefix) && candidate.IsManager == (prefix == managerPrefix) {
					r = candidate
					found = true
				}
			}
			if !found {
				id, e := newID()
				if e != nil {
					return e
				}
				r = model.Record{ID: id}
			}
			// One record must never be rebound to multiple live workspaces.
			if previous, exists := byID[r.ID]; exists && previous.HostID != "" && previous.HostID != w.ID && seen[previous.HostID] {
				return fmt.Errorf("ambiguous workspace binding for %s", r.ID)
			}
			r.HostID = w.ID
			r.Status = model.WorkspaceActive
			temporaryLabel := w.Label == restorePrefix+r.ID || w.Label == managerPrefix+r.ID
			if !temporaryLabel {
				r.Label = w.Label
			}
			if r.Label == "" {
				r.Label = w.Label
			}
			if w.ID == s.ManagerID {
				r.IsManager = true
			}
			if temporaryLabel && w.Label != r.Label {
				renames = append(renames, r)
			}
			results = append(results, r)
			byID[r.ID] = r
		}
		for _, original := range records {
			r := byID[original.ID]
			if !seen[r.HostID] {
				switch r.Status {
				case model.WorkspaceActive:
					r.Status = model.WorkspaceClosed
				case model.WorkspaceArchiving:
					r.Status = model.WorkspaceArchived
				}
				results = append(results, r)
			}
		}
		return s.Store.ApplySync(ctx, results)
	}()
	if err != nil {
		return nil, err
	}
	for _, r := range renames {
		caller, ok := s.Host.(Caller)
		if !ok {
			return nil, fmt.Errorf("host does not support temporary workspace name cleanup")
		}
		if err := caller.Call(ctx, nil, "workspace", "rename", r.HostID, r.Label); err != nil {
			return nil, fmt.Errorf("clean up workspace name: %w", err)
		}
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
	if r.IsManager || r.HostID == s.ManagerID && s.ManagerID != "" {
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
