// Package config persists plugin preferences independently of session data.
package config

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"github.com/spf13/cast"
	"github.com/spf13/viper"
	"herdr-workspace-enhancer/model"
)

type Store struct{ Path string }

func valid(r float64) bool { return !math.IsNaN(r) && !math.IsInf(r, 0) && r >= .1 && r <= .9 }
func (s *Store) LoadRatio() (float64, error) {
	v := viper.New()
	v.SetConfigFile(s.Path)
	if err := v.ReadInConfig(); err != nil {
		if os.IsNotExist(err) {
			return model.DefaultWorkspaceRatio, nil
		}
		return model.DefaultWorkspaceRatio, err
	}
	if !v.IsSet("tui.workspace_ratio") {
		return model.DefaultWorkspaceRatio, nil
	}
	r, err := cast.ToFloat64E(v.Get("tui.workspace_ratio"))
	if err != nil || !valid(r) {
		return model.DefaultWorkspaceRatio, fmt.Errorf("tui.workspace_ratio must be a number between 0.1 and 0.9")
	}
	return r, nil
}
func (s *Store) SaveRatio(r float64) error {
	if !valid(r) {
		return fmt.Errorf("tui.workspace_ratio must be a number between 0.1 and 0.9")
	}
	if !filepath.IsAbs(s.Path) {
		return fmt.Errorf("plugin config path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	l := flock.New(s.Path + ".lock")
	defer l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ok, err := l.TryLockContext(ctx, 10*time.Millisecond)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("plugin config is busy; try again")
	}
	v := viper.New()
	v.SetConfigFile(s.Path)
	if err := v.ReadInConfig(); err != nil && !os.IsNotExist(err) {
		return err
	}
	v.Set("tui.workspace_ratio", r)
	f, err := os.CreateTemp(filepath.Dir(s.Path), "config-*.toml")
	if err != nil {
		return err
	}
	name := f.Name()
	if err = f.Close(); err != nil {
		os.Remove(name)
		return err
	}
	defer os.Remove(name)
	if err = v.WriteConfigAs(name); err != nil {
		return err
	}
	f, err = os.OpenFile(name, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return replace(name, s.Path)
}
