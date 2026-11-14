package state

import "testing"

func TestNotesLastWriterWinsAndSessionIsolation(t *testing.T) {
	root := t.TempDir()
	a, e := Open(root, "a")
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	b, e := Open(root, "b")
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	if _, e = a.Save("workspace", "中文\nnotes"); e != nil {
		t.Fatal(e)
	}
	n, e := a.Load("workspace")
	if e != nil || n.Text != "中文\nnotes" {
		t.Fatal(n, e)
	}
	n, e = b.Load("workspace")
	if e != nil || n.Text != "" {
		t.Fatal(n, e)
	}
	if _, e = a.Save("workspace", "newer"); e != nil {
		t.Fatal(e)
	}
	if _, e = a.Save("workspace", ""); e != nil {
		t.Fatal(e)
	}
	n, e = a.Load("workspace")
	if e != nil || n.Text != "" {
		t.Fatal(n, e)
	}
}
func TestRejectImplicitStorage(t *testing.T) {
	for _, v := range []struct{ root, session string }{{"", "x"}, {"relative", "x"}, {t.TempDir(), ""}} {
		if s, e := Open(v.root, v.session); e == nil {
			s.Close()
			t.Fatal(v)
		}
	}
}
