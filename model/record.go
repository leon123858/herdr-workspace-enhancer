package model

import "encoding/json"

type Record struct {
	ID       string    `json:"id"`
	HostID   string    `json:"host_id"`
	Label    string    `json:"label"`
	Status   string    `json:"status"`
	Snapshot *Snapshot `json:"snapshot,omitempty"`
	Journal  []Step    `json:"journal,omitempty"`
}
type Step struct {
	Before json.RawMessage `json:"before,omitempty"`
	Kind   string          `json:"kind"`
	Args   []string        `json:"args"`
	Result json.RawMessage `json:"result,omitempty"`
	Status string          `json:"status"`
}
type Database struct {
	Version int      `json:"version"`
	Records []Record `json:"records"`
}
