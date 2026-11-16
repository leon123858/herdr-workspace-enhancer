package service

import (
	"context"
	"fmt"
	"strconv"

	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/model/layout"
	"herdr-workspace-enhancer/repository/herdr"
)

type Caller interface {
	Call(context.Context, any, ...string) error
}
type creation struct {
	Workspace struct {
		ID string `json:"workspace_id"`
	} `json:"workspace"`
	Tab struct {
		ID string `json:"tab_id"`
	} `json:"tab"`
	Root struct {
		ID string `json:"pane_id"`
	} `json:"root_pane"`
	Pane struct {
		ID string `json:"pane_id"`
	} `json:"pane"`
}

func first(n *layout.Node) *layout.Node {
	for n.Type == layout.NodeSplit {
		n = n.First
	}
	return n
}

// Restore creates a target for event reconciliation, or retries reconciliation
// immediately when a previous attempt already created the target.
func (s *Service) Restore(ctx context.Context, id string) error {
	if s.MutationError != nil {
		return s.MutationError
	}
	caller, ok := s.Host.(Caller)
	if !ok {
		return fmt.Errorf("host does not support restoration")
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
	if r.ManagerState != model.ManagerNone {
		return fmt.Errorf("cannot restore manager workspace")
	}
	if r.Status != model.WorkspaceArchived {
		return fmt.Errorf("workspace is %s", r.Status)
	}
	live, err := s.Host.Workspaces(ctx)
	if err != nil {
		return err
	}
	count := 0
	for _, w := range live {
		if w.ID == r.HostID || w.Label == restorePrefix+r.ID {
			count++
		}
	}
	if count > 1 {
		return fmt.Errorf("ambiguous restore candidates for %s", r.ID)
	}
	if count == 1 {
		// The session lock is already held. A previous sync may have failed,
		// so do not wait for a creation event that will not be emitted again.
		_, err := s.refreshLocked(ctx)
		return err
	}
	if err := validateRestore(r); err != nil {
		return err
	}
	if err := caller.Call(ctx, nil, "workspace", "create", "--cwd", first(r.Snapshot.Tabs[0].Root).CWD, "--label", restorePrefix+r.ID, "--no-focus"); err != nil {
		return s.repairLocked(err)
	}
	return nil
}

func validateRestore(r model.Record) error {
	if r.Snapshot == nil || len(r.Snapshot.Tabs) == 0 {
		return fmt.Errorf("snapshot is empty")
	}
	for _, tab := range r.Snapshot.Tabs {
		if err := tab.Root.Validate(true); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) restoreWorkspace(ctx context.Context, r model.Record, w herdr.Workspace) error {
	if err := validateRestore(r); err != nil {
		return fmt.Errorf("validate snapshot: %w", err)
	}
	caller, ok := s.Host.(Caller)
	if !ok {
		return fmt.Errorf("host does not support restoration")
	}
	r.HostID = w.ID
	if err := s.Store.ApplySync(ctx, []model.Record{r}); err != nil {
		return fmt.Errorf("bind restore: %w", err)
	}
	var listed struct {
		Tabs []herdr.Tab `json:"tabs"`
	}
	if err := caller.Call(ctx, &listed, "tab", "list", "--workspace", w.ID); err != nil {
		return fmt.Errorf("list old tabs: %w", err)
	}
	for i, tab := range r.Snapshot.Tabs {
		var created creation
		if err := caller.Call(ctx, &created, "tab", "create", "--workspace", w.ID, "--cwd", first(tab.Root).CWD, "--label", tab.Label, "--no-focus"); err != nil {
			return fmt.Errorf("create tab %d: %w", i, err)
		}
		if i == 0 {
			for _, old := range listed.Tabs {
				if err := caller.Call(ctx, nil, "tab", "close", old.ID); err != nil {
					return fmt.Errorf("close old tab %s: %w", old.ID, err)
				}
			}
		}
		if err := expandRestore(ctx, caller, tab.Root, created.Root.ID); err != nil {
			return fmt.Errorf("expand tab %d: %w", i, err)
		}
	}
	if err := caller.Call(ctx, nil, "workspace", "rename", w.ID, r.Label); err != nil {
		return fmt.Errorf("rename restored workspace: %w", err)
	}
	if err := s.Store.CompleteRestore(ctx, r.ID, w.ID); err != nil {
		return fmt.Errorf("complete restore: %w", err)
	}
	return nil
}

func expandRestore(ctx context.Context, caller Caller, n *layout.Node, paneID string) error {
	if n.Type == layout.NodePane {
		if n.Label != "" {
			return caller.Call(ctx, nil, "pane", "rename", paneID, n.Label)
		}
		return nil
	}
	var split creation
	if err := caller.Call(ctx, &split, "pane", "split", paneID, "--direction", n.Direction, "--ratio", strconv.FormatFloat(n.Ratio, 'f', -1, 64), "--cwd", first(n.Second).CWD, "--no-focus"); err != nil {
		return err
	}
	if err := expandRestore(ctx, caller, n.First, paneID); err != nil {
		return err
	}
	return expandRestore(ctx, caller, n.Second, split.Pane.ID)
}
