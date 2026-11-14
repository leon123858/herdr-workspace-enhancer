package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/repository/herdr"
	"herdr-workspace-enhancer/repository/state"
	"reflect"
	"strings"
	"sync"
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
			t.Cleanup(func() { _ = store.Close() })
			creates, closes := 0, 0
			live := []herdr.Workspace{}
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
					live = append(live, herdr.Workspace{ID: fmt.Sprint(creates), Label: args[5]})
					result = map[string]any{"workspace": herdr.Workspace{ID: fmt.Sprint(creates)}, "tab": herdr.Tab{ID: "first"}, "root_pane": herdr.Pane{ID: "initial"}}
				case "plugin pane open":
					isManager, e := store.IsManager(fmt.Sprint(creates))
					if e != nil || !isManager {
						t.Fatal("not registered before launch", isManager, e)
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
				case "workspace list":
					result["workspaces"] = live
				case "workspace rename":
					for i := range live {
						if live[i].ID == args[2] {
							live[i].Label = args[3]
						}
					}
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
				err = OpenManager(context.Background(), client, store)
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
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	label := ""
	var hostMu sync.Mutex
	var client *herdr.Client
	client = &herdr.Client{Run: func(ctx context.Context, args ...string) ([]byte, error) {
		hostMu.Lock()
		defer hostMu.Unlock()
		result := map[string]any{}
		switch strings.Join(args[:2], " ") {
		case "workspace create":
			label = args[5]
			// This notification occurs before openManager sees the created ID.
			go func() {
				_, e := (&Service{Store: store, Host: client}).Refresh(ctx)
				done <- e
			}()
			lockCtx, cancel := context.WithTimeout(ctx, 40*time.Millisecond)
			defer cancel()
			if unlock, e := store.WaitLock(lockCtx, "session"); e == nil {
				unlock()
				return nil, fmt.Errorf("manager creation was not protected")
			} else if !errors.Is(e, context.DeadlineExceeded) {
				return nil, fmt.Errorf("unexpected session lock error: %w", e)
			}
			result = map[string]any{"workspace": herdr.Workspace{ID: "manager"}, "tab": herdr.Tab{ID: "first"}, "root_pane": herdr.Pane{ID: "shell"}}
		case "workspace list":
			result["workspaces"] = []herdr.Workspace{{ID: "manager", Label: label}}
		case "workspace rename":
			label = args[3]
		case "plugin pane":
			result["plugin_pane"] = map[string]any{"pane": herdr.Pane{ID: "tui"}}
		}
		return json.Marshal(map[string]any{"result": result})
	}}
	if err = OpenManager(ctx, client, store); err != nil {
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
	rows, err := store.List()
	if err != nil || len(rows) != 0 {
		t.Fatal("manager recorded by create event", rows, err)
	}
}

func TestLostManagerReplyIsStillClassifiedAsManager(t *testing.T) {
	store := serviceStore(t)
	label := ""
	client := &herdr.Client{Run: func(_ context.Context, args ...string) ([]byte, error) {
		if args[0] == "workspace" && args[1] == "create" {
			for i, arg := range args {
				if arg == "--label" {
					label = args[i+1]
				}
			}
			return nil, fmt.Errorf("lost manager creation reply")
		}
		return nil, fmt.Errorf("unexpected call")
	}}
	if e := OpenManager(context.Background(), client, store); e == nil {
		t.Fatal("expected lost reply")
	}
	host := &fakeHost{live: []herdr.Workspace{{ID: "manager-host", Label: label}}}
	rows, e := (&Service{Store: store, Host: host}).Refresh(context.Background())
	if e != nil || len(rows) != 0 {
		t.Fatal("manager exposed as project", rows, e)
	}
	yes, e := store.IsManager("manager-host")
	if e != nil || !yes {
		t.Fatal(yes, e)
	}
}

type failedManagerStore struct {
	*state.Store
	syncs int
}

func (s *failedManagerStore) CreateManager(context.Context, model.Record) error {
	return fmt.Errorf("manager write failed")
}
func (s *failedManagerStore) ApplySync(ctx context.Context, rows []model.Record) error {
	s.syncs++
	return s.Store.ApplySync(ctx, rows)
}
func TestManagerWriteFailureStopsBeforeHostAndSync(t *testing.T) {
	store := &failedManagerStore{Store: serviceStore(t)}
	calls := 0
	client := &herdr.Client{Run: func(context.Context, ...string) ([]byte, error) {
		calls++
		return nil, fmt.Errorf("unexpected host call")
	}}
	if err := OpenManager(context.Background(), client, store); err == nil {
		t.Fatal("expected write failure")
	}
	if calls != 0 || store.syncs != 0 {
		t.Fatal(calls, store.syncs)
	}
}
