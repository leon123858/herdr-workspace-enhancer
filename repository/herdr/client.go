// Package herdr wraps the host CLI without shell interpolation.
package herdr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/model/layout"
	"os/exec"
	"time"
)

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

type Runner func(context.Context, ...string) ([]byte, error)
type Client struct{ Run Runner }

func New(binary string) *Client {
	return &Client{Run: func(ctx context.Context, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			var response struct {
				Error *APIError `json:"error"`
			}
			if json.Unmarshal(stderr.Bytes(), &response) == nil && response.Error != nil {
				return nil, response.Error
			}
			return nil, fmt.Errorf("herdr %v: %w: %s", args, err, stderr.String())
		}
		return out, nil
	}}
}
func (c *Client) Call(ctx context.Context, dst any, args ...string) error {
	out, err := c.Run(ctx, args...)
	if err != nil {
		return err
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err = json.Unmarshal(out, &envelope); err != nil {
		return fmt.Errorf("invalid Herdr JSON: %w", err)
	}
	if envelope.Error != nil {
		return &APIError{Code: envelope.Error.Code, Message: envelope.Error.Message}
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return fmt.Errorf("Herdr response lacks result")
	}
	if dst == nil {
		return nil
	}
	return json.Unmarshal(envelope.Result, dst)
}

type Workspace = model.Workspace
type Tab = model.Tab
type Pane = model.Pane
type SavedTab = model.SavedTab
type Snapshot = model.Snapshot

func (c *Client) Workspaces(ctx context.Context) ([]Workspace, error) {
	var r struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	err := c.Call(ctx, &r, "workspace", "list")
	if err != nil {
		return nil, err
	}
	if r.Workspaces == nil {
		return nil, fmt.Errorf("Herdr workspace list is missing or null")
	}
	seen := map[string]bool{}
	for _, w := range r.Workspaces {
		if w.ID == "" || seen[w.ID] {
			return nil, fmt.Errorf("invalid workspace list identity")
		}
		seen[w.ID] = true
	}
	return r.Workspaces, nil
}
func (c *Client) Focus(ctx context.Context, id string) error {
	return c.Call(ctx, nil, "workspace", "focus", id)
}
func (c *Client) Close(ctx context.Context, id string) error {
	return c.Call(ctx, nil, "workspace", "close", id)
}
func (c *Client) Snapshot(ctx context.Context, w Workspace) (Snapshot, error) {
	result := Snapshot{Label: w.Label}
	var tabs struct {
		Tabs []Tab `json:"tabs"`
	}
	if e := c.Call(ctx, &tabs, "tab", "list", "--workspace", w.ID); e != nil {
		return result, e
	}
	var panes struct {
		Panes []Pane `json:"panes"`
	}
	if e := c.Call(ctx, &panes, "pane", "list", "--workspace", w.ID); e != nil {
		return result, e
	}
	for _, tab := range tabs.Tabs {
		byID := map[string]Pane{}
		first := ""
		for _, p := range panes.Panes {
			if p.TabID == tab.ID {
				byID[p.ID] = p
				if first == "" {
					first = p.ID
				}
			}
		}
		if first == "" {
			return result, fmt.Errorf("tab %s has no panes", tab.ID)
		}
		var r struct {
			Layout struct {
				Splits []layout.Split `json:"splits"`
				Zoomed bool           `json:"zoomed"`
				Panes  []layout.Pane  `json:"panes"`
			} `json:"layout"`
		}
		if e := c.Call(ctx, &r, "pane", "layout", "--pane", first); e != nil {
			return result, e
		}
		if r.Layout.Zoomed {
			return result, fmt.Errorf("unzoom tab %s before archiving", tab.Label)
		}
		if len(r.Layout.Panes) != len(byID) {
			return result, fmt.Errorf("layout changed during capture")
		}
		seen := map[string]bool{}
		for i := range r.Layout.Panes {
			p := &r.Layout.Panes[i]
			info, ok := byID[p.ID]
			if !ok || seen[p.ID] {
				return result, fmt.Errorf("inconsistent pane identity")
			}
			seen[p.ID] = true
			p.CWD = info.ForegroundCWD
			if p.CWD == "" {
				p.CWD = info.CWD
			}
			p.Label = info.Label
			p.TerminalID = info.TerminalID
		}
		root, e := layout.ExactTree(r.Layout.Panes, r.Layout.Splits)
		if e != nil {
			return result, e
		}
		if e = root.Validate(true); e != nil {
			return result, e
		}
		result.Tabs = append(result.Tabs, SavedTab{SourceTabID: tab.ID, Label: tab.Label, Root: root})
	}
	if len(result.Tabs) == 0 {
		return result, fmt.Errorf("workspace has no tabs")
	}
	return result, nil
}
