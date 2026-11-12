package service

import (
	"context"
	"encoding/json"
	"fmt"
	"herdr-workspace-enhancer/repository/state"
	"testing"
)

type reconcileHost struct {
	responses []string
	calls     int
}

func (h *reconcileHost) Call(_ context.Context, dst any, args ...string) error {
	if h.calls >= len(h.responses) {
		return fmt.Errorf("unexpected call")
	}
	raw := h.responses[h.calls]
	h.calls++
	return json.Unmarshal([]byte(raw), dst)
}
func TestRecoverWorkspaceCreation(t *testing.T) {
	h := &reconcileHost{responses: []string{`{"workspaces":[{"workspace_id":"w5","label":"enhancer-restore-uuid"}]}`, `{"tabs":[{"tab_id":"t5"}]}`, `{"panes":[{"pane_id":"p5","tab_id":"t5"}]}`}}
	raw, e := reconcileCreation(context.Background(), h, state.Step{Args: []string{"workspace", "create", "--label", "enhancer-restore-uuid"}})
	if e != nil {
		t.Fatal(e)
	}
	var got creation
	if e = json.Unmarshal(raw, &got); e != nil {
		t.Fatal(e)
	}
	if got.Workspace.ID != "w5" || got.Root.ID != "p5" {
		t.Fatal(got)
	}
}
func TestRejectAmbiguousRecovery(t *testing.T) {
	h := &reconcileHost{responses: []string{`{"workspaces":[{"workspace_id":"w5","label":"token"},{"workspace_id":"w6","label":"token"}]}`}}
	if _, e := reconcileCreation(context.Background(), h, state.Step{Args: []string{"workspace", "create", "--label", "token"}}); e == nil {
		t.Fatal("ambiguous recovery accepted")
	}
	if h.calls != 1 {
		t.Fatal(h.calls)
	}
}

func TestRecoverSplit(t *testing.T) {
	baseline := json.RawMessage(`{"target":{"pane_id":"p1","tab_id":"t1","workspace_id":"w1"},"panes":[{"pane_id":"p1","tab_id":"t1","terminal_id":"term1"}]}`)
	cases := []struct {
		name, panes string
		ok          bool
	}{
		{"applied", `[{"pane_id":"p1","tab_id":"t1","terminal_id":"term1"},{"pane_id":"p2","tab_id":"t1","cwd":"/project"}]`, true},
		{"absent", `[{"pane_id":"p1","tab_id":"t1","terminal_id":"term1"}]`, false},
		{"replaced", `[{"pane_id":"p1","tab_id":"t1","terminal_id":"other"},{"pane_id":"p2","tab_id":"t1","cwd":"/project"}]`, false},
		{"different-tab", `[{"pane_id":"p1","tab_id":"t1","terminal_id":"term1"},{"pane_id":"p2","tab_id":"t2","cwd":"/project"}]`, false},
		{"multiple", `[{"pane_id":"p1","tab_id":"t1","terminal_id":"term1"},{"pane_id":"p2","tab_id":"t1","cwd":"/project"},{"pane_id":"p3","tab_id":"t1","cwd":"/project"}]`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &reconcileHost{responses: []string{`{"panes":` + tc.panes + `}`}}
			raw, e := reconcileCreation(context.Background(), h, state.Step{Args: []string{"pane", "split", "p1", "--cwd", "/project"}, Before: baseline})
			if (e == nil) != tc.ok {
				t.Fatal(string(raw), e)
			}
			if tc.ok {
				var got creation
				if e = json.Unmarshal(raw, &got); e != nil || got.Pane.ID != "p2" {
					t.Fatal(got, e)
				}
			}
		})
	}
}
