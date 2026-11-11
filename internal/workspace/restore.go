package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"herdr-workspace-enhancer/internal/herdr"
	"herdr-workspace-enhancer/internal/layout"
	"herdr-workspace-enhancer/internal/state"
	"reflect"
	"strconv"
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
	for n.Type == "split" {
		n = n.First
	}
	return n
}

// Restore replays completed journal entries without repeating their mutations.
// A pending entry is never blindly retried: the caller must reconcile its host
// outcome first. Snapshots remain intact throughout restoration.
func (s *Service) Restore(ctx context.Context, id string) error {
	if s.MutationError != nil {
		return s.MutationError
	}
	caller, ok := s.Host.(Caller)
	if !ok {
		return fmt.Errorf("host does not support restoration")
	}
	unlock, e := s.Store.WaitLock(ctx, "session")
	if e != nil {
		return e
	}
	defer unlock()
	db, e := s.Store.ReadDatabase()
	if e != nil {
		return e
	}
	var r *state.Record
	for i := range db.Records {
		if db.Records[i].ID == id {
			r = &db.Records[i]
			break
		}
	}
	if r == nil {
		return fmt.Errorf("workspace record not found")
	}
	if r.Status != "archived" && r.Status != "restoring" {
		return fmt.Errorf("workspace is %s", r.Status)
	}
	if r.Snapshot == nil || len(r.Snapshot.Tabs) == 0 {
		return fmt.Errorf("snapshot is empty")
	}
	for _, tab := range r.Snapshot.Tabs {
		if e = tab.Root.Validate(true); e != nil {
			return e
		}
	}
	if r.Status == "archived" {
		r.Journal = nil
		r.Status = "restoring"
		if e = s.Store.WriteDatabase(db); e != nil {
			return e
		}
	}
	index := 0
	call := func(dst any, args ...string) error {
		stepIndex := index
		index++
		if stepIndex < len(r.Journal) && r.Journal[stepIndex].Status == "rejected" {
			if stepIndex != len(r.Journal)-1 {
				return fmt.Errorf("invalid rejected journal position")
			}
			r.Journal = r.Journal[:stepIndex]
		}
		if stepIndex < len(r.Journal) {
			step := r.Journal[stepIndex]
			if !reflect.DeepEqual(step.Args, args) {
				return fmt.Errorf("restore journal does not match snapshot")
			}
			if step.Status != "done" {
				raw, err := reconcileCreation(ctx, caller, step)
				if err != nil {
					return fmt.Errorf("step %d: %w", stepIndex+1, err)
				}
				step.Result = raw
				step.Status = "done"
				r.Journal[stepIndex] = step
				if err = s.Store.WriteDatabase(db); err != nil {
					return err
				}
			}
			if dst != nil {
				return json.Unmarshal(step.Result, dst)
			}
			return nil
		}
		var before json.RawMessage
		if args[0] == "pane" && args[1] == "split" {
			var err error
			before, err = captureSplit(ctx, caller, args[2])
			if err != nil {
				return err
			}
		}
		r.Journal = append(r.Journal, state.Step{Kind: args[0] + " " + args[1], Args: args, Status: "pending", Before: before})
		if e := s.Store.WriteDatabase(db); e != nil {
			return e
		}
		var raw json.RawMessage
		if e := caller.Call(ctx, &raw, args...); e != nil {
			var apiErr *herdr.APIError
			if errors.As(e, &apiErr) && apiErr.Rejected() {
				r.Journal[stepIndex].Status = "rejected"
				if saveErr := s.Store.WriteDatabase(db); saveErr != nil {
					return fmt.Errorf("%v; journal save: %w", e, saveErr)
				}
			}
			return e
		}
		r.Journal[stepIndex].Result = raw
		r.Journal[stepIndex].Status = "done"
		if e := s.Store.WriteDatabase(db); e != nil {
			return e
		}
		if dst != nil {
			return json.Unmarshal(raw, dst)
		}
		return nil
	}
	root := first(r.Snapshot.Tabs[0].Root)
	var created creation
	if e = call(&created, "workspace", "create", "--cwd", root.CWD, "--label", "enhancer-restore-"+r.ID, "--no-focus"); e != nil {
		return e
	}
	if created.Workspace.ID == "" || created.Tab.ID == "" || created.Root.ID == "" {
		return fmt.Errorf("workspace creation returned incomplete IDs")
	}
	r.HostID = created.Workspace.ID
	if e = s.Store.WriteDatabase(db); e != nil {
		return e
	}
	var expand func(*layout.Node, string) error
	expand = func(n *layout.Node, paneID string) error {
		if n.Type == "pane" {
			if n.Label != "" {
				return call(nil, "pane", "rename", paneID, n.Label)
			}
			return nil
		}
		right := first(n.Second)
		var split creation
		if e := call(&split, "pane", "split", paneID, "--direction", n.Direction, "--ratio", strconv.FormatFloat(n.Ratio, 'f', -1, 64), "--cwd", right.CWD, "--no-focus"); e != nil {
			return e
		}
		if split.Pane.ID == "" {
			return fmt.Errorf("split returned no pane ID")
		}
		if e := expand(n.First, paneID); e != nil {
			return e
		}
		return expand(n.Second, split.Pane.ID)
	}
	for i, tab := range r.Snapshot.Tabs {
		tabID, paneID := created.Tab.ID, created.Root.ID
		if i > 0 {
			var next creation
			if e = call(&next, "tab", "create", "--workspace", r.HostID, "--cwd", first(tab.Root).CWD, "--label", fmt.Sprintf("enhancer-restore-%s-tab-%d", r.ID, i), "--no-focus"); e != nil {
				return e
			}
			tabID, paneID = next.Tab.ID, next.Root.ID
			if tabID == "" || paneID == "" {
				return fmt.Errorf("tab creation returned incomplete IDs")
			}
		}
		{
			if e = call(nil, "tab", "rename", tabID, tab.Label); e != nil {
				return e
			}
		}
		if e = expand(tab.Root, paneID); e != nil {
			return e
		}
	}
	if e = call(nil, "workspace", "rename", r.HostID, r.Label); e != nil {
		return e
	}
	r.Status = "active"
	return s.Store.WriteDatabase(db)
}
