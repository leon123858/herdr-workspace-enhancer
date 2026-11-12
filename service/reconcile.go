package service

import (
	"context"
	"encoding/json"
	"fmt"
	state "herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/repository/herdr"
)

func argument(args []string, key string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key {
			return args[i+1]
		}
	}
	return ""
}

// reconcileCreation finds only the unique operation label, never a project name
// or cwd. An absent or ambiguous result stays pending and is not retried.
func reconcileCreation(ctx context.Context, c Caller, step state.Step) (json.RawMessage, error) {
	args := step.Args
	if len(args) < 2 {
		return nil, fmt.Errorf("invalid journal arguments")
	}
	kind := args[0] + " " + args[1]
	if kind == "pane split" {
		return reconcileSplit(ctx, c, step)
	}
	if kind == "workspace create" {
		var list struct {
			Workspaces []herdr.Workspace `json:"workspaces"`
		}
		if e := c.Call(ctx, &list, "workspace", "list"); e != nil {
			return nil, e
		}
		var matches []herdr.Workspace
		for _, w := range list.Workspaces {
			if w.Label == argument(args, "--label") {
				matches = append(matches, w)
			}
		}
		if len(matches) != 1 {
			return nil, fmt.Errorf("cannot uniquely identify pending workspace creation")
		}
		var tabs struct {
			Tabs []herdr.Tab `json:"tabs"`
		}
		if e := c.Call(ctx, &tabs, "tab", "list", "--workspace", matches[0].ID); e != nil {
			return nil, e
		}
		if len(tabs.Tabs) != 1 {
			return nil, fmt.Errorf("pending workspace has unexpected tabs")
		}
		var panes struct {
			Panes []herdr.Pane `json:"panes"`
		}
		if e := c.Call(ctx, &panes, "pane", "list", "--workspace", matches[0].ID); e != nil {
			return nil, e
		}
		if len(panes.Panes) != 1 || panes.Panes[0].TabID != tabs.Tabs[0].ID {
			return nil, fmt.Errorf("pending workspace has unexpected panes")
		}
		return json.Marshal(map[string]any{"workspace": matches[0], "tab": tabs.Tabs[0], "root_pane": panes.Panes[0]})
	}
	if kind == "tab create" {
		var tabs struct {
			Tabs []herdr.Tab `json:"tabs"`
		}
		if e := c.Call(ctx, &tabs, "tab", "list", "--workspace", argument(args, "--workspace")); e != nil {
			return nil, e
		}
		var found []herdr.Tab
		for _, t := range tabs.Tabs {
			if t.Label == argument(args, "--label") {
				found = append(found, t)
			}
		}
		if len(found) != 1 {
			return nil, fmt.Errorf("cannot uniquely identify pending tab creation")
		}
		var panes struct {
			Panes []herdr.Pane `json:"panes"`
		}
		if e := c.Call(ctx, &panes, "pane", "list", "--workspace", argument(args, "--workspace")); e != nil {
			return nil, e
		}
		var selected []herdr.Pane
		for _, p := range panes.Panes {
			if p.TabID == found[0].ID {
				selected = append(selected, p)
			}
		}
		if len(selected) != 1 {
			return nil, fmt.Errorf("pending tab has unexpected panes")
		}
		return json.Marshal(map[string]any{"tab": found[0], "root_pane": selected[0]})
	}
	// Renames are idempotent and refer to durable, recorded identifiers.
	if args[1] == "rename" {
		var raw json.RawMessage
		if e := c.Call(ctx, &raw, args...); e != nil {
			return nil, e
		}
		return raw, nil
	}
	return nil, fmt.Errorf("pending %s needs inspection before retrying", kind)
}

type splitBefore struct {
	Target herdr.Pane   `json:"target"`
	Panes  []herdr.Pane `json:"panes"`
}

func captureSplit(ctx context.Context, c Caller, id string) (json.RawMessage, error) {
	var target struct {
		Pane herdr.Pane `json:"pane"`
	}
	if e := c.Call(ctx, &target, "pane", "get", id); e != nil {
		return nil, e
	}
	if target.Pane.ID != id || target.Pane.WorkspaceID == "" || target.Pane.TabID == "" {
		return nil, fmt.Errorf("split target identity is incomplete")
	}
	var list struct {
		Panes []herdr.Pane `json:"panes"`
	}
	if e := c.Call(ctx, &list, "pane", "list", "--workspace", target.Pane.WorkspaceID); e != nil {
		return nil, e
	}
	return json.Marshal(splitBefore{Target: target.Pane, Panes: list.Panes})
}
func reconcileSplit(ctx context.Context, c Caller, step state.Step) (json.RawMessage, error) {
	var before splitBefore
	if e := json.Unmarshal(step.Before, &before); e != nil {
		return nil, fmt.Errorf("missing split baseline: %w", e)
	}
	if len(before.Panes) == 0 || before.Target.WorkspaceID == "" {
		return nil, fmt.Errorf("invalid split baseline")
	}
	var now struct {
		Panes []herdr.Pane `json:"panes"`
	}
	if e := c.Call(ctx, &now, "pane", "list", "--workspace", before.Target.WorkspaceID); e != nil {
		return nil, e
	}
	old := map[string]herdr.Pane{}
	for _, p := range before.Panes {
		old[p.ID] = p
	}
	var added []herdr.Pane
	for _, p := range now.Panes {
		if prev, ok := old[p.ID]; ok {
			if p.TerminalID != prev.TerminalID || p.TabID != prev.TabID {
				return nil, fmt.Errorf("original pane changed during recovery")
			}
			delete(old, p.ID)
		} else {
			added = append(added, p)
		}
	}
	if len(old) != 0 || len(added) != 1 {
		return nil, fmt.Errorf("split outcome is not uniquely identifiable; keep snapshot and inspect workspace")
	}
	p := added[0]
	if p.TabID != before.Target.TabID || p.CWD != argument(step.Args, "--cwd") {
		return nil, fmt.Errorf("new pane does not match pending split")
	}
	return json.Marshal(map[string]any{"pane": p})
}
