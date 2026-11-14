package controller

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samber/do/v2"
	"herdr-workspace-enhancer/repository/herdr"
	"herdr-workspace-enhancer/repository/state"
	"herdr-workspace-enhancer/service"
)

func TestInitializationIsLazyAndIndependent(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", "")
	root := filepath.Join(t.TempDir(), "plugin")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", root)
	a, b := New(), New()
	if a.injector == b.injector {
		t.Fatal("controllers share an injector")
	}
	for _, args := range [][]string{nil, {"--help"}, {"open", "--help"}, {"sync", "--help"}, {"tui", "--help"}} {
		cmd := a.Command()
		cmd.SetArgs(args)
		var out bytes.Buffer
		cmd.SetOut(&out)
		if err := cmd.Execute(); err != nil {
			t.Fatal(args, err)
		}
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("initialization or help touched plugin state", err)
	}
	if _, err := do.Invoke[*service.Service](a.injector); err == nil {
		t.Fatal("missing environment accepted")
	}
}

func TestInitializedProvidersShareDependencies(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HERDR_BIN_PATH", "herdr")
	t.Setenv("HERDR_SOCKET_PATH", filepath.Join(root, "socket"))
	t.Setenv("HERDR_PLUGIN_STATE_DIR", root)
	c := New()
	t.Cleanup(func() { _ = c.Shutdown() })
	svc, err := do.Invoke[*service.Service](c.injector)
	if err != nil {
		t.Fatal(err)
	}
	host, err := do.Invoke[*herdr.Client](c.injector)
	if err != nil {
		t.Fatal(err)
	}
	store, err := do.Invoke[*state.Store](c.injector)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := do.Invoke[*service.SettingsService](c.injector)
	if err != nil {
		t.Fatal(err)
	}
	if svc.Host != host || svc.Store != store {
		t.Fatal("providers did not share dependencies")
	}
	if err = settings.SaveRatio(.45); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "config.toml")); err != nil {
		t.Fatal("config not stored under plugin root", err)
	}
	other := New()
	t.Cleanup(func() { _ = other.Shutdown() })
	otherSvc, err := do.Invoke[*service.Service](other.injector)
	if err != nil {
		t.Fatal(err)
	}
	if svc == otherSvc || svc.Host == otherSvc.Host || svc.Store == otherSvc.Store {
		t.Fatal("controllers share resolved services")
	}
}

func TestCommandsReportMissingContext(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", "")
	for _, name := range []string{"open", "sync", "tui"} {
		cmd := New().Command()
		cmd.SetArgs([]string{name})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "required environment is missing") {
			t.Fatal(name, err)
		}
	}

}
