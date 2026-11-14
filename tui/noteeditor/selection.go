package textarea

import (
	"encoding/base64"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/rivo/uniseg"
	"os"
	"strings"
)

// ClipboardCopied reports that the OSC52 request was written to terminal output.
// Whether the clipboard is updated depends on the terminal's OSC52 support.
type ClipboardCopied struct{}

type ClipboardCopyFailed struct{ Err error }

func (m *Model) deleteSelection() {
	if m.anchor == m.head {
		return
	}
	text := []rune(m.Value())
	a, b := min(m.anchor, m.head), max(m.anchor, m.head)
	a = min(a, len(text))
	b = min(b, len(text))
	prefix := string(text[:a])
	m.anchor = 0
	m.head = 0
	m.SetValue(string(append(text[:a], text[b:]...)))
	m.row = strings.Count(prefix, "\n")
	parts := strings.Split(prefix, "\n")
	m.col = len([]rune(parts[len(parts)-1]))
	m.repositionView()
}

func (m Model) copySelection() tea.Cmd {
	if m.anchor == m.head {
		return nil
	}
	text := []rune(m.Value())
	a, b := min(m.anchor, m.head), max(m.anchor, m.head)
	encoded := base64.StdEncoding.EncodeToString([]byte(string(text[min(a, len(text)):min(b, len(text))])))
	return func() tea.Msg {
		_, err := fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\a", encoded)
		if err != nil {
			return ClipboardCopyFailed{Err: err}
		}
		return ClipboardCopied{}
	}
}

func (m *Model) selectMouse(mouse tea.MouseMsg) {
	if mouse.Button == tea.MouseButtonWheelUp {
		m.viewport.LineUp(3)
		return
	}
	if mouse.Button == tea.MouseButtonWheelDown {
		m.viewport.LineDown(3)
		return
	}
	if mouse.Button != tea.MouseButtonLeft && !m.dragging {
		return
	}
	if mouse.Action == tea.MouseActionPress {
		m.dragging = true
	}
	if !m.dragging {
		return
	}
	if mouse.Y < 0 {
		m.viewport.LineUp(1)
	} else if mouse.Y >= m.height {
		m.viewport.LineDown(1)
	}
	target := m.viewport.YOffset + max(0, min(mouse.Y, m.height-1))
	row := 0
	offset := 0
	pos := len([]rune(m.Value()))
	found := false
	for _, line := range m.value {
		used := 0
		for _, wrapped := range m.memoizedWrap(line, m.width) {
			if row == target {
				x := max(0, mouse.X-m.promptWidth)
				if m.ShowLineNumbers {
					x = max(0, x-uniseg.StringWidth(m.formatLineNumber(0)))
				}
				count := 0
				cells := 0
				g := uniseg.NewGraphemes(string(wrapped))
				for g.Next() {
					w := g.Width()
					if cells+w > x {
						break
					}
					cells += w
					count += len([]rune(g.Str()))
				}
				pos = offset + min(len(line), used+count)
				found = true
				break
			}
			used += len(wrapped)
			row++
		}
		if found {
			break
		}
		offset += len(line) + 1
	}
	if mouse.Action == tea.MouseActionPress {
		m.anchor = pos
	}
	m.head = pos
	// Keep the insertion cursor at the clicked/dragged endpoint, including wrapped
	// lines and wide Unicode characters. Selection offsets remain rune-based.
	prefix := string([]rune(m.Value())[:pos])
	m.row = strings.Count(prefix, "\n")
	parts := strings.Split(prefix, "\n")
	m.SetCursor(len([]rune(parts[len(parts)-1])))
	if mouse.Action == tea.MouseActionRelease {
		m.dragging = false
	}
}

// DocumentStart moves to the first line and reveals the start of the note.
func (m *Model) DocumentStart() { m.row = 0; m.SetCursor(0); m.repositionView() }

// DocumentEnd moves to the last line and reveals the end of the note.
func (m *Model) DocumentEnd() { m.row = len(m.value) - 1; m.CursorEnd(); m.repositionView() }
