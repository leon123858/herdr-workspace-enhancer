package herdr

import (
	"context"
	"encoding/json"
	"fmt"
)

func (c *Client) CheckCapabilities(ctx context.Context) error {
	raw, e := c.Run(ctx, "api", "schema", "--json")
	if e != nil {
		return fmt.Errorf("cannot inspect Herdr capabilities: %w", e)
	}
	var schema any
	if e = json.Unmarshal(raw, &schema); e != nil {
		return fmt.Errorf("invalid Herdr API schema: %w", e)
	}
	found := map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if name, ok := x["const"].(string); ok {
				found[name] = true
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(schema)
	for _, name := range []string{"workspace.list", "workspace.create", "workspace.close", "workspace.rename", "tab.list", "tab.create", "tab.rename", "pane.list", "pane.get", "pane.layout", "pane.split", "pane.rename"} {
		if !found[name] {
			return fmt.Errorf("Herdr lacks %s; upgrade Herdr before archiving or restoring", name)
		}
	}
	return nil
}
