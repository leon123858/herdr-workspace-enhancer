package controller

import (
	"bytes"
	"context"
	"errors"
	"herdr-workspace-enhancer/repository/herdr"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFinishNotification(t *testing.T) {
	for _, fail := range []bool{false, true} {
		c := New()
		calls := 0
		original := errors.New("database unavailable")
		c.host = &herdr.Client{Run: func(ctx context.Context, args ...string) ([]byte, error) {
			calls++
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 3*time.Second {
				t.Fatal("missing short timeout")
			}
			want := []string{"notification", "show", "Workspace Enhancer failed", "--body", original.Error(), "--position", "top-right", "--sound", "request"}
			if !reflect.DeepEqual(args, want) {
				t.Fatalf("args: %v", args)
			}
			if fail {
				return nil, errors.New("host unavailable")
			}
			return []byte(`{"result":{}}`), nil
		}}
		var out bytes.Buffer
		if err := c.Finish(original, &out); !errors.Is(err, original) {
			t.Fatal(err)
		}
		if calls != 1 {
			t.Fatal(calls)
		}
		if fail && !strings.Contains(out.String(), "host unavailable") {
			t.Fatal(out.String())
		}
	}
}

func TestFinishSuccessDoesNotNotify(t *testing.T) {
	c := New()
	c.host = &herdr.Client{Run: func(context.Context, ...string) ([]byte, error) { t.Fatal("unexpected notification"); return nil, nil }}
	if err := c.Finish(nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
}

func TestFinishWithoutHostPreservesError(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", "")
	c := New()
	original := errors.New("invalid arguments")
	if err := c.Finish(original, &bytes.Buffer{}); !errors.Is(err, original) {
		t.Fatal(err)
	}
}
