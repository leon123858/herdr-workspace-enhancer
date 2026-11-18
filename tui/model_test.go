package tui

import (
	"context"
	"errors"
	"fmt"
	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
	domain "herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/repository/state"
	workspace "herdr-workspace-enhancer/service"
	"strings"
	"testing"
)

func key(m model, k tea.KeyMsg) model { next, _ := m.Update(k); return next.(model) }

func runeKey(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestNoteEditingAndUnsavedPrompt(t *testing.T) {
	store, e := state.Open(t.TempDir(), "test")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = store.Close() })
	m := NewUI(&workspace.Service{Store: store}, func(context.Context, string) error { return nil }, nil).(model)
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
	m := NewUI(nil, nil, nil).(model)
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
	m := NewUI(&workspace.Service{Store: store}, nil, nil).(model)
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
			m := NewUI(nil, nil, nil).(model)
			m.mode = mode
			m.editor.Cursor.SetMode(cursor.CursorBlink)
			m.search.Cursor.SetMode(cursor.CursorBlink)
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
	m := NewUI(nil, nil, nil).(model)
	m.records = []state.Record{{ID: "a", Status: domain.WorkspaceActive}, {ID: "b", Status: domain.WorkspaceActive}}
	m.cursor = 1
	next, cmd := m.Update(storedRows{records: []state.Record{{ID: "b", Label: "renamed", Status: domain.WorkspaceActive}}})
	m = next.(model)
	if cmd == nil || m.cursor != 0 || len(m.records) != 1 || m.records[0].Label != "renamed" {
		t.Fatal(m.records, m.cursor)
	}
	m.mode = ModeEdit
	m.editingID = "b"
	m.noteLoaded = true
	m.editor.SetValue("unsaved note")
	next, cmd = m.Update(storedRows{})
	m = next.(model)
	if cmd == nil || m.mode != ModeEdit || m.editor.Value() != "unsaved note" || len(m.records) != 0 {
		t.Fatal("background refresh interrupted editing")
	}
	m.mode = ModeConfirmArchive
	next, _ = m.Update(storedRows{})
	if next.(model).mode != ModeConfirmArchive || len(next.(model).records) != 0 {
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
	m := NewUI(&workspace.Service{Store: store}, nil, nil).(model)
	next, cmd := m.Update(finished{err: fmt.Errorf("close refused")})
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	next, _ = m.Update(storedRows{})
	m = next.(model)
	if m.status != "close refused" {
		t.Fatal(m.status)
	}
	m.records = []state.Record{{ID: "r", Status: domain.WorkspaceArchiving}}
	if cmd := m.execute(action{kind: ActionArchive}); cmd != nil {
		t.Fatal("archived in-progress workspace")
	}
	next, cmd = m.Update(finished{})
	if !strings.Contains(next.(model).status, "waiting for sync") {
		t.Fatal(next.(model).status)
	}
	if cmd == nil {
		t.Fatal("missing database refresh")
	}
}

func TestDeleteArchivedConfirmation(t *testing.T) {
	m := NewUI(nil, nil, nil).(model)
	m.width, m.height = 100, 30
	m.records = []state.Record{{ID: "r", Label: "broken", Status: domain.WorkspaceArchived, ManagerState: domain.ManagerNone}}
	m = key(m, runeKey("d"))
	if m.mode != ModeConfirmDelete || m.deleteTarget.ID != "r" {
		t.Fatal(m.mode)
	}
	if !strings.Contains(m.View(), "Permanently removes") {
		t.Fatal(m.View())
	}
	next, _ := m.Update(storedRows{records: []state.Record{}})
	m = next.(model)
	if len(m.records) != 1 {
		t.Fatal("poll changed confirmation target")
	}
	m = key(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != ModeList || m.busy {
		t.Fatal(m.mode, m.busy)
	}
	m = key(m, runeKey("d"))
	next, cmd := m.Update(runeKey("y"))
	if !next.(model).busy || cmd == nil {
		t.Fatal("confirmation did not schedule deletion")
	}
	m.records[0].Status = domain.WorkspaceActive
	m.mode = ModeList
	m = key(m, runeKey("d"))
	if m.mode != ModeList {
		t.Fatal("active record deletion allowed")
	}
}

func fixture(t *testing.T) (model, *state.Store) {
	t.Helper()
	s, e := state.Open(t.TempDir(), "test")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	for id, text := range map[string]string{"one": "First note", "two": "Second note"} {
		if _, e = s.Save(id, text); e != nil {
			t.Fatal(e)
		}
	}
	m := NewUI(&workspace.Service{Store: s}, func(context.Context, string) error { return nil }, nil).(model)
	m.updateRows([]state.Record{{ID: "one", HostID: "h1", Label: "First", Status: domain.WorkspaceActive}, {ID: "two", Label: "Second", Status: domain.WorkspaceArchived}})
	return m, s
}

func mouse(m model, x, y int, b tea.MouseButton, a tea.MouseAction) model {
	v, _ := m.Update(tea.MouseMsg{X: x, Y: y, Button: b, Action: a})
	return v.(model)
}

func click(m model, x, y int) model {
	m = mouse(m, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
	return mouse(m, x, y, tea.MouseButtonLeft, tea.MouseActionRelease)
}

func TestSelectionPreviewAndDraftConfirmation(t *testing.T) {
	for _, choice := range []string{"s", "d", "esc"} {
		t.Run(choice, func(t *testing.T) {
			m, s := fixture(t)
			if m.editor.Value() != "First note" || !strings.Contains(m.View(), "First note") {
				t.Fatal("missing initial preview")
			}
			m = key(m, runeKey("e"))
			m.editor.SetValue("draft")
			m = key(m, tea.KeyMsg{Type: tea.KeyEsc})
			if m.mode != ModeList || !m.dirty() {
				t.Fatal("blur lost draft")
			}
			m = key(m, tea.KeyMsg{Type: tea.KeyDown})
			if m.mode != ModeConfirmUnsaved || m.editingID != "one" {
				t.Fatal("switched before confirmation")
			}
			if choice == "esc" {
				m = key(m, tea.KeyMsg{Type: tea.KeyEsc})
				if !m.dirty() || m.cursor != 0 {
					t.Fatal("cancel lost draft")
				}
				return
			}
			m = key(m, runeKey(choice))
			if m.editingID != "two" || m.editor.Value() != "Second note" {
				t.Fatal("pending selection not completed")
			}
			n, _ := s.Load("one")
			want := "First note"
			if choice == "s" {
				want = "draft"
			}
			if n.Text != want {
				t.Fatal(n)
			}
		})
	}
}

func TestDirtyGuardsAndEditorKeys(t *testing.T) {
	for _, k := range []string{"f", "/", "a", "enter", "q", "ctrl+c", "r"} {
		t.Run(k, func(t *testing.T) {
			m, _ := fixture(t)
			m.editor.SetValue("draft")
			var msg tea.KeyMsg
			switch k {
			case "enter":
				msg = tea.KeyMsg{Type: tea.KeyEnter}
			case "ctrl+c":
				msg = tea.KeyMsg{Type: tea.KeyCtrlC}
			default:
				msg = runeKey(k)
			}
			n, cmd := m.Update(msg)
			m = n.(model)
			if m.mode != ModeConfirmUnsaved || cmd != nil {
				t.Fatal("unguarded", k, m.mode)
			}
			m = key(m, tea.KeyMsg{Type: tea.KeyEsc})
			if !m.dirty() {
				t.Fatal("cancel lost text")
			}
		})
	}
	m, _ := fixture(t)
	m = key(m, runeKey("e"))
	m.editor.SetValue("")
	m = key(m, runeKey("qaf"))
	if m.editor.Value() != "qaf" || m.mode != ModeEdit {
		t.Fatal("editor triggered action")
	}
	m = key(m, tea.KeyMsg{Type: tea.KeyTab})
	if m.mode != ModeList || !m.dirty() {
		t.Fatal("tab discarded draft")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil {
		t.Fatal("quit without prompt")
	}
	m = key(m, runeKey("q"))
	n, cmd := m.Update(runeKey("s"))
	m = n.(model)
	if cmd == nil {
		t.Fatal("save did not resume quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("not quit")
	}
}

func TestLastWriterWinsAndExplicitReload(t *testing.T) {
	m, s := fixture(t)
	m.editor.SetValue("draft")
	if _, e := s.Save("one", "external"); e != nil {
		t.Fatal(e)
	}
	m = key(m, tea.KeyMsg{Type: tea.KeyDown})
	m = key(m, runeKey("s"))
	saved, e := s.Load("one")
	if e != nil || saved.Text != "draft" || m.editingID != "two" {
		t.Fatal("last writer did not save", saved, e)
	}
	m = key(m, tea.KeyMsg{Type: tea.KeyUp})
	m.editor.SetValue("discard this")
	if _, e = s.Save("one", "latest"); e != nil {
		t.Fatal(e)
	}
	g := m.geometry()
	m = click(m, g.note.x+9, g.note.y+g.note.h-2)
	if m.mode != ModeConfirmUnsaved {
		t.Fatal("discard did not prompt")
	}
	m = key(m, runeKey("d"))
	if m.editor.Value() != "latest" || m.dirty() {
		t.Fatal("discard did not reload latest note")
	}
}

type failingStore struct {
	*state.Store
	loadErr, saveErr error
}

func (s failingStore) Load(id string) (state.Note, error) {
	if s.loadErr != nil {
		return state.Note{}, s.loadErr
	}
	return s.Store.Load(id)
}

func (s failingStore) Save(id string, text string) (state.Note, error) {
	if s.saveErr != nil {
		return state.Note{}, s.saveErr
	}
	return s.Store.Save(id, text)
}

func TestNoteFailuresAndBackgroundUpdates(t *testing.T) {
	m, s := fixture(t)
	m.service.Store = failingStore{Store: s, loadErr: errors.New("unreadable")}
	m.updateRows(m.records)
	if m.noteLoaded || !strings.Contains(m.status, "Could not load") {
		t.Fatal("load failure hidden")
	}
	m = key(m, runeKey("e"))
	if m.mode == ModeEdit {
		t.Fatal("editing unreadable note")
	}
	m.service.Store = s
	m.syncNote(true)
	m.editor.SetValue("draft")
	m.service.Store = failingStore{Store: s, saveErr: errors.New("read only")}
	m = key(m, tea.KeyMsg{Type: tea.KeyDown})
	m = key(m, runeKey("s"))
	if m.editor.Value() != "draft" || m.cursor != 0 {
		t.Fatal("write failure lost draft")
	}
	m.service.Store = s
	n, _ := m.Update(storedRows{records: []state.Record{{ID: "two", Label: "Second", Status: domain.WorkspaceArchived}}})
	m = n.(model)
	if m.editingID != "one" || m.editor.Value() != "draft" || len(m.records) != 1 {
		t.Fatal("removed workspace lost draft")
	}
	m = key(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	note, _ := s.Load("one")
	if note.Text != "draft" {
		t.Fatal("could not save orphan draft")
	}
	m.syncNote(true)
	if m.editingID != "two" {
		t.Fatal("failed to select remaining note")
	}
	if _, e := s.Save("two", "external update"); e != nil {
		t.Fatal(e)
	}
	m.updateRows(m.records)
	if m.editor.Value() != "external update" {
		t.Fatal("clean note did not refresh")
	}
}

type settingsMemory struct {
	ratio  float64
	writes int
	err    error
}

func (s *settingsMemory) LoadRatio() (float64, error) { return s.ratio, s.err }

func (s *settingsMemory) SaveRatio(r float64) error { s.writes++; s.ratio = r; return s.err }

func TestBackgroundUpdatesClampScrollOffsetAndKeyboardRevealsSelection(t *testing.T) {
	m, _ := fixture(t)
	for i := 0; i < 40; i++ {
		m.records = append(m.records, state.Record{ID: strings.Repeat("x", i+1), Status: domain.WorkspaceActive})
	}
	g := m.geometry()
	for i := 0; i < 5; i++ {
		m = mouse(m, 3, g.list.y+4, tea.MouseButtonWheelDown, tea.MouseActionPress)
	}
	if m.listOffset != 15 {
		t.Fatal(m.listOffset)
	}
	// A shorter result must clamp the viewport, without snapping to row zero.
	next, _ := m.Update(storedRows{records: m.records[:m.listHeight()+4]})
	m = next.(model)
	if m.listOffset != 4 || m.cursor != 0 {
		t.Fatal("updated viewport was not clamped independently", m.listOffset, m.cursor)
	}
	m = key(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 1 || m.listOffset > m.cursor {
		t.Fatal("keyboard navigation did not reveal selection", m.listOffset, m.cursor)
	}
	next, _ = m.Update(storedRows{})
	m = next.(model)
	if m.listOffset != 0 || m.cursor != 0 {
		t.Fatal("empty list retained invalid viewport", m.listOffset, m.cursor)
	}
}

func TestSearchEmptyStateAndClickFilter(t *testing.T) {
	m, _ := fixture(t)
	m = key(m, runeKey("/"))
	m = key(m, runeKey("second"))
	if m.editingID != "two" || len(m.visible()) != 1 {
		t.Fatal("search did not preview selection")
	}
	m = key(m, runeKey(" missing"))
	if len(m.visible()) != 0 || m.editingID != "" {
		t.Fatal("stale preview on empty results")
	}
	if !strings.Contains(m.View(), "Select a workspace") || !strings.Contains(m.View(), "No matching") {
		t.Fatal(m.View())
	}
	m.search.SetValue("")
	m = key(m, tea.KeyMsg{Type: tea.KeyEsc})
	m.syncNote(false)
	g := m.geometry()
	m = click(m, 2, g.list.y+1)
	if m.filter != FilterActive || len(m.visible()) != 1 {
		t.Fatal("filter click failed")
	}
	m = click(m, 2, g.list.y+2)
	if m.mode != ModeSearch {
		t.Fatal("search click failed")
	}
}

func TestSaveResumesWorkspaceFocusAndArchivePreview(t *testing.T) {
	m, s := fixture(t)
	focused := ""
	m.focus = func(_ context.Context, id string) error { focused = id; return nil }
	m.editor.SetValue("draft")
	n, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = n.(model)
	n, cmd := m.Update(runeKey("s"))
	m = n.(model)
	if cmd == nil {
		t.Fatal("focus did not resume")
	}
	if msg := cmd().(finished); msg.err != nil || focused != "h1" {
		t.Fatal(msg, focused)
	}
	note, _ := s.Load("one")
	if note.Text != "draft" {
		t.Fatal("focused before saving")
	}
	n, _ = m.Update(finished{})
	m = n.(model)
	m.service.MutationError = errors.New("archive unavailable")
	m.editor.SetValue("next draft")
	m = key(m, runeKey("a"))
	n, cmd = m.Update(runeKey("s"))
	m = n.(model)
	if cmd == nil {
		t.Fatal("archive preview did not resume")
	}
	msg := cmd().(previewed)
	if msg.err == nil {
		t.Fatal("expected capability error")
	}
}
