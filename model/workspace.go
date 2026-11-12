package model

import "herdr-workspace-enhancer/model/layout"

type Workspace struct {
	ID     string `json:"workspace_id"`
	Label  string `json:"label"`
	Number int    `json:"number"`
}
type Tab struct {
	ID     string `json:"tab_id"`
	Label  string `json:"label"`
	Number int    `json:"number"`
}
type Pane struct {
	WorkspaceID   string `json:"workspace_id"`
	TerminalID    string `json:"terminal_id"`
	ID            string `json:"pane_id"`
	TabID         string `json:"tab_id"`
	CWD           string `json:"cwd"`
	ForegroundCWD string `json:"foreground_cwd"`
	Label         string `json:"label"`
}
type SavedTab struct {
	SourceTabID string       `json:"source_tab_id,omitempty"`
	Label       string       `json:"label"`
	Root        *layout.Node `json:"root"`
}
type Snapshot struct {
	Label string     `json:"label"`
	Tabs  []SavedTab `json:"tabs"`
}
