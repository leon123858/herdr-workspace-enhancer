package textarea

import "testing"

func TestCursorPositionWideWrapAndScroll(t *testing.T) {
	m := New()
	m.ShowLineNumbers = false
	m.Prompt = ""
	m.SetWidth(8)
	m.SetHeight(2)
	m.SetValue("中文ab")
	m.CursorEnd()
	x, y := m.CursorPosition()
	if x != 6 || y != 0 {
		t.Fatal(x, y)
	}
	m.SetValue("中文中文中文中文\n第二行\n第三行")
	m.View()
	m.DocumentEnd()
	m.repositionView()
	x, y = m.CursorPosition()
	if x < 0 || x > 8 || y < 0 || y >= 2 {
		t.Fatal(x, y)
	}
}
