package main

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"herdr-workspace-enhancer/internal/herdr"
	"herdr-workspace-enhancer/internal/state"
	"herdr-workspace-enhancer/internal/tui"
	"herdr-workspace-enhancer/internal/workspace"
	"os"
	"time"
)

func run() error {
	if len(os.Args) > 1 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		fmt.Println("Herdr Workspace Enhancer\nRun from a dedicated Herdr manager workspace.\nRequired environment: HERDR_BIN_PATH, HERDR_SOCKET_PATH, HERDR_PLUGIN_STATE_DIR, HERDR_WORKSPACE_ID")
		return nil
	}
	binary, socket, root, manager := os.Getenv("HERDR_BIN_PATH"), os.Getenv("HERDR_SOCKET_PATH"), os.Getenv("HERDR_PLUGIN_STATE_DIR"), os.Getenv("HERDR_WORKSPACE_ID")
	if binary == "" || socket == "" || root == "" {
		return fmt.Errorf("launch through Herdr plugin context; required environment is missing (see --help)")
	}
	store, e := state.Open(root, socket)
	if e != nil {
		return e
	}
	client := herdr.New(binary)
	if len(os.Args) > 1 {
		if os.Args[1] == "--open" {
			return openManager(context.Background(), client, store)
		}
		if os.Args[1] == "--sync" {
			// Event context identifies the affected user workspace, not a manager.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			_, err := (&workspace.Service{Store: store, Host: client}).Refresh(ctx)
			return err
		}
		return fmt.Errorf("unknown argument %q", os.Args[1])
	}
	if manager == "" {
		return fmt.Errorf("manager workspace context is missing")
	}
	known, err := store.ManagerIDs()
	if err != nil {
		return err
	}
	if !known[manager] {
		return fmt.Errorf("open the manager using the plugin action; this is not its dedicated workspace")
	}

	service := &workspace.Service{Store: store, Host: client, ManagerID: manager, MutationError: client.CheckCapabilities(context.Background())}
	_, e = tea.NewProgram(tui.New(service, client.Focus), tea.WithAltScreen()).Run()
	return e
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
