package textarea

import (
	"encoding/base64"
	tea "github.com/charmbracelet/bubbletea"
	"io"
	"os"
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

func TestClickPositionsInsertionCursorAndWrappedUnicode(t *testing.T) {
	m := New()
	m.ShowLineNumbers = false
	m.Prompt = ""
	m.SetWidth(20)
	m.SetHeight(5)
	m.SetValue("中文abc\nsecond")
	m.Focus()
	m.selectMouse(tea.MouseMsg{X: 4, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.selectMouse(tea.MouseMsg{X: 4, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'!'}})
	if m.Value() != "中文!abc\nsecond" {
		t.Fatal(m.Value())
	}
	m.SetWidth(8)
	m.SetValue("中文abcdefghij")
	m.DocumentStart()
	m.selectMouse(tea.MouseMsg{X: 2, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.selectMouse(tea.MouseMsg{X: 2, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	pos := m.head
	original := []rune(m.Value())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'!'}})
	want := string(original[:pos]) + "!" + string(original[pos:])
	if m.Value() != want {
		t.Fatal(m.Value(), want)
	}
}

func TestMouseSelectionCopiesOnRelease(t *testing.T) {
	for _, tc := range []struct {
		name, text                 string
		startX, startY, endX, endY int
		want                       string
	}{
		{"Unicode", "中文abc", 0, 0, 4, 0, "中文"},
		{"reverse multiline", "中文\nhello", 3, 1, 2, 0, "文\nhel"},
		{"click", "unchanged", 2, 0, 2, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New()
			m.ShowLineNumbers = false
			m.Prompt = ""
			m.SetWidth(40)
			m.SetHeight(5)
			m.SetValue(tc.text)
			m.DocumentStart()
			var cmd tea.Cmd
			m, cmd = m.Update(tea.MouseMsg{X: tc.startX, Y: tc.startY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			if cmd != nil {
				t.Fatal("copied before selection completed")
			}
			m, cmd = m.Update(tea.MouseMsg{X: tc.endX, Y: tc.endY, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
			if cmd != nil {
				t.Fatal("copied during drag")
			}
			release := tea.MouseMsg{X: tc.endX, Y: tc.endY, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease}
			m, cmd = m.Update(release)
			if tc.want == "" {
				if cmd != nil {
					t.Fatal("click overwrote clipboard")
				}
				return
			}
			if cmd == nil {
				t.Fatal("release did not copy selection")
			}
			previous := os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			os.Stdout = w
			var msg tea.Msg
			func() { defer func() { os.Stdout = previous; w.Close() }(); msg = cmd() }()
			if _, ok := msg.(ClipboardCopied); !ok {
				t.Fatal("copy failed", msg)
			}
			b, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(tc.want)) + "\a"
			if string(b) != want {
				t.Fatalf("clipboard output = %q; want %q", b, want)
			}
			if m.Value() != tc.text {
				t.Fatal("copy changed note")
			}
			_, cmd = m.Update(release)
			if cmd != nil {
				t.Fatal("duplicate release copied again")
			}
			_, cmd = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
			if cmd != nil {
				t.Fatal("scroll copied again")
			}
		})
	}
}

func TestRightClickCopiesExistingSelectionWithoutChangingIt(t *testing.T) {
	m := New()
	m.SetValue("中文\nhello")
	m.anchor = 1
	m.head = 5
	n, cmd := m.Update(tea.MouseMsg{X: 0, Y: 0, Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
	if cmd == nil || n.anchor != m.anchor || n.head != m.head || n.Value() != m.Value() {
		t.Fatal("right click changed selection instead of copying")
	}
	_, cmd = n.Update(tea.MouseMsg{Button: tea.MouseButtonRight, Action: tea.MouseActionRelease})
	if cmd != nil {
		t.Fatal("right button release copied twice")
	}
	m.anchor = m.head
	_, cmd = m.Update(tea.MouseMsg{Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
	if cmd != nil {
		t.Fatal("empty selection changed clipboard")
	}
}
