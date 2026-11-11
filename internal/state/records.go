package state

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gofrs/flock"
	"herdr-workspace-enhancer/internal/herdr"
	"os"
	"path/filepath"
	"time"
)

type Record struct {
	ID       string          `json:"id"`
	HostID   string          `json:"host_id"`
	Label    string          `json:"label"`
	Status   string          `json:"status"`
	Snapshot *herdr.Snapshot `json:"snapshot,omitempty"`
	Journal  []Step          `json:"journal,omitempty"`
}
type Step struct {
	Before json.RawMessage `json:"before,omitempty"`
	Kind   string          `json:"kind"`
	Args   []string        `json:"args"`
	Result json.RawMessage `json:"result,omitempty"`
	Status string          `json:"status"`
}
type Database struct {
	Version int      `json:"version"`
	Records []Record `json:"records"`
}

func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func (s *Store) Lock(name string) (func(), error) {
	l := flock.New(filepath.Join(s.Dir, name+".lock"))
	ok, err := l.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("another operation is in progress")
	}
	return func() { _ = l.Close() }, nil
}

// WaitLock lets event handlers wait for archive/restore and manager creation
// instead of dropping notifications when another process owns the session.
func (s *Store) WaitLock(ctx context.Context, name string) (func(), error) {
	l := flock.New(filepath.Join(s.Dir, name+".lock"))
	ok, err := l.TryLockContext(ctx, 25*time.Millisecond)
	if err != nil {
		_ = l.Close()
		return nil, err
	}
	if !ok {
		_ = l.Close()
		return nil, ctx.Err()
	}
	return func() { _ = l.Close() }, nil
}
func (s *Store) ReadDatabase() (Database, error) {
	db := Database{Version: 1}
	b, err := os.ReadFile(filepath.Join(s.Dir, "workspaces.json"))
	if errors.Is(err, os.ErrNotExist) {
		return db, nil
	}
	if err != nil {
		return db, err
	}
	if err = json.Unmarshal(b, &db); err != nil {
		return db, err
	}
	if db.Version != 1 {
		return db, fmt.Errorf("unsupported data version %d", db.Version)
	}
	seen := map[string]bool{}
	for _, r := range db.Records {
		if r.ID == "" || seen[r.ID] {
			return db, fmt.Errorf("invalid workspace identity")
		}
		seen[r.ID] = true
	}
	return db, nil
}

// WriteDatabase must be called while holding the session operation lock.
func (s *Store) WriteDatabase(db Database) error {
	b, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return err
	}
	return s.write(filepath.Join(s.Dir, "workspaces.json"), b)
}
func (s *Store) write(path string, b []byte) error {
	f, err := os.CreateTemp(s.Dir, ".write-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return replace(tmp, path)
}
