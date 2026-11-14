// Package controller owns CLI commands and initializes their dependencies.
package controller

import (
	"fmt"
	"herdr-workspace-enhancer/repository/config"
	"herdr-workspace-enhancer/repository/herdr"
	"herdr-workspace-enhancer/repository/state"
	"herdr-workspace-enhancer/service"
	"os"
	"path/filepath"

	"github.com/samber/do/v2"
)

type Controller struct{ injector do.Injector }

// New initializes an independent container. Providers remain lazy so help and
// command validation do not need a Herdr session or touch plugin state.
func New() *Controller {
	c := &Controller{}
	c.init()
	return c
}

func (c *Controller) init() {
	i := do.New()
	c.injector = i
	do.ProvideValue(i, &service.SettingsService{Repository: &config.Store{Path: filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "config.toml")}})
	do.Provide(i, func(do.Injector) (*herdr.Client, error) {
		b := os.Getenv("HERDR_BIN_PATH")
		if b == "" || os.Getenv("HERDR_SOCKET_PATH") == "" || os.Getenv("HERDR_PLUGIN_STATE_DIR") == "" {
			return nil, fmt.Errorf("launch through Herdr plugin context; required environment is missing")
		}
		return herdr.New(b), nil
	})
	do.Provide(i, func(do.Injector) (*state.Store, error) {
		return state.Open(os.Getenv("HERDR_PLUGIN_STATE_DIR"), os.Getenv("HERDR_SOCKET_PATH"))
	})
	do.Provide(i, func(i do.Injector) (*service.Service, error) {
		c, e := do.Invoke[*herdr.Client](i)
		if e != nil {
			return nil, e
		}
		s, e := do.Invoke[*state.Store](i)
		if e != nil {
			return nil, e
		}
		return &service.Service{Store: s, Host: c}, nil
	})
}

// Shutdown releases resolved dependencies, including the session SQLite pool.
func (c *Controller) Shutdown() error {
	report := c.injector.Shutdown()
	if len(report.Errors) > 0 {
		return report
	}
	return nil
}
