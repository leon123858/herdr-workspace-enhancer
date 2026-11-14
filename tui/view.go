package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type rect struct{ x, y, w, h int }

func (r rect) contains(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

type geometry struct {
	list, note rect
	split      int
	wide       bool
}

func (m Model) geometry() geometry {
	h := max(3, m.height-5)
	if m.width >= 80 {
		ratio := m.ratio
		if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < .1 || ratio > .9 {
			ratio = .35
		}
		left := max(26, min(m.width-39, int(float64(m.width)*ratio)))
		return geometry{rect{0, 2, left, h}, rect{left + 1, 2, m.width - left - 1, h}, left, true}
	}
	top := max(4, h/3)
	return geometry{list: rect{0, 2, m.width, top}, note: rect{0, 2 + top, m.width, max(3, h-top)}}
}
func (m *Model) resize() {
	g := m.geometry()
	m.editor.SetWidth(max(1, g.note.w-2))
	m.editor.SetHeight(max(1, g.note.h-3))
	m.search.Width = max(1, g.list.w-4)
	m.ensureVisible()
}
func (m Model) listHeight() int { return max(1, m.geometry().list.h-4) }
func (m *Model) ensureVisible() {
	count := len(m.visible())
	m.cursor = max(0, min(m.cursor, max(0, count-1)))
	if m.cursor < m.listOffset {
		m.listOffset = m.cursor
	}
	if m.cursor >= m.listOffset+m.listHeight() {
		m.listOffset = m.cursor - m.listHeight() + 1
	}
	m.listOffset = max(0, min(m.listOffset, max(0, count-m.listHeight())))
}
func clip(s string, w int) string {
	return ansi.Truncate(strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", " "), max(0, w), "…")
}
func box(title string, lines []string, r rect, focused bool) string {
	w := max(0, r.w-2)
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	if focused {
		style = style.Foreground(lipgloss.Color("75"))
	}
	head := clip(" "+title+" ", w)
	head += "─" + strings.Repeat("─", max(0, w-ansi.StringWidth(head)-1))
	out := []string{style.Render("┌" + ansi.Truncate(head, w, "") + "┐")}
	for i := 0; i < r.h-2; i++ {
		line := ""
		if i < len(lines) {
			line = ansi.Truncate(lines[i], w, "…")
		}
		line += strings.Repeat(" ", max(0, w-ansi.StringWidth(line)))
		out = append(out, style.Render("│")+line+style.Render("│"))
	}
	out = append(out, style.Render("└"+strings.Repeat("─", w)+"┘"))
	return strings.Join(out, "\n")
}
func (m Model) View() string {
	g := m.geometry()
	list := []string{"Filter: " + m.filter.String() + " [f]", m.search.View()}
	rows := m.visible()
	for i := m.listOffset; i < min(len(rows), m.listOffset+m.listHeight()); i++ {
		r := rows[i]
		mark := "  "
		if i == m.cursor {
			mark = "> "
		}
		line := clip(fmt.Sprintf("%s[%s] %s", mark, r.Status, r.Label), g.list.w-2)
		if i == m.cursor {
			line = lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true).Render(line)
		}
		list = append(list, line)
	}
	if len(rows) == 0 {
		list = append(list, "No matching workspaces")
	}
	title := "Notes"
	if m.editingID != "" {
		title += " · " + clip(m.editingLabel, max(1, g.note.w-17))
	}
	if m.dirty() {
		title += " · Modified"
	} else if m.noteLoaded {
		title += " · Saved"
	}
	note := strings.Split(m.editor.View(), "\n")
	if !m.noteLoaded {
		note = []string{"Select a workspace to view notes"}
		if m.editingID != "" {
			note = []string{"Could not load note. Press r to retry."}
		}
	}
	buttons := "[Save] [Discard]"
	switch m.mode {
	case ModeConfirmUnsaved:
		note = []string{"Unsaved changes.", "Save or discard before continuing.", "s Save · d Discard · Esc Cancel"}
		buttons = "[Save] [Discard] [Cancel]"
	case ModeConfirmArchive:
		text := "Archiving closes all tabs, panes, and foreground processes. Restoring recreates shells only; running processes are not restored."
		note = strings.Split(lipgloss.NewStyle().Width(max(1, g.note.w-2)).Render(text), "\n")
		for _, t := range m.preview.Snapshot.Tabs {
			note = append(note, fmt.Sprintf("%s — %d panes", clip(t.Label, g.note.w-15), paneCount(t.Root)))
		}
		note = append(note, "y Confirm · any other key Cancel")
		buttons = "[Confirm] [Cancel]"
	}
	innerH := max(1, g.note.h-2)
	body := make([]string, innerH)
	copy(body[:max(0, innerH-1)], note)
	body[innerH-1] = buttons
	if m.copyNotice != "" && m.mode != ModeConfirmUnsaved && m.mode != ModeConfirmArchive {
		if innerH >= 4 {
			notice := box("Clipboard", []string{m.copyNotice}, rect{w: g.note.w - 2, h: 3}, true)
			copy(body[innerH-4:innerH-1], strings.Split(notice, "\n"))
		} else if innerH >= 2 {
			body[innerH-2] = m.copyNotice
		}
	}
	left := box("Workspaces", list, g.list, m.mode == ModeList || m.mode == ModeSearch)
	right := box(title, body, g.note, m.mode == ModeEdit || m.mode == ModeConfirmUnsaved || m.mode == ModeConfirmArchive)
	bodyView := left + "\n" + right
	if g.wide {
		bodyView = lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat("│\n", max(0, g.list.h-1))+"│", right)
	}
	status := m.status
	if m.busy {
		status = "Working… " + status
	}
	out := "Herdr Workspace Enhancer\n\n" + bodyView + "\n" + clip("Tab Switch pane · Ctrl+S Save · Esc Focus list", m.width) + "\n" + clip("↑↓ Select · f Filter · / Search · a Archive · Enter Restore/focus · q Quit", m.width) + "\n" + clip(status, m.width)
	// Tiny terminals still receive a bounded frame.
	lines := strings.Split(out, "\n")
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], m.width, "")
	}
	return strings.Join(lines, "\n")
}
