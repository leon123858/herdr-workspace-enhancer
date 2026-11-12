package bootstrap

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/samber/do/v2"
	"herdr-workspace-enhancer/repository/herdr"
	"herdr-workspace-enhancer/repository/state"
	"herdr-workspace-enhancer/service"
	"herdr-workspace-enhancer/tui"
	"os"
	"time"
)

func New() *do.RootScope {
	i := do.New()
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
	do.Provide(i, func(i do.Injector) (*Controller, error) { return &Controller{injector: i}, nil })
	return i
}

type Controller struct{ injector do.Injector }

func (controller *Controller) Run(ctx context.Context, name string) error {
	i := controller.injector

	s, e := do.Invoke[*service.Service](i)
	if e != nil {
		return e
	}
	c, e := do.Invoke[*herdr.Client](i)
	if e != nil {
		return e
	}
	switch name {
	case "open":
		store, e := do.Invoke[*state.Store](i)
		if e != nil {
			return e
		}
		return service.OpenManager(ctx, c, store)
	case "sync":
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		_, e = s.Refresh(ctx)
		return e
	default:
		manager := os.Getenv("HERDR_WORKSPACE_ID")
		ids, e := s.Store.ManagerIDs()
		if e != nil {
			return e
		}
		if manager == "" || !ids[manager] {
			return fmt.Errorf("open the manager using the plugin action; this is not its dedicated workspace")
		}
		s.ManagerID = manager
		s.MutationError = c.CheckCapabilities(ctx)
		_, e = tea.NewProgram(tui.New(s, c.Focus), tea.WithAltScreen(), tea.WithContext(ctx)).Run()
		return e
	}
}
