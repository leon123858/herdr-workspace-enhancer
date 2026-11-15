package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/repository/herdr"
)

type syncItem struct {
	record         *model.Record
	workspace      *herdr.Workspace
	resolved       *model.Record
	resolutionDone bool
}

// Complete all reads and identity checks before any item can mutate the host.
func (s *Service) syncItems(ctx context.Context, live []herdr.Workspace) ([]syncItem, error) {
	records, err := s.Store.NonClosed(ctx)
	if err != nil {
		return nil, err
	}
	byHost := map[string]*model.Record{}
	for i := range records {
		r := &records[i]
		if r.HostID != "" {
			if byHost[r.HostID] != nil {
				return nil, fmt.Errorf("ambiguous host binding %s", r.HostID)
			}
			byHost[r.HostID] = r
		}
	}
	items := make([]syncItem, 0, len(records)+len(live))
	seenHost, seenRecord := map[string]bool{}, map[string]bool{}
	for i := range live {
		w := &live[i]
		if w.ID == "" || seenHost[w.ID] {
			return nil, fmt.Errorf("invalid Herdr workspace identity")
		}
		seenHost[w.ID] = true
		item := syncItem{workspace: w, record: byHost[w.ID]}
		candidate := item.record
		if candidate == nil {
			r, err := s.Store.FindHost(ctx, w.ID)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				for _, prefix := range []string{managerPrefix, restorePrefix} {
					if strings.HasPrefix(w.Label, prefix) {
						r, err = s.Store.Get(strings.TrimPrefix(w.Label, prefix))
						if err == nil && (r.ManagerState != model.ManagerNone) != (prefix == managerPrefix) {
							err = gorm.ErrRecordNotFound
						}
						break
					}
				}
			}
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
			if err == nil {
				item.resolved = &r
				candidate = &r
			}
		}
		if candidate != nil {
			if seenRecord[candidate.ID] {
				return nil, fmt.Errorf("ambiguous workspace binding for %s", candidate.ID)
			}
			seenRecord[candidate.ID] = true
		}
		items = append(items, item)
	}
	for i := range records {
		if !seenRecord[records[i].ID] {
			items = append(items, syncItem{record: &records[i]})
		}
	}
	return items, nil
}

type workspaceState int

const (
	classify workspaceState = iota
	resolveRecord
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
	case resolveRecord:
		return "resolveRecord"
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
				if !item.resolutionDone {
					state = resolveRecord
				} else {
					state = register
				}
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
		case resolveRecord:
			item.record, item.resolutionDone = item.resolved, true
			state = classify
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
