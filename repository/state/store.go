// Package state persists session data in SQLite through GORM.
package state

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"herdr-workspace-enhancer/model"
)

type Note = model.Note

type Store struct {
	dir string
	db  *gorm.DB
}

// Open requires an explicit host-owned directory and session identity. Tables
// are created or extended by AutoMigrate; existing JSON files are not imported.
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
	s := &Store{dir: dir}
	// Serialize migrations across TUI/action/event processes, including first use.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	unlock, err := s.WaitLock(ctx, "schema")
	if err != nil {
		return nil, err
	}
	defer unlock()
	path := filepath.Join(dir, "state.sqlite")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	if len(u.Path) > 1 && u.Path[1] == ':' {
		u.Path = "/" + u.Path
	}
	q := url.Values{}
	q.Set("_txlock", "immediate")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(FULL)")
	u.RawQuery = q.Encode()
	db, err := gorm.Open(sqlite.Open(u.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	s.db = db
	pool, err := db.DB()
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	if err = db.AutoMigrate(&workspaceRow{}); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate session database: %w", err)
	}
	return s, nil
}
func (s *Store) Close() error {
	pool, err := s.db.DB()
	if err != nil {
		return err
	}
	return pool.Close()
}

// Shutdown lets the controller's DI container release SQLite connections.
func (s *Store) Shutdown() error   { return s.Close() }
func (s *Store) Directory() string { return s.dir }

func (s *Store) Load(id string) (Note, error) {
	var r workspaceRow
	err := s.db.Select("note").Where("id = ?", id).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Note{}, nil
	}
	return Note{Text: r.Note}, err
}

// Save only updates note text. Workspace metadata is never overwritten, and
// concurrent editors deliberately use last-writer-wins semantics.
func (s *Store) Save(id, text string) (Note, error) {
	if id == "" {
		return Note{}, fmt.Errorf("workspace record ID is required")
	}
	err := s.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"note"})}).Create(&workspaceRow{ID: id, Status: model.WorkspaceClosed, Note: text}).Error
	return Note{Text: text}, err
}
