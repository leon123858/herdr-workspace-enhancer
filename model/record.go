package model

import "context"

type WorkspaceStatus string

const (
	WorkspaceActive    WorkspaceStatus = "active"
	WorkspaceArchiving WorkspaceStatus = "archiving"
	WorkspaceArchived  WorkspaceStatus = "archived"
	WorkspaceClosed    WorkspaceStatus = "closed"
)

// Record keeps the same ID and note when restoration changes the Herdr ID.
type Record struct {
	ID        string          `json:"id"`
	HostID    string          `json:"host_id"`
	Label     string          `json:"label"`
	Status    WorkspaceStatus `json:"status"`
	Note      string          `json:"note"`
	IsManager bool            `json:"is_manager"`
	Snapshot  *Snapshot       `json:"snapshot,omitempty"`
}

// WorkspaceTx is valid only inside Transaction. Put updates workspace metadata;
// existing notes are only changed by Save, so sync cannot overwrite an editor.
type WorkspaceTx interface {
	All() ([]Record, error)
	Get(string) (Record, error)
	Put(Record) error
}
type WorkspaceStore interface {
	All(context.Context) ([]Record, error)
	ApplySync(context.Context, []Record) error
	ArchiveIntent(context.Context, string, string, *Snapshot) error
	CreateManager(context.Context, Record) error
	Get(string) (Record, error)
	List() ([]Record, error)
	IsManager(string) (bool, error)
	Load(string) (Note, error)
	Save(string, string) (Note, error)
	WaitLock(context.Context, string) (func(), error)
}
