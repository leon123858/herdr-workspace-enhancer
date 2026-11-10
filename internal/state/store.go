// Package state stores plugin data outside project directories.
package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

var ErrConflict = errors.New("note changed in another editor; reload before saving")

type Note struct {
	Revision uint64 `json:"revision"`
	Text     string `json:"text"`
}

type Store struct{ Dir string }

// Open requires an explicit host-owned state directory and session identity.
func Open(root, session string) (*Store, error) {
	if root == "" || session == "" {
		return nil, errors.New("plugin state directory and session identity are required")
	}
	if !filepath.IsAbs(root) {
		return nil, errors.New("state directory must be absolute")
	}
	sum := sha256.Sum256([]byte(session))
	dir := filepath.Join(root, "sessions", hex.EncodeToString(sum[:]))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Store{Dir: dir}, nil
}
func (s *Store) path(id string) string {
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(s.Dir, hex.EncodeToString(sum[:])+".json")
}
func (s *Store) Load(id string) (Note, error) {
	var n Note
	b, err := os.ReadFile(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return n, nil
	}
	if err != nil {
		return n, err
	}
	err = json.Unmarshal(b, &n)
	return n, err
}

// Save serializes edits and rejects stale revisions.
func (s *Store) Save(id string, expected uint64, text string) (Note, error) {
	path := s.path(id)
	unlock, err := s.Lock(filepath.Base(path))
	if err != nil {
		return Note{}, err
	}
	defer unlock()

	old, err := s.Load(id)
	if err != nil {
		return Note{}, err
	}
	if old.Revision != expected {
		return Note{}, ErrConflict
	}
	n := Note{Revision: expected + 1, Text: text}
	b, err := json.Marshal(n)
	if err != nil {
		return Note{}, err
	}
	if err = s.write(path, b); err != nil {
		return Note{}, err
	}
	return n, nil
}
