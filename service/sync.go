package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/repository/herdr"

	"gorm.io/gorm"
)

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

type syncItem struct {
	record    *model.Record
	workspace *herdr.Workspace
}

// Resolve all identities before the FSM performs any writes.
func (s *Service) syncItems(ctx context.Context, live []herdr.Workspace) ([]syncItem, error) {
	records, err := s.Store.NonClosed(ctx)
	if err != nil {
		return nil, err
	}
	hosts, db := newIdentitySet[*herdr.Workspace](), newIdentitySet[*model.Record]()
	warn := func(err error) {
		if err != nil {
			slog.Warn("workspace sync identity skipped", "error", err)
		}
	}

	// get host ws and non closed record in db
	for i := range live {
		warn(hosts.add("", live[i].ID, &live[i]))
	}
	for i := range records {
		warn(db.add(records[i].ID, records[i].HostID, &records[i]))
	}

	// use host ws try to bind record in db, 
	// if not found, try to find record by label prefix
	// so may have to key: host id or record id.
	for _, e := range hosts.entries() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		w := e.value
		r, err := s.Store.FindHost(ctx, w.ID)
		if err == nil {
			warn(db.add(r.ID, r.HostID, &r))
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		for _, prefix := range []string{managerPrefix, restorePrefix} {
			if !strings.HasPrefix(w.Label, prefix) {
				continue
			}
			r, err := s.Store.Get(strings.TrimPrefix(w.Label, prefix))
			if err == nil {
				warn(db.add(r.ID, r.HostID, &r))
				warn(hosts.bindRecord(e, r.ID))
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
			break
		}
	}

	items := make([]syncItem, 0, len(live)+len(records))

	// get intersaction of host ws and record in db, remove them from both sets
	for _, byHost := range []bool{true, false} {
		for _, h := range hosts.entries() {
			var r *identityEntry[*model.Record]
			if byHost {
				r = db.hosts[h.hostID]
			} else {
				r = db.records[h.recordID]
			}
			if r == nil {
				continue
			}
			items = append(items, syncItem{record: r.value, workspace: h.value})
			hosts.remove(h)
			db.remove(r)
		}
	}

	// push non intersected host ws and record in db to items, so FSM can handle them
	for _, h := range hosts.entries() {
		items = append(items, syncItem{workspace: h.value})
	}
	for _, r := range db.entries() {
		items = append(items, syncItem{record: r.value})
	}
	return items, nil
}

type workspaceState int

const (
	classify workspaceState = iota
	register
	activate
	markClosed
	markArchived
	restore
	metadata
	manager
)

func (state workspaceState) String() string {
	switch state {
	case classify:
		return "classify"
	case register:
		return "register"
	case activate:
		return "activate"
	case markClosed:
		return "markClosed"
	case markArchived:
		return "markArchived"
	case restore:
		return "restore"
	case metadata:
		return "metadata"
	case manager:
		return "manager"
	default:
		return "unknown"
	}
}

func (s *Service) workspaceFSM(ctx context.Context, item syncItem) (err error) {
	state := classify
	defer func() {
		if err != nil {
			rid, wid := "", ""
			if item.record != nil {
				rid = item.record.ID
			}
			if item.workspace != nil {
				wid = item.workspace.ID
			}
			err = fmt.Errorf("record %s workspace %s stage %s: %w", rid, wid, state, err)
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch state {
		case classify:
			if item.record == nil {
				if item.workspace == nil {
					return nil
				}
				state = register
				continue
			}
			if item.workspace == nil {
				switch item.record.Status {
				case model.WorkspaceActive:
					state = markClosed
				case model.WorkspaceArchiving:
					state = markArchived
				default:
					return nil
				}
			} else {
				switch item.record.Status {
				case model.WorkspaceArchived:
					state = restore
				case model.WorkspaceActive:
					state = activate
				case model.WorkspaceClosed, model.WorkspaceArchiving:
					state = activate
				default:
					return fmt.Errorf("invalid status %s", item.record.Status)
				}
			}
		case register:
			id, err := newID()
			if err != nil {
				return err
			}
			item.record = &model.Record{ID: id, ManagerState: model.ManagerNone}
			state = activate
		case activate:
			r := *item.record
			r.HostID, r.Status = item.workspace.ID, model.WorkspaceActive
			if err := s.Store.ApplySync(ctx, []model.Record{r}); err != nil {
				return err
			}
			*item.record = r
			state = metadata
		case markClosed, markArchived:
			r := *item.record
			r.Status = model.WorkspaceClosed
			if state == markArchived {
				r.Status = model.WorkspaceArchived
			}
			if err := s.Store.ApplySync(ctx, []model.Record{r}); err != nil {
				return err
			}
			*item.record = r
			return nil
		case restore:
			return s.restoreWorkspace(ctx, *item.record, *item.workspace)
		case metadata:
			r, w := *item.record, *item.workspace
			if (w.Label == restorePrefix+r.ID || w.Label == managerPrefix+r.ID) && r.Label != "" {
				if w.Label != r.Label {
					caller, ok := s.Host.(Caller)
					if !ok {
						return fmt.Errorf("host does not support name cleanup")
					}
					if err := caller.Call(ctx, nil, "workspace", "rename", w.ID, r.Label); err != nil {
						return err
					}
				}
			} else if r.Label != w.Label {
				r.Label = w.Label
				if err := s.Store.ApplySync(ctx, []model.Record{r}); err != nil {
					return err
				}
				*item.record = r
			}
			state = manager
		case manager:
			return s.managerFSM(ctx, *item.record)
		}
	}
}
