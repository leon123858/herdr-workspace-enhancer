package service

import (
	"context"
	"fmt"
	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/repository/herdr"
	"sort"
)

// initializeManager runs under the session lock. INIT panes are disposable;
// only a durable DONE prevents another initialization attempt.
func (s *Service) initializeManager(ctx context.Context, r model.Record) error {
	host, ok := s.Host.(Caller)
	if !ok {
		return fmt.Errorf("host does not support manager initialization")
	}
	var listed struct {
		Panes []herdr.Pane `json:"panes"`
	}
	if err := host.Call(ctx, &listed, "pane", "list", "--workspace", r.HostID); err != nil {
		return err
	}
	if len(listed.Panes) == 0 {
		return fmt.Errorf("manager has no panes")
	}
	seen := map[string]bool{}
	for _, p := range listed.Panes {
		if p.ID == "" || p.WorkspaceID != r.HostID || seen[p.ID] {
			return fmt.Errorf("invalid manager pane list")
		}
		seen[p.ID] = true
	}
	sort.Slice(listed.Panes, func(i, j int) bool { return listed.Panes[i].ID < listed.Panes[j].ID })
	retained := listed.Panes[0].ID
	for _, p := range listed.Panes[1:] {
		if err := host.Call(ctx, nil, "pane", "close", p.ID); err != nil {
			return fmt.Errorf("reset manager panes: %w", err)
		}
	}
	var opened struct {
		PluginPane struct {
			Pane herdr.Pane `json:"pane"`
		} `json:"plugin_pane"`
	}
	if err := host.Call(ctx, &opened, "plugin", "pane", "open", "--plugin", "workspace.enhancer", "--entrypoint", "manager", "--placement", "split", "--target-pane", retained, "--no-focus"); err != nil {
		return fmt.Errorf("open manager pane: %w", err)
	}
	p := opened.PluginPane.Pane
	if p.ID == "" || seen[p.ID] || p.WorkspaceID != r.HostID {
		return fmt.Errorf("invalid new manager pane")
	}
	if err := host.Call(ctx, nil, "pane", "close", retained); err != nil {
		return fmt.Errorf("clean up retained pane: %w", err)
	}
	if err := s.Store.CompleteManager(ctx, r.ID, r.HostID); err != nil {
		return err
	}
	if err := host.Call(ctx, nil, "workspace", "focus", r.HostID); err != nil {
		return fmt.Errorf("focus completed manager: %w", err)
	}
	return nil
}
