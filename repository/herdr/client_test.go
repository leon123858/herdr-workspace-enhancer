package herdr

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestResponseValidation(t *testing.T) {
	for _, raw := range []string{`{}`, `{"result":null}`, `{"result":{}}`, `{"result":{"workspaces":null}}`, `{"result":{"workspaces":[{}]}}`, `{"error":{"code":"gone","message":"closed"}}`, `not json`} {
		c := &Client{Run: func(context.Context, ...string) ([]byte, error) { return []byte(raw), nil }}
		if _, e := c.Workspaces(context.Background()); e == nil {
			t.Fatal(raw)
		}
	}
}
func TestArguments(t *testing.T) {
	want := []string{"workspace", "focus", "w1; echo bad"}
	c := &Client{Run: func(_ context.Context, args ...string) ([]byte, error) {
		if !reflect.DeepEqual(args, want) {
			t.Fatal(args)
		}
		return []byte(`{"result":{}}`), nil
	}}
	if e := c.Focus(context.Background(), want[2]); e != nil {
		t.Fatal(e)
	}
}

func TestSnapshot(t *testing.T) {
	dir := t.TempDir()
	responses := []any{
		map[string]any{"tabs": []any{map[string]any{"tab_id": "t1", "label": "中文"}}},
		map[string]any{"panes": []any{map[string]any{"pane_id": "p1", "tab_id": "t1", "cwd": dir}}},
		map[string]any{"layout": map[string]any{"panes": []any{map[string]any{"pane_id": "p1", "rect": map[string]int{"x": 0, "y": 0, "width": 80, "height": 24}}}}},
	}
	index := 0
	c := &Client{Run: func(_ context.Context, args ...string) ([]byte, error) {
		if index >= len(responses) {
			t.Fatal("unexpected call", args)
		}
		b, e := json.Marshal(map[string]any{"result": responses[index]})
		index++
		return b, e
	}}
	snap, e := c.Snapshot(context.Background(), Workspace{ID: "w1", Label: "project"})
	if e != nil {
		t.Fatal(e)
	}
	if len(snap.Tabs) != 1 || snap.Tabs[0].Root.CWD != dir || snap.Tabs[0].Label != "中文" {
		t.Fatal(snap)
	}
}

func TestCapabilitiesRejectMissingMethods(t *testing.T) {
	c := &Client{Run: func(context.Context, ...string) ([]byte, error) { return []byte(`{"schemas":{}}`), nil }}
	if e := c.CheckCapabilities(context.Background()); e == nil {
		t.Fatal("missing methods accepted")
	}
}
