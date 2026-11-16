package tui

import (
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	domain "herdr-workspace-enhancer/model"
	state "herdr-workspace-enhancer/repository/state"
	workspace "herdr-workspace-enhancer/service"
	textarea "herdr-workspace-enhancer/tui/noteeditor"
	"strings"
	"testing"
)

func TestResponsiveLayoutAndMouseControls(t *testing.T) {
	for _, width := range []int{1, 40, 79, 80, 120} {
		t.Run(string(rune(width)), func(t *testing.T) {
			m, _ := fixture(t)
			n, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 32})
			m = n.(model)
			view := m.View()
			if len(strings.Split(view, "\n")) > 32 {
				t.Fatal("frame overflow")
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatal("width overflow")
				}
			}
			if width < 40 {
				return
			}
			if !strings.Contains(view, "First note") {
				t.Fatal("missing note", view)
			}
			g := m.geometry()
			m = click(m, g.list.x+2, g.list.y+4)
			if m.editingID != "two" {
				t.Fatal("wrong clicked row")
			}
			m = click(m, g.note.x+2, g.note.y+1)
			if m.mode != ModeEdit {
				t.Fatal("click did not focus notes")
			}
			m.editor.SetValue("mouse draft")
			m = click(m, g.note.x+2, g.note.y+g.note.h-2)
			if m.dirty() {
				t.Fatal("save button failed")
			}
			m.editor.SetValue("discard me")
			m = click(m, g.note.x+9, g.note.y+g.note.h-2)
			if m.mode != ModeConfirmUnsaved {
				t.Fatal("missing discard confirmation")
			}
			m = click(m, g.note.x+19, g.note.y+g.note.h-2)
			if !m.dirty() || m.mode != ModeEdit {
				t.Fatal("cancel button failed")
			}
			m = key(m, tea.KeyMsg{Type: tea.KeyEsc})
			m = key(m, runeKey("f"))
			if m.mode != ModeConfirmUnsaved {
				t.Fatal("filter discarded draft")
			}
			m = key(m, runeKey("d"))
			if m.filter != FilterActive || m.editingID != "one" {
				t.Fatal("filter did not resume")
			}
		})
	}
}

func TestDividerPersistsOnlyOnReleaseAndResizePreservesPreference(t *testing.T) {
	m, _ := fixture(t)
	settings := &settingsMemory{ratio: .35}
	m.settings = &workspace.SettingsService{Repository: settings}
	m.editor.SetValue("draft")
	g := m.geometry()
	m = mouse(m, g.split, 5, tea.MouseButtonLeft, tea.MouseActionPress)
	m = mouse(m, 42, 5, tea.MouseButtonLeft, tea.MouseActionMotion)
	if settings.writes != 0 || m.geometry().split != 41 && m.geometry().split != 42 {
		t.Fatal("drag not live or wrote too early")
	}
	m = mouse(m, 42, 5, tea.MouseButtonLeft, tea.MouseActionRelease)
	if settings.writes != 1 || m.editor.Value() != "draft" {
		t.Fatal("release failed")
	}
	ratio := m.ratio
	n, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	m = n.(model)
	if m.geometry().wide || m.ratio != ratio {
		t.Fatal("resize replaced preference")
	}
	n, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	m = n.(model)
	if !m.geometry().wide || m.ratio != ratio {
		t.Fatal("wide restore failed")
	}
	settings.err = errors.New("read only")
	g = m.geometry()
	m = mouse(m, g.split, 5, tea.MouseButtonLeft, tea.MouseActionPress)
	m = mouse(m, 999, 5, tea.MouseButtonLeft, tea.MouseActionRelease)
	if m.ratio > .9 || !strings.Contains(m.status, "not saved") {
		t.Fatal(m.ratio, m.status)
	}
}

func TestListMouseScrollAndClickUsesOffset(t *testing.T) {
	m, _ := fixture(t)
	for i := 0; i < 40; i++ {
		m.records = append(m.records, state.Record{ID: strings.Repeat("x", i+1), Label: "Extra", Status: domain.WorkspaceActive})
	}
	g := m.geometry()
	m = mouse(m, 3, g.list.y+4, tea.MouseButtonWheelDown, tea.MouseActionPress)
	if m.listOffset != 3 || m.cursor != 0 {
		t.Fatal("scroll changed selection")
	}
	id := m.visible()[m.listOffset].ID
	for i := 0; i < 3; i++ {
		next, _ := m.Update(storedRows{records: append([]state.Record(nil), m.records...)})
		m = next.(model)
		if m.listOffset != 3 || m.cursor != 0 {
			t.Fatal("background polling moved the viewport or selection", m.listOffset, m.cursor)
		}
	}
	m = click(m, 2, g.list.y+3)
	if m.editingID != id {
		t.Fatal("click ignored scroll offset")
	}
}

func TestNoteMouseSelectionCoordinatesAcrossLayouts(t *testing.T) {
	for _, width := range []int{60, 100} {
		m, s := fixture(t)
		n, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		m = n.(model)
		g := m.geometry()
		m = click(m, g.note.x+1, g.note.y+1)
		m.editor.SetValue("中文abc")
		m.editor.DocumentStart()
		// The textarea's line-number gutter and prompt occupy four terminal cells.
		m.editor.ShowLineNumbers = false
		m.editor.Prompt = ""
		m.resize()
		m = mouse(m, g.note.x+1, g.note.y+1, tea.MouseButtonLeft, tea.MouseActionPress)
		m = mouse(m, g.note.x+5, g.note.y+1, tea.MouseButtonLeft, tea.MouseActionRelease)
		m = key(m, runeKey("replacement"))
		m = key(m, tea.KeyMsg{Type: tea.KeyCtrlS})
		note, _ := s.Load("one")
		if note.Text != "replacementabc" {
			t.Fatal(width, note.Text)
		}
	}
}

func TestSelectionReleaseCopiesAndCtrlCStillQuits(t *testing.T) {
	for _, dirty := range []bool{false, true} {
		m, _ := fixture(t)
		m.editor.ShowLineNumbers = false
		m.editor.Prompt = ""
		m.resize()
		if dirty {
			m.editor.SetValue("unsaved note")
			m.editor.DocumentStart()
		}
		g := m.geometry()
		n, _ := m.Update(tea.MouseMsg{X: g.note.x + 1, Y: g.note.y + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		m = n.(model)
		n, cmd := m.Update(tea.MouseMsg{X: g.note.x + 5, Y: g.note.y + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
		m = n.(model)
		if cmd == nil || m.mode != ModeEdit || m.selecting {
			t.Fatal("note selection did not return copy command")
		}
		n, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		m = n.(model)
		if dirty {
			if m.mode != ModeConfirmUnsaved || cmd != nil || m.pending.kind != ActionQuit {
				t.Fatal("Ctrl+C did not guard unsaved exit")
			}
		} else {
			if cmd == nil {
				t.Fatal("Ctrl+C did not quit")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatal("Ctrl+C copied instead of quitting")
			}
		}
	}
}

func TestRightClickAndCopyNotification(t *testing.T) {
	for _, width := range []int{60, 100} {
		m, _ := fixture(t)
		n, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		m = n.(model)
		m.editor.ShowLineNumbers = false
		m.editor.Prompt = ""
		m.resize()
		g := m.geometry()
		m = mouse(m, g.note.x+1, g.note.y+1, tea.MouseButtonLeft, tea.MouseActionPress)
		n, _ = m.Update(tea.MouseMsg{X: g.note.x + 5, Y: g.note.y + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
		m = n.(model)
		before := m.editor.Value()
		n, cmd := m.Update(tea.MouseMsg{X: g.note.x + 1, Y: g.note.y + 1, Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
		m = n.(model)
		if cmd == nil {
			t.Fatal("TUI did not route right-click to editor")
		}
		n, cmd = m.Update(textarea.ClipboardCopied{})
		m = n.(model)
		if cmd == nil || !strings.Contains(m.View(), "Clipboard") || !strings.Contains(m.View(), "Selection copied") {
			t.Fatal("missing copy feedback")
		}
		first := m.copyNoticeID
		n, _ = m.Update(textarea.ClipboardCopied{})
		m = n.(model)
		n, _ = m.Update(dismissCopyNotice{first})
		m = n.(model)
		if m.copyNotice == "" {
			t.Fatal("older timer dismissed newer notification")
		}
		n, _ = m.Update(dismissCopyNotice{m.copyNoticeID})
		m = n.(model)
		if m.copyNotice != "" || m.editor.Value() != before || m.dirty() {
			t.Fatal("notification altered note or did not expire")
		}
		n, cmd = m.Update(textarea.ClipboardCopyFailed{Err: errors.New("terminal closed")})
		m = n.(model)
		if cmd == nil || !strings.Contains(m.View(), "Could not copy") || !strings.Contains(m.status, "terminal closed") {
			t.Fatal("copy failure hidden")
		}
	}
}
