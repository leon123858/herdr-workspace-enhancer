// Package service coordinates plugin state with Herdr operations.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/repository/herdr"
)

type Host interface {
	Workspaces(context.Context) ([]herdr.Workspace, error)
	Snapshot(context.Context, herdr.Workspace) (herdr.Snapshot, error)
	Close(context.Context, string) error
}
type Store = model.WorkspaceStore

type Service struct {
	Store         Store
	Host          Host
	MutationError error
	ManagerID     string
}

const restorePrefix = "enhancer-restore-"
const managerPrefix = "enhancer-manager-"

func (s *Service) StoredRows(ctx context.Context) ([]model.Record, error) {
	return s.Store.ListSortedRecords(ctx)
}
func (s *Service) Load(id string) (model.Note, error)       { return s.Store.Load(id) }
func (s *Service) Save(id, text string) (model.Note, error) { return s.Store.Save(id, text) }

func newID() (string, error) {
	var b [16]byte
	_, err := rand.Read(b[:])
	return hex.EncodeToString(b[:]), err
}

func (s *Service) MoveRecord(ctx context.Context, source, target string, before bool) ([]model.Record, error) {
	return s.Store.MoveRecord(ctx, source, target, before)
}
