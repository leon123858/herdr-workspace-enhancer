package state

import (
	"encoding/json"
	"fmt"
	"github.com/gofrs/flock"
	"os"
	"path/filepath"
)

// ManagerIDs includes the legacy singleton without changing or deleting it.
func (s *Store) ManagerIDs() (map[string]bool, error) {
	ids := map[string]bool{}
	b, err := os.ReadFile(filepath.Join(s.Dir, "manager-workspaces.json"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		if err = json.Unmarshal(b, &ids); err != nil {
			return nil, err
		}
	}
	if ids == nil {
		ids = map[string]bool{}
	}
	legacy, err := s.Load("manager-workspace-id")
	if err != nil {
		return nil, err
	}
	if legacy.Text != "" {
		ids[legacy.Text] = true
	}
	return ids, nil
}

// RegisterManager serializes concurrent processes without losing earlier IDs.
func (s *Store) RegisterManager(id string) error {
	if id == "" {
		return fmt.Errorf("manager workspace ID is empty")
	}
	lock := flock.New(filepath.Join(s.Dir, "manager.lock"))
	if err := lock.Lock(); err != nil {
		return err
	}
	defer lock.Close()
	ids, err := s.ManagerIDs()
	if err != nil {
		return err
	}
	ids[id] = true
	b, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return s.write(filepath.Join(s.Dir, "manager-workspaces.json"), b)
}
