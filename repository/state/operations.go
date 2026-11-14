package state

import (
	"context"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"herdr-workspace-enhancer/model"
)

func (s *Store) All(ctx context.Context) ([]Record, error) {
	return (transaction{s.db.WithContext(ctx)}).All()
}

// ArchiveIntent is one conditional write; a stale preview cannot close a new host.
func (s *Store) ArchiveIntent(ctx context.Context, id, host string, snapshot *model.Snapshot) error {
	value := row(Record{Status: model.WorkspaceArchiving, Snapshot: snapshot})
	result := s.db.WithContext(ctx).Model(&workspaceRow{}).
		Where("id = ? AND host_id = ? AND status = ? AND is_manager = ?", id, host, model.WorkspaceActive, false).
		Select("status", "snapshot").Updates(&value)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("workspace changed before archive intent was saved")
	}
	return nil
}

func (s *Store) CreateManager(ctx context.Context, r Record) error {
	if r.ID == "" || !r.IsManager || r.Status != model.WorkspaceClosed || r.HostID != "" {
		return fmt.Errorf("invalid manager intent")
	}
	value := row(r)
	return s.db.WithContext(ctx).Create(&value).Error
}

// ApplySync uses a single SQLite statement. Conflict updates deliberately exclude
// notes, including notes saved after the caller read the records.
func (s *Store) ApplySync(ctx context.Context, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	values := make([]workspaceRow, 0, len(records))
	seen := map[string]bool{}
	for _, r := range records {
		if r.ID == "" || seen[r.ID] {
			return fmt.Errorf("invalid or duplicate record identity")
		}
		seen[r.ID] = true
		switch r.Status {
		case model.WorkspaceActive, model.WorkspaceArchiving, model.WorkspaceArchived, model.WorkspaceClosed:
		default:
			return fmt.Errorf("invalid workspace status %q", r.Status)
		}
		values = append(values, row(r))
	}
	return s.db.WithContext(ctx).Session(&gorm.Session{SkipDefaultTransaction: true, CreateBatchSize: len(values)}).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"host_id", "label", "status", "is_manager", "snapshot"})}).Create(&values).Error
}
