package service

import (
	"context"
	"herdr-workspace-enhancer/model"
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
				saveRecord(t, store, model.Record{ID: "stable", HostID: "host", Status: status, Note: "retained"})
				live := []herdr.Workspace{}
				if exists {
					live = append(live, herdr.Workspace{ID: "host", Label: "project"})
				}
				svc := Service{Store: store, Host: &fakeHost{live: live}}
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
