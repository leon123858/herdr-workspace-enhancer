package main

import (
	"context"
	"encoding/json"
	"fmt"
	"herdr-workspace-enhancer/internal/herdr"
	"herdr-workspace-enhancer/internal/state"
	"herdr-workspace-enhancer/internal/workspace"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOpenManager(t *testing.T) {
	for _, failure := range []string{"", "workspace create", "plugin pane open", "pane close", "workspace focus", "missing-pane"} {
		t.Run(failure, func(t *testing.T) {
			store, err := state.Open(t.TempDir(), "session")
			if err != nil {
				t.Fatal(err)
			}
			creates, closes := 0, 0
			client := &herdr.Client{Run: func(ctx context.Context, args ...string) ([]byte, error) {
				command := strings.Join(args[:2], " ")
				if command == "plugin pane" {
					command = strings.Join(args[:3], " ")
				}
				if command == failure {
					return nil, fmt.Errorf("injected failure")
				}
				result := map[string]any{}
				switch command {
				case "workspace create":
					creates++
					result = map[string]any{"workspace": herdr.Workspace{ID: fmt.Sprint(creates)}, "tab": herdr.Tab{ID: "first"}, "root_pane": herdr.Pane{ID: "initial"}}
				case "plugin pane open":
					ids, e := store.ManagerIDs()
					if e != nil || !ids[fmt.Sprint(creates)] {
						t.Fatal("not registered before launch", ids, e)
					}
					expected := []string{"plugin", "pane", "open", "--plugin", "workspace.enhancer", "--entrypoint", "manager", "--placement", "split", "--target-pane", "initial", "--focus"}
					if !reflect.DeepEqual(args, expected) {
						t.Fatal(args)
					}
					if failure != "missing-pane" {
						result["plugin_pane"] = map[string]any{"pane": herdr.Pane{ID: "manager"}}
					}
				case "pane close":
					if !reflect.DeepEqual(args, []string{"pane", "close", "initial"}) {
						t.Fatal(args)
					}
					closes++
				case "workspace focus":
					if args[2] != fmt.Sprint(creates) {
						t.Fatal(args)
					}
				default:
					t.Fatalf("unexpected operation %v", args)
				}
				return json.Marshal(map[string]any{"result": result})
			}}
			for i := 0; i < 2; i++ {
				err = openManager(context.Background(), client, store)
				if (err != nil) != (failure != "") {
					t.Fatal(err)
				}
			}
			if failure == "" && (creates != 2 || closes != 2) {
				t.Fatal(creates, closes)
			}
			if (failure == "plugin pane open" || failure == "missing-pane") && closes != 0 {
				t.Fatal("closed shell after failed startup")
			}
		})
	}
}

func TestCreateEventWaitsForManagerRegistration(t *testing.T) {
	store, err := state.Open(t.TempDir(), "session")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	var client *herdr.Client
	client = &herdr.Client{Run: func(ctx context.Context, args ...string) ([]byte, error) {
		result := map[string]any{}
		switch strings.Join(args[:2], " ") {
		case "workspace create":
			// This notification occurs before openManager sees the created ID.
			go func() {
				_, e := (&workspace.Service{Store: store, Host: client}).Refresh(ctx)
				done <- e
			}()
			if unlock, e := store.Lock("session"); e == nil {
				unlock()
				return nil, fmt.Errorf("manager creation was not protected")
			}
			result = map[string]any{"workspace": herdr.Workspace{ID: "manager"}, "tab": herdr.Tab{ID: "first"}, "root_pane": herdr.Pane{ID: "shell"}}
		case "workspace list":
			result["workspaces"] = []herdr.Workspace{{ID: "manager"}}
		case "plugin pane":
			result["plugin_pane"] = map[string]any{"pane": herdr.Pane{ID: "tui"}}
		}
		return json.Marshal(map[string]any{"result": result})
	}}
	if err = openManager(ctx, client, store); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	db, err := store.ReadDatabase()
	if err != nil || len(db.Records) != 0 {
		t.Fatal("manager recorded by create event", db, err)
	}
}
