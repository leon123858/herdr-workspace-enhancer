package tui

import (
	"context"
	"errors"
	domain "herdr-workspace-enhancer/model"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	state "herdr-workspace-enhancer/repository/state"
	workspace "herdr-workspace-enhancer/service"
	textarea "herdr-workspace-enhancer/tui/noteeditor"
)

func fixture(t *testing.T) (Model, *state.Store) {
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
	m := New(&workspace.Service{Store: s}, func(context.Context, string) error { return nil })
	m.updateRows([]state.Record{{ID: "one", HostID: "h1", Label: "First", Status: domain.WorkspaceActive}, {ID: "two", Label: "Second", Status: domain.WorkspaceArchived}})
	return m, s
}
func mouse(m Model, x, y int, b tea.MouseButton, a tea.MouseAction) Model {
	v, _ := m.Update(tea.MouseMsg{X: x, Y: y, Button: b, Action: a})
	return v.(Model)
}
func click(m Model, x, y int) Model {
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
			m = n.(Model)
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
	m = n.(Model)
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
	m.Service.Store = failingStore{Store: s, loadErr: errors.New("unreadable")}
	m.updateRows(m.records)
	if m.noteLoaded || !strings.Contains(m.status, "Could not load") {
		t.Fatal("load failure hidden")
	}
	m = key(m, runeKey("e"))
	if m.mode == ModeEdit {
		t.Fatal("editing unreadable note")
	}
	m.Service.Store = s
	m.syncNote(true)
	m.editor.SetValue("draft")
	m.Service.Store = failingStore{Store: s, saveErr: errors.New("read only")}
	m = key(m, tea.KeyMsg{Type: tea.KeyDown})
	m = key(m, runeKey("s"))
	if m.editor.Value() != "draft" || m.cursor != 0 {
		t.Fatal("write failure lost draft")
	}
	m.Service.Store = s
	n, _ := m.Update(storedRows{records: []state.Record{{ID: "two", Label: "Second", Status: domain.WorkspaceArchived}}})
	m = n.(Model)
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
func TestResponsiveLayoutAndMouseControls(t *testing.T) {
	for _, width := range []int{1, 40, 79, 80, 120} {
		t.Run(string(rune(width)), func(t *testing.T) {
			m, _ := fixture(t)
			n, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 32})
			m = n.(Model)
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

type settingsMemory struct {
	ratio  float64
	writes int
	err    error
}

func (s *settingsMemory) LoadRatio() (float64, error) { return s.ratio, s.err }
func (s *settingsMemory) SaveRatio(r float64) error   { s.writes++; s.ratio = r; return s.err }
func TestDividerPersistsOnlyOnReleaseAndResizePreservesPreference(t *testing.T) {
	m, _ := fixture(t)
	settings := &settingsMemory{ratio: .35}
	m.Settings = &workspace.SettingsService{Repository: settings}
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
	m = n.(Model)
	if m.geometry().wide || m.ratio != ratio {
		t.Fatal("resize replaced preference")
	}
	n, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	m = n.(Model)
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
		m = next.(Model)
		if m.listOffset != 3 || m.cursor != 0 {
			t.Fatal("background polling moved the viewport or selection", m.listOffset, m.cursor)
		}
	}
	m = click(m, 2, g.list.y+3)
	if m.editingID != id {
		t.Fatal("click ignored scroll offset")
	}
}

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
	m = next.(Model)
	if m.listOffset != 4 || m.cursor != 0 {
		t.Fatal("updated viewport was not clamped independently", m.listOffset, m.cursor)
	}
	m = key(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 1 || m.listOffset > m.cursor {
		t.Fatal("keyboard navigation did not reveal selection", m.listOffset, m.cursor)
	}
	next, _ = m.Update(storedRows{})
	m = next.(Model)
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
	m.Focus = func(_ context.Context, id string) error { focused = id; return nil }
	m.editor.SetValue("draft")
	n, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = n.(Model)
	n, cmd := m.Update(runeKey("s"))
	m = n.(Model)
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
	m = n.(Model)
	m.Service.MutationError = errors.New("archive unavailable")
	m.editor.SetValue("next draft")
	m = key(m, runeKey("a"))
	n, cmd = m.Update(runeKey("s"))
	m = n.(Model)
	if cmd == nil {
		t.Fatal("archive preview did not resume")
	}
	msg := cmd().(previewed)
	if msg.err == nil {
		t.Fatal("expected capability error")
	}
}
func TestNoteMouseSelectionCoordinatesAcrossLayouts(t *testing.T) {
	for _, width := range []int{60, 100} {
		m, s := fixture(t)
		n, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		m = n.(Model)
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
		m = n.(Model)
		n, cmd := m.Update(tea.MouseMsg{X: g.note.x + 5, Y: g.note.y + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
		m = n.(Model)
		if cmd == nil || m.mode != ModeEdit || m.selecting {
			t.Fatal("note selection did not return copy command")
		}
		n, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		m = n.(Model)
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
		m = n.(Model)
		m.editor.ShowLineNumbers = false
		m.editor.Prompt = ""
		m.resize()
		g := m.geometry()
		m = mouse(m, g.note.x+1, g.note.y+1, tea.MouseButtonLeft, tea.MouseActionPress)
		n, _ = m.Update(tea.MouseMsg{X: g.note.x + 5, Y: g.note.y + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
		m = n.(Model)
		before := m.editor.Value()
		n, cmd := m.Update(tea.MouseMsg{X: g.note.x + 1, Y: g.note.y + 1, Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
		m = n.(Model)
		if cmd == nil {
			t.Fatal("TUI did not route right-click to editor")
		}
		n, cmd = m.Update(textarea.ClipboardCopied{})
		m = n.(Model)
		if cmd == nil || !strings.Contains(m.View(), "Clipboard") || !strings.Contains(m.View(), "Selection copied") {
			t.Fatal("missing copy feedback")
		}
		first := m.copyNoticeID
		n, _ = m.Update(textarea.ClipboardCopied{})
		m = n.(Model)
		n, _ = m.Update(dismissCopyNotice{first})
		m = n.(Model)
		if m.copyNotice == "" {
			t.Fatal("older timer dismissed newer notification")
		}
		n, _ = m.Update(dismissCopyNotice{m.copyNoticeID})
		m = n.(Model)
		if m.copyNotice != "" || m.editor.Value() != before || m.dirty() {
			t.Fatal("notification altered note or did not expire")
		}
		n, cmd = m.Update(textarea.ClipboardCopyFailed{Err: errors.New("terminal closed")})
		m = n.(Model)
		if cmd == nil || !strings.Contains(m.View(), "Could not copy") || !strings.Contains(m.status, "terminal closed") {
			t.Fatal("copy failure hidden")
		}
	}
}
