package tui

import (
	"context"
	"fmt"
	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
	domain "herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/repository/state"
	workspace "herdr-workspace-enhancer/service"
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
	t.Cleanup(func() { _ = store.Close() })
	m := New(&workspace.Service{Store: store}, func(context.Context, string) error { return nil })
	m.records = []state.Record{{ID: "one", Label: "workspace", Status: domain.WorkspaceArchived}}
	m = key(m, runeKey("e"))
	if m.mode != ModeEdit {
		t.Fatal(m.mode)
	}
	m.editor.SetValue("中文\nline two")
	m = key(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != ModeList || !m.dirty() {
		t.Fatal("focus change lost draft", m.mode)
	}
	m = key(m, runeKey("q"))
	if m.mode != ModeConfirmUnsaved {
		t.Fatal(m.mode)
	}
	m = key(m, runeKey("s"))
	if m.mode != ModeList {
		t.Fatal(m.mode)
	}
	note, e := store.Load("one")
	if e != nil || note.Text != "中文\nline two" {
		t.Fatal(note, e)
	}
	m = key(m, runeKey("e"))
	m.editor.SetValue("discard")
	m = key(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = key(m, runeKey("q"))
	m = key(m, runeKey("d"))
	note, e = store.Load("one")
	if e != nil || note.Text == "discard" {
		t.Fatal(note, e)
	}
}
func TestFilterAndSearch(t *testing.T) {
	m := New(nil, nil)
	m.records = []state.Record{{Label: "Alpha", Status: domain.WorkspaceActive}, {Label: "Beta", Status: domain.WorkspaceArchived}}
	m = key(m, runeKey("f"))
	if len(m.visible()) != 1 || m.visible()[0].Label != "Alpha" {
		t.Fatal(m.visible())
	}
	m = key(m, runeKey("f"))
	if m.filter != FilterArchived || len(m.visible()) != 1 || m.visible()[0].Label != "Beta" {
		t.Fatal("archived filter", m.visible())
	}
	m = key(m, runeKey("f"))
	if m.filter != FilterAll || len(m.visible()) != 2 {
		t.Fatal("filter cycle", m.visible())
	}
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
	t.Cleanup(func() { _ = store.Close() })
	text := strings.Repeat("中文長筆記\n", 149) + "最後一行"
	if _, e = store.Save("long", text); e != nil {
		t.Fatal(e)
	}
	m := New(&workspace.Service{Store: store}, nil)
	m.records = []state.Record{{ID: "long", Status: domain.WorkspaceArchived}}
	m = key(m, runeKey("e"))
	if m.editor.Value() != text {
		t.Fatal("loading truncated note")
	}
	m.editor.DocumentEnd()
	m = key(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = key(m, runeKey("新增一行"))
	m = key(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	got, e := store.Load("long")
	if e != nil || got.Text != text+"\n新增一行" {
		t.Fatal("save changed long note", e)
	}
}
func TestAsyncBlinkForwarding(t *testing.T) {
	for _, mode := range []Mode{ModeEdit, ModeSearch} {
		t.Run(string(mode), func(t *testing.T) {
			m := New(nil, nil)
			m.mode = mode
			if mode == ModeEdit {
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

func TestEventUpdatesPreserveSelectionAndEditor(t *testing.T) {
	m := New(nil, nil)
	m.records = []state.Record{{ID: "a", Status: domain.WorkspaceActive}, {ID: "b", Status: domain.WorkspaceActive}}
	m.cursor = 1
	next, cmd := m.Update(storedRows{records: []state.Record{{ID: "b", Label: "renamed", Status: domain.WorkspaceActive}}})
	m = next.(Model)
	if cmd == nil || m.cursor != 0 || len(m.records) != 1 || m.records[0].Label != "renamed" {
		t.Fatal(m.records, m.cursor)
	}
	m.mode = ModeEdit
	m.editingID = "b"
	m.noteLoaded = true
	m.editor.SetValue("unsaved note")
	next, cmd = m.Update(storedRows{})
	m = next.(Model)
	if cmd == nil || m.mode != ModeEdit || m.editor.Value() != "unsaved note" || len(m.records) != 0 {
		t.Fatal("background refresh interrupted editing")
	}
	m.mode = ModeConfirmArchive
	next, _ = m.Update(storedRows{})
	if next.(Model).mode != ModeConfirmArchive || len(next.(Model).records) != 0 {
		t.Fatal("confirmation changed")
	}
}

func TestFinishedReadsDBAndKeepsErrorOnPoll(t *testing.T) {
	store, e := state.Open(t.TempDir(), "test")
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	// A nil Host makes any accidental post-action Herdr sync fail immediately.
	m := New(&workspace.Service{Store: store}, nil)
	next, cmd := m.Update(finished{err: fmt.Errorf("close refused")})
	m = next.(Model)
	next, _ = m.Update(cmd())
	m = next.(Model)
	next, _ = m.Update(storedRows{})
	m = next.(Model)
	if m.status != "close refused" {
		t.Fatal(m.status)
	}
	m.records = []state.Record{{ID: "r", Status: domain.WorkspaceArchiving}}
	if cmd := m.execute(action{kind: ActionArchive}); cmd != nil {
		t.Fatal("archived in-progress workspace")
	}
	next, cmd = m.Update(finished{})
	if !strings.Contains(next.(Model).status, "waiting for sync") {
		t.Fatal(next.(Model).status)
	}
	if cmd == nil {
		t.Fatal("missing database refresh")
	}
}
