package model

import "context"

type ManagerState string

const (
	ManagerNone ManagerState = "NONE"
	ManagerInit ManagerState = "INIT"
	ManagerDone ManagerState = "DONE"
)

type WorkspaceStatus string

const (
	WorkspaceActive    WorkspaceStatus = "active"
	WorkspaceArchiving WorkspaceStatus = "archiving"
	WorkspaceArchived  WorkspaceStatus = "archived"
	WorkspaceClosed    WorkspaceStatus = "closed"
)

// Record keeps the same ID and note when restoration changes the Herdr ID.
type Record struct {
	ID           string          `json:"id"`
	HostID       string          `json:"host_id"`
	Label        string          `json:"label"`
	Status       WorkspaceStatus `json:"status"`
	Note         string          `json:"note"`
	ManagerState ManagerState    `json:"manager_state"`
	Snapshot     *Snapshot       `json:"snapshot,omitempty"`
}

type WorkspaceStore interface {
	MoveRecord(context.Context, string, string, bool) ([]Record, error)
	ListNonClosedRecords(context.Context) ([]Record, error)
	FindHost(context.Context, string) (Record, error)
	CompleteRestore(context.Context, string, string) error
	DeleteArchived(context.Context, string, string) error
	ApplySync(context.Context, []Record) error
	ArchiveIntent(context.Context, string, string, *Snapshot) error
	CreateManager(context.Context, Record) error
	CompleteManager(context.Context, string, string) error
	Get(string) (Record, error)
	ListSortedRecords(context.Context) ([]Record, error)
	IsManager(string) (bool, error)
	Load(string) (Note, error)
	Save(string, string) (Note, error)
	WaitLock(context.Context, string) (func(), error)
}
