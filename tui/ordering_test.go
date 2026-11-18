package tui

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	domain "herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/repository/state"
	"herdr-workspace-enhancer/service"
	"testing"
)

func TestKeyboardOrderAndStalePoll(t *testing.T) {
	store, err := state.Open(t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rows := []domain.Record{{ID: "a", Status: domain.WorkspaceActive}, {ID: "hidden", Status: domain.WorkspaceArchived}, {ID: "b", Status: domain.WorkspaceActive}}
	if err := store.ApplySync(context.Background(), rows); err != nil {
		t.Fatal(err)
	}
	m := NewUI(&service.Service{Store: store}, nil, nil).(model)
	m.records = rows
	m.filter = FilterActive
	next, cmd := m.Update(runeKey("J"))
	m = next.(model)
	if !m.busy || cmd == nil {
		t.Fatal("move not asynchronous")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	if m.busy || m.records[0].ID != "hidden" || m.records[2].ID != "a" {
		t.Fatal(m.records, m.status)
	}
	next, _ = m.Update(storedRows{records: rows})
	m = next.(model)
	if m.records[2].ID != "a" {
		t.Fatal("stale poll replaced order")
	}
	next, cmd = m.Update(runeKey("J"))
	if cmd != nil {
		t.Fatal("boundary move")
	}
	m = next.(model)
	m.recordDrag = "a"
	m.dragActive = true
	m.dragToken = 5
	m.dragY = 999
	next, cmd = m.Update(dragTick{token: 5})
	m = next.(model)
	if cmd == nil {
		t.Fatal("scroll timer stopped")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if m.recordDrag != "" {
		t.Fatal("cancel failed")
	}
	next, cmd = m.Update(dragTick{token: 5})
	if cmd != nil {
		t.Fatal("cancelled timer restarted")
	}

}

type moveOnlyStore struct {
	domain.WorkspaceStore
	lists int
}

func (s *moveOnlyStore) ListSortedRecords(ctx context.Context) ([]domain.Record, error) {
	s.lists++
	return s.WorkspaceStore.ListSortedRecords(ctx)
}
func TestMoveUsesReturnedRecords(t *testing.T) {
	store, err := state.Open(t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rows := []domain.Record{{ID: "a", Status: domain.WorkspaceActive}, {ID: "b", Status: domain.WorkspaceActive}}
	if err := store.ApplySync(context.Background(), rows); err != nil {
		t.Fatal(err)
	}
	wrapped := &moveOnlyStore{WorkspaceStore: store}
	m := NewUI(&service.Service{Store: wrapped}, nil, nil).(model)
	result := m.moveRecord("b", "a", true)().(reordered)
	if result.err != nil || len(result.records) != 2 || result.records[0].ID != "b" || wrapped.lists != 0 {
		t.Fatal(result, wrapped.lists)
	}
}
