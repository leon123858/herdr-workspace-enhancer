package service

import (
	"context"
	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/repository/herdr"
	"testing"
)

func TestClosedManagerResolution(t *testing.T) {
	for _, managerState := range []model.ManagerState{model.ManagerNone, model.ManagerInit, model.ManagerDone} {
		t.Run(string(managerState), func(t *testing.T) {
			store := serviceStore(t)
			saveRecord(t, store, model.Record{ID: "r", HostID: "h", Label: "manager", Status: model.WorkspaceClosed, ManagerState: managerState, Note: "note"})
			host := &restoreHost{fakeHost: fakeHost{live: []herdr.Workspace{{ID: "h", Label: "manager"}}}}
			svc := Service{Store: store, Host: host}
			_, err := svc.Refresh(context.Background())
			// This host has no manager panes: INIT must stay retryable after binding.
			if (err != nil) != (managerState == model.ManagerInit) {
				t.Fatal(err)
			}
			r, e := store.Get("r")
			if e != nil || r.Status != model.WorkspaceActive || r.ManagerState != managerState || r.Note != "note" {
				t.Fatal(r, e)
			}
			if managerState != model.ManagerInit && host.calls != 0 {
				t.Fatal(host.calls)
			}
		})
	}
}

func TestClosedManagerBindingBeforeMetadata(t *testing.T) {
	for _, failure := range []string{"binding", "rename", "manager"} {
		t.Run(failure, func(t *testing.T) {
			store := serviceStore(t)
			saveRecord(t, store, model.Record{ID: "manager", Label: "Manager", Status: model.WorkspaceClosed, ManagerState: model.ManagerInit})
			host := &restoreHost{fakeHost: fakeHost{live: []herdr.Workspace{{ID: "new", Label: managerPrefix + "manager"}}}}
			wrapped := &failedCheckpointStore{Store: store}
			switch failure {
			case "binding":
				wrapped.failAt = 1
			case "rename":
				host.failAt = 1
			case "manager": // Successful rename followed by missing manager panes.
			}
			svc := Service{Store: wrapped, Host: host}
			if _, err := svc.Refresh(context.Background()); err == nil {
				t.Fatal("expected failure")
			}
			r, err := store.Get("manager")
			want := model.WorkspaceActive
			if failure == "binding" {
				want = model.WorkspaceClosed
			}
			if err != nil || r.Status != want {
				t.Fatal(r, err)
			}
			if failure == "binding" {
				if r.HostID != "" || host.calls != 0 {
					t.Fatal("rename ran before binding", r, host.calls)
				}
			} else if r.HostID != "new" {
				t.Fatal("binding was lost", r)
			}
			if failure == "manager" && host.live[0].Label != "Manager" {
				t.Fatal("rename did not complete")
			}
			if failure == "manager" && (host.calls != 2 || wrapped.calls != 1) {
				t.Fatal("expected one activation commit, one rename and one pane list", host.calls, wrapped.calls)
			}
			wrapped.failAt, host.failAt = 0, 0
			// This fake has no panes: reaching manager initialization produces an error,
			// but must leave the original manager active, without a duplicate record.
			if _, err := svc.Refresh(context.Background()); err == nil {
				t.Fatal("expected manager pane error")
			}
			r, err = store.Get("manager")
			if err != nil || r.Status != model.WorkspaceActive || r.HostID != "new" || r.ManagerState != model.ManagerInit {
				t.Fatal(r, err)
			}
			records, err := store.All(context.Background())
			if err != nil || len(records) != 1 {
				t.Fatal("identity lost", records, err)
			}
		})
	}
}
