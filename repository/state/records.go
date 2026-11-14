package state

import (
	"gorm.io/gorm"
	"herdr-workspace-enhancer/model"
)

type Record = model.Record

func allRecords(db *gorm.DB) ([]Record, error) {
	var rows []workspaceRow
	if err := db.Order("rowid").Find(&rows).Error; err != nil {
		return nil, err
	}
	records := make([]Record, 0, len(rows))
	for _, r := range rows {
		records = append(records, r.record())
	}
	return records, nil
}
func (s *Store) Get(id string) (Record, error) {
	var r workspaceRow
	err := s.db.Where("id = ?", id).Take(&r).Error
	return r.record(), err
}
func (s *Store) List() ([]Record, error) {
	return allRecords(s.db.Where("is_manager = ? AND status <> ?", false, model.WorkspaceClosed))
}
func (s *Store) IsManager(hostID string) (bool, error) {
	var count int64
	err := s.db.Model(&workspaceRow{}).Where("host_id = ? AND is_manager = ?", hostID, true).Count(&count).Error
	return count > 0, err
}
