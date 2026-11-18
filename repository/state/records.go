package state

import (
	"container/list"
	"context"
	"fmt"
	"gorm.io/gorm"
	"herdr-workspace-enhancer/model"
	"sort"
)

type Record = model.Record

func recordsFromRows(rows []workspaceRow) []Record {
	records := make([]Record, 0, len(rows))
	for _, r := range rows {
		records = append(records, r.record())
	}
	return records
}

func listSortableRecords(tx *gorm.DB) ([]workspaceRow, error) {
	var rows []workspaceRow
	err := tx.Where("status <> ? AND manager_state = ?", model.WorkspaceClosed, model.ManagerNone).Order("rowid").Find(&rows).Error
	return rows, err
}

func sortRecords(rows []workspaceRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].SortOrder == nil {
			return false
		}
		return rows[j].SortOrder == nil || *rows[i].SortOrder < *rows[j].SortOrder
	})
}

func updateRecordSortedIdx(tx *gorm.DB, rows []workspaceRow) error {
	for i, r := range rows {
		if r.SortOrder != nil && *r.SortOrder == i {
			continue
		}
		if err := tx.Model(&workspaceRow{}).Where("id = ?", r.ID).UpdateColumn("sort_order", i).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListSortedRecords(ctx context.Context) ([]Record, error) {
	var rows []workspaceRow
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		rows, err = listSortableRecords(tx)
		if err != nil {
			return err
		}
		sortRecords(rows)
		return updateRecordSortedIdx(tx, rows)
	})
	if err != nil {
		return nil, err
	}
	return recordsFromRows(rows), nil
}
func (s *Store) Get(id string) (Record, error) {
	var r workspaceRow
	err := s.db.Where("id = ?", id).Take(&r).Error
	return r.record(), err
}
func (s *Store) IsManager(hostID string) (bool, error) {
	var count int64
	err := s.db.Model(&workspaceRow{}).Where("host_id = ? AND manager_state <> ?", hostID, model.ManagerNone).Count(&count).Error
	return count > 0, err
}

// MoveRecord changes only the source position in the latest stored ordering.
func (s *Store) MoveRecord(ctx context.Context, source, target string, before bool) ([]Record, error) {
	var records []workspaceRow
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		records, err = listSortableRecords(tx)
		if err != nil {
			return err
		}
		sortRecords(records)
		linked := listToLinkList(records)
		var sourceElement, targetElement *list.Element
		for element := linked.Front(); element != nil; element = element.Next() {
			r := element.Value.(workspaceRow)
			if r.ID == source {
				sourceElement = element
			}
			if r.ID == target {
				targetElement = element
			}
		}
		if sourceElement == nil || targetElement == nil {
			return fmt.Errorf("record is no longer available")
		}
		if sourceElement == targetElement {
			return updateRecordSortedIdx(tx, records)
		}
		if before {
			linked.MoveBefore(sourceElement, targetElement)
		} else {
			linked.MoveAfter(sourceElement, targetElement)
		}
		records = linkListToList(linked)
		return updateRecordSortedIdx(tx, records)
	})
	if err != nil {
		return nil, err
	}
	return recordsFromRows(records), nil
}

func listToLinkList(rows []workspaceRow) *list.List {
	linked := list.New()
	for _, row := range rows {
		linked.PushBack(row)
	}
	return linked
}

func linkListToList(linked *list.List) []workspaceRow {
	rows := make([]workspaceRow, 0, linked.Len())
	for element := linked.Front(); element != nil; element = element.Next() {
		rows = append(rows, element.Value.(workspaceRow))
	}
	return rows
}
