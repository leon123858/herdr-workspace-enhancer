package bootstrap

import (
	"github.com/samber/do/v2"
	"herdr-workspace-enhancer/service"
	"testing"
)

func TestLazyIndependentContainers(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", "")
	a, b := New(), New()
	if a == b {
		t.Fatal("shared injector")
	}
	if _, e := do.Invoke[*Controller](a); e != nil {
		t.Fatal(e)
	}
	if _, e := do.Invoke[*service.Service](a); e == nil {
		t.Fatal("missing environment accepted")
	}
}
