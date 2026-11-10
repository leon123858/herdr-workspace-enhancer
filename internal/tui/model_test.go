package tui

import (
	"context"
	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
	"herdr-workspace-enhancer/internal/state"
	"herdr-workspace-enhancer/internal/workspace"
	"strings"
	"testing"
)

func key(m Model, k tea.KeyMsg) Model { next, _ := m.Update(k); return next.(Model) }
func runeKey(s string) tea.KeyMsg     { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
func TestNoteEditingAndUnsavedPrompt(t *testing.T) {
	store, e := state.Open(t.TempDir(), "test")
	if e != nil {
		t.Fatal(e)
	}
	m := New(&workspace.Service{Store: store}, func(context.Context, string) error { return nil })
	m.records = []state.Record{{ID: "one", Label: "workspace", Status: "archived"}}
	m = key(m, runeKey("e"))
	if m.mode != "edit" {
		t.Fatal(m.mode)
	}
	m.editor.SetValue("中文\nline two")
	m = key(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != "dirty" {
		t.Fatal(m.mode)
	}
	m = key(m, runeKey("s"))
	if m.mode != "" {
		t.Fatal(m.mode)
	}
	note, e := store.Load("one")
	if e != nil || note.Text != "中文\nline two" {
		t.Fatal(note, e)
	}
	m = key(m, runeKey("e"))
	m.editor.SetValue("discard")
	m = key(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = key(m, runeKey("d"))
	note, e = store.Load("one")
	if e != nil || note.Text == "discard" {
		t.Fatal(note, e)
	}
}
func TestFilterAndSearch(t *testing.T) {
	m := New(nil, nil)
	m.records = []state.Record{{Label: "Alpha", Status: "active"}, {Label: "Beta", Status: "archived"}}
	m = key(m, tea.KeyMsg{Type: tea.KeyTab})
	if len(m.visible()) != 1 || m.visible()[0].Label != "Alpha" {
		t.Fatal(m.visible())
	}
	m.filter = 0
	m.search.SetValue("BETA")
	if len(m.visible()) != 1 {
		t.Fatal(m.visible())
	}
}

func TestLongNoteRoundTrip(t *testing.T) {
	store, e := state.Open(t.TempDir(), "test")
	if e != nil {
		t.Fatal(e)
	}
	text := strings.Repeat("中文長筆記\n", 149) + "最後一行"
	if _, e = store.Save("long", 0, text); e != nil {
		t.Fatal(e)
	}
	m := New(&workspace.Service{Store: store}, nil)
	m.records = []state.Record{{ID: "long", Status: "archived"}}
	m = key(m, runeKey("e"))
	if m.editor.Value() != text {
		t.Fatal("loading truncated note")
	}
	m.editor.CursorEnd()
	m = key(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = key(m, runeKey("新增一行"))
	m = key(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	got, e := store.Load("long")
	if e != nil || got.Text != text+"\n新增一行" {
		t.Fatal("save changed long note", e)
	}
}
func TestAsyncBlinkForwarding(t *testing.T) {
	for _, mode := range []string{"edit", "search"} {
		t.Run(mode, func(t *testing.T) {
			m := New(nil, nil)
			m.mode = mode
			if mode == "edit" {
				m.editor.Focus()
			} else {
				m.search.Focus()
			}
			_, cmd := m.Update(cursor.Blink())
			if cmd == nil {
				t.Fatal("component asynchronous message was dropped")
			}
		})
	}
}
