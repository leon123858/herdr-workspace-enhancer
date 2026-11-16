package service

import (
	"context"
	"herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/model/layout"
	"herdr-workspace-enhancer/repository/herdr"
	"testing"
)

func TestSyncFourStateMatrix(t *testing.T) {
	for _, status := range []model.WorkspaceStatus{model.WorkspaceActive, model.WorkspaceArchiving, model.WorkspaceArchived, model.WorkspaceClosed} {
		for _, exists := range []bool{false, true} {
			name := string(status) + "/absent"
			if exists {
				name = string(status) + "/present"
			}
			t.Run(name, func(t *testing.T) {
				store := serviceStore(t)
				saveRecord(t, store, model.Record{ID: "stable", HostID: "host", Status: status, Note: "retained", Snapshot: &herdr.Snapshot{Tabs: []herdr.SavedTab{{Root: &layout.Node{Type: layout.NodePane, CWD: t.TempDir()}}}}})
				live := []herdr.Workspace{}
				if exists {
					live = append(live, herdr.Workspace{ID: "host", Label: "project"})
				}
				svc := Service{Store: store, Host: &restoreHost{fakeHost: fakeHost{live: live}}}
				if _, err := svc.Refresh(context.Background()); err != nil {
					t.Fatal(err)
				}
				want := status
				if exists && status != model.WorkspaceArchiving {
					want = model.WorkspaceActive
				} else if status == model.WorkspaceActive {
					want = model.WorkspaceClosed
				} else if status == model.WorkspaceArchiving {
					want = model.WorkspaceArchived
				}
				got, err := store.Get("stable")
				if err != nil || got.Status != want || got.Note != "retained" {
					t.Fatal(got, err, "want", want)
				}
			})
		}
	}
}

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
	for _, failure := range []string{"binding", "rename", "activation"} {
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
			case "activation":
				wrapped.failAt = 2
			}
			svc := Service{Store: wrapped, Host: host}
			if _, err := svc.Refresh(context.Background()); err == nil {
				t.Fatal("expected failure")
			}
			r, err := store.Get("manager")
			if err != nil || r.Status != model.WorkspaceClosed {
				t.Fatal(r, err)
			}
			if failure == "binding" {
				if r.HostID != "" || host.calls != 0 {
					t.Fatal("rename ran before binding", r, host.calls)
				}
			} else if r.HostID != "new" {
				t.Fatal("binding was lost", r)
			}
			if failure == "activation" && host.live[0].Label != "Manager" {
				t.Fatal("rename did not complete")
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
