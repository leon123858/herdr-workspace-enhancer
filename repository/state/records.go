package state

import (
	"context"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"herdr-workspace-enhancer/model"
)

type Record = model.Record

type transaction struct{ db *gorm.DB }

func (s *Store) Transaction(ctx context.Context, fn func(model.WorkspaceTx) error) error {
	return s.db.WithContext(ctx).Transaction(func(db *gorm.DB) error { return fn(transaction{db}) })
}
func (t transaction) All() ([]Record, error) {
	var rows []workspaceRow
	if err := t.db.Order("rowid").Find(&rows).Error; err != nil {
		return nil, err
	}
	records := make([]Record, 0, len(rows))
	for _, r := range rows {
		records = append(records, r.record())
	}
	return records, nil
}
func (t transaction) Get(id string) (Record, error) {
	var r workspaceRow
	err := t.db.Where("id = ?", id).Take(&r).Error
	return r.record(), err
}
func (t transaction) Put(r Record) error {
	if r.ID == "" {
		return fmt.Errorf("workspace record ID is required")
	}
	switch r.Status {
	case model.WorkspaceActive, model.WorkspaceArchiving, model.WorkspaceArchived, model.WorkspaceClosed:
	default:
		return fmt.Errorf("invalid workspace status %q", r.Status)
	}
	value := row(r)
	return t.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"host_id", "label", "status", "is_manager", "snapshot"})}).Create(&value).Error
}
func (s *Store) Get(id string) (Record, error) { return (transaction{s.db}).Get(id) }
func (s *Store) List() ([]Record, error) {
	return (transaction{s.db.Where("is_manager = ? AND status <> ?", false, model.WorkspaceClosed)}).All()
}
func (s *Store) IsManager(hostID string) (bool, error) {
	var count int64
	err := s.db.Model(&workspaceRow{}).Where("host_id = ? AND is_manager = ?", hostID, true).Count(&count).Error
	return count > 0, err
}
