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
				if exists {
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
