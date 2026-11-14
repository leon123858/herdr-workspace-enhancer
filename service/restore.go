package service

import (
	"context"
	"fmt"
	"strconv"

	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/model/layout"
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

// Restore performs one attempt. A partial result remains active and is never
// replayed automatically. No SQL transaction spans a Herdr call.
func (s *Service) Restore(ctx context.Context, id string) (result error) {
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
	if r.IsManager {
		return fmt.Errorf("cannot restore manager workspace")
	}
	if r.Status != model.WorkspaceArchived {
		return fmt.Errorf("workspace is %s", r.Status)
	}
	if r.Snapshot == nil || len(r.Snapshot.Tabs) == 0 {
		return fmt.Errorf("snapshot is empty")
	}
	for _, tab := range r.Snapshot.Tabs {
		if err = tab.Root.Validate(true); err != nil {
			return err
		}
	}
	// Reconcile an earlier lost response before issuing a new create request.
	live, err := s.Host.Workspaces(ctx)
	if err != nil {
		return err
	}
	if live == nil {
		return fmt.Errorf("Herdr workspace scan is incomplete")
	}
	existing := ""
	seen := map[string]bool{}
	for _, w := range live {
		if w.ID == "" || seen[w.ID] {
			return fmt.Errorf("invalid Herdr workspace identity")
		}
		seen[w.ID] = true
		if w.ID == r.HostID || w.Label == restorePrefix+r.ID {
			if existing != "" {
				return fmt.Errorf("multiple restore candidates; inspect Herdr before retrying")
			}
			existing = w.ID
		}
	}
	if existing != "" {
		if _, err = s.refreshLocked(ctx); err != nil {
			return err
		}
		return fmt.Errorf("workspace already exists; focus it instead of restoring again")
	}
	defer func() {
		if result != nil {
			result = s.repairLocked(result)
			if current, err := s.Store.Get(id); err == nil && current.Status == model.WorkspaceActive {
				result = fmt.Errorf("workspace exists but restoration did not complete; inspect it before continuing: %w", result)
			}
		}
	}()
	var created creation
	err = caller.Call(ctx, &created, "workspace", "create", "--cwd", first(r.Snapshot.Tabs[0].Root).CWD, "--label", restorePrefix+r.ID, "--no-focus")
	if err != nil {
		return err
	}
	if created.Workspace.ID == "" {
		return fmt.Errorf("workspace creation returned no workspace ID; sync to reconnect it")
	}
	if created.Tab.ID == "" || created.Root.ID == "" {
		return fmt.Errorf("workspace created with incomplete tab/pane IDs; inspect it before further changes")
	}
	var expand func(*layout.Node, string) error
	expand = func(n *layout.Node, paneID string) error {
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
		if split.Pane.ID == "" {
			return fmt.Errorf("split returned no pane ID")
		}
		if err := expand(n.First, paneID); err != nil {
			return err
		}
		return expand(n.Second, split.Pane.ID)
	}
	for i, tab := range r.Snapshot.Tabs {
		tabID, paneID := created.Tab.ID, created.Root.ID
		if i > 0 {
			var next creation
			if err = caller.Call(ctx, &next, "tab", "create", "--workspace", created.Workspace.ID, "--cwd", first(tab.Root).CWD, "--label", tab.Label, "--no-focus"); err != nil {
				return err
			}
			tabID, paneID = next.Tab.ID, next.Root.ID
			if tabID == "" || paneID == "" {
				return fmt.Errorf("tab creation returned incomplete IDs")
			}
		}
		if err = caller.Call(ctx, nil, "tab", "rename", tabID, tab.Label); err != nil {
			return err
		}
		if err = expand(tab.Root, paneID); err != nil {
			return err
		}
	}
	return nil
}
