package herdr

import (
	"context"
	"testing"
)

func TestCapabilitiesRejectMissingMethods(t *testing.T) {
	c := &Client{Run: func(context.Context, ...string) ([]byte, error) { return []byte(`{"schemas":{}}`), nil }}
	if e := c.CheckCapabilities(context.Background()); e == nil {
		t.Fatal("missing methods accepted")
	}
}
