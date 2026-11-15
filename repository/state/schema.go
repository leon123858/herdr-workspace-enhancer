package state

import "herdr-workspace-enhancer/model"

type workspaceRow struct {
	ID           string  `gorm:"primaryKey"`
	HostID       *string `gorm:"uniqueIndex"`
	Label        string
	Status       model.WorkspaceStatus `gorm:"not null;default:closed;index"`
	Note         string                `gorm:"not null;default:''"`
	ManagerState model.ManagerState    `gorm:"not null;default:NONE;index"`
	Snapshot     *model.Snapshot       `gorm:"serializer:json;type:text"`
}

func (workspaceRow) TableName() string { return "workspaces" }
func row(r model.Record) workspaceRow {
	if r.ManagerState == "" {
		r.ManagerState = model.ManagerNone
	}
	var host *string
	if r.HostID != "" {
		id := r.HostID
		host = &id
	}
	return workspaceRow{ID: r.ID, HostID: host, Label: r.Label, Status: r.Status, Note: r.Note, ManagerState: r.ManagerState, Snapshot: r.Snapshot}
}
func (r workspaceRow) record() model.Record {
	host := ""
	if r.HostID != nil {
		host = *r.HostID
	}
	return model.Record{ID: r.ID, HostID: host, Label: r.Label, Status: r.Status, Note: r.Note, ManagerState: r.ManagerState, Snapshot: r.Snapshot}
}
