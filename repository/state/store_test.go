package state

import (
	"errors"
	"testing"
)

func TestNotes(t *testing.T) {
	root := t.TempDir()
	a, e := Open(root, "session-a")
	if e != nil {
		t.Fatal(e)
	}
	b, e := Open(root, "session-b")
	if e != nil {
		t.Fatal(e)
	}
	n, e := a.Save("workspace", 0, "中文\nnotes")
	if e != nil {
		t.Fatal(e)
	}
	if n.Revision != 1 {
		t.Fatal(n)
	}
	if _, e = a.Save("workspace", 0, "lost update"); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	got, e := a.Load("workspace")
	if e != nil || got.Text != n.Text {
		t.Fatal(got, e)
	}
	got, e = b.Load("workspace")
	if e != nil || got.Text != "" {
		t.Fatal(got, e)
	}
	if _, e = a.Save("workspace", 1, ""); e != nil {
		t.Fatal(e)
	}
}
func TestRejectImplicitStorage(t *testing.T) {
	for _, v := range []struct{ root, session string }{{"", "x"}, {"relative", "x"}, {t.TempDir(), ""}} {
		if _, e := Open(v.root, v.session); e == nil {
			t.Fatal(v)
		}
	}
}
