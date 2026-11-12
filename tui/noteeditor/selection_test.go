package textarea

import (
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

func TestReplaceReverseMultilineSelection(t *testing.T) {
	m := New()
	m.SetValue("中文\nhello")
	m.anchor = 5
	m.head = 1
	m.InsertString("替換")
	if m.Value() != "中替換llo" {
		t.Fatal(m.Value())
	}
}

func TestCopyWithoutSelectionDoesNotExit(t *testing.T) {
	m := New()
	m.SetValue("unchanged")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil || updated.Value() != m.Value() {
		t.Fatal("copy changed editor")
	}
}

func TestMouseSelectChinese(t *testing.T) {
	m := New()
	m.ShowLineNumbers = false
	m.Prompt = ""
	m.SetWidth(40)
	m.SetValue("中文abc")
	m.selectMouse(tea.MouseMsg{X: 0, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.selectMouse(tea.MouseMsg{X: 4, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	if m.anchor != 0 || m.head != 2 {
		t.Fatalf("selection %d:%d", m.anchor, m.head)
	}
}
