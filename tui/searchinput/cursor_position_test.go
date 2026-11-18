package textinput

import "testing"

func TestCursorColumnScrollAndWideText(t *testing.T) {
	m := New()
	m.Prompt = "> "
	m.Width = 6
	m.SetValue("中文abcdef")
	m.CursorEnd()
	x := m.CursorColumn()
	if x < 2 || x > 8 {
		t.Fatal(x)
	}
	m.CursorStart()
	if x = m.CursorColumn(); x != 2 {
		t.Fatal(x)
	}
}
