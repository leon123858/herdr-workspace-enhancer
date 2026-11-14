// Package tui implements the keyboard- and mouse-driven workspace manager.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	state "herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/model/layout"
	workspace "herdr-workspace-enhancer/service"
	textarea "herdr-workspace-enhancer/tui/noteeditor"

	keybind "github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type refreshed struct {
	records []state.Record
	err     error
}
type previewed struct {
	preview workspace.ArchivePreview
	err     error
}
type finished struct{ err error }
type pollState struct{}
type dismissCopyNotice struct{ id uint64 }
type storedRows struct {
	records []state.Record
	err     error
}
type action struct {
	kind ActionKind
	id   string
}

func pollLater() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return pollState{} })
}

type Model struct {
	Settings                  *workspace.SettingsService
	Service                   *workspace.Service
	Focus                     func(context.Context, string) error
	records                   []state.Record
	cursor, listOffset        int
	filter                    Filter
	mode                      Mode
	returnMode                Mode
	pending                   action
	editor                    textarea.Model
	search                    textinput.Model
	editingID, editingLabel   string
	note                      state.Note
	noteLoaded                bool
	preview                   workspace.ArchivePreview
	status                    string
	copyNotice                string
	copyNoticeID              uint64
	busy, dragging, selecting bool
	ratio                     float64
	width, height             int
}

func New(s *workspace.Service, focus func(context.Context, string) error) Model {
	lipgloss.SetHasDarkBackground(true)
	editor := textarea.New()
	editor.CharLimit = 0
	editor.MaxHeight = 0
	editor.MaxWidth = 0
	search := textinput.New()
	search.Placeholder = "Search workspaces"
	search.CharLimit = 200
	m := Model{Service: s, Focus: focus, editor: editor, search: search, ratio: state.DefaultWorkspaceRatio, width: 80, height: 24}
	if s != nil && s.MutationError != nil {
		m.status = s.MutationError.Error()
	}
	m.resize()
	return m
}
func (m Model) refresh() tea.Cmd {
	return func() tea.Msg { r, e := m.Service.Refresh(context.Background()); return refreshed{r, e} }
}
func (m Model) Init() tea.Cmd { return tea.Batch(m.refresh(), pollLater()) }
func (m Model) visible() []state.Record {
	var rows []state.Record
	for _, r := range m.records {
		if m.filter == FilterActive && r.Status != state.WorkspaceActive || m.filter == FilterArchived && r.Status != state.WorkspaceArchived {
			continue
		}
		if strings.Contains(strings.ToLower(r.Label), strings.ToLower(m.search.Value())) {
			rows = append(rows, r)
		}
	}
	return rows
}
func (m Model) selected() (state.Record, bool) {
	rows := m.visible()
	if len(rows) == 0 {
		return state.Record{}, false
	}
	return rows[max(0, min(m.cursor, len(rows)-1))], true
}
func (m Model) dirty() bool { return m.editingID != "" && m.editor.Value() != m.note.Text }

// syncNote only replaces clean content. A removed workspace can retain a draft.
func (m *Model) syncNote(reload bool) {
	if m.dirty() {
		return
	}
	r, ok := m.selected()
	if !ok {
		m.editingID = ""
		m.editingLabel = ""
		m.note = state.Note{}
		m.noteLoaded = false
		m.editor.SetValue("")
		return
	}
	if m.Service == nil {
		return
	}
	if !reload && m.editingID == r.ID && m.noteLoaded {
		m.editingLabel = r.Label
		return
	}
	n, e := m.Service.Load(r.ID)
	if e != nil {
		if m.editingID != r.ID {
			m.editor.SetValue("")
			m.note = state.Note{}
		}
		m.editingID = r.ID
		m.editingLabel = r.Label
		m.noteLoaded = false
		m.status = "Could not load note: " + e.Error()
		return
	}
	changed := m.editingID != r.ID || n != m.note
	m.note = n
	m.editingID = r.ID
	m.editingLabel = r.Label
	m.noteLoaded = true
	if changed {
		m.editor.SetValue(n.Text)
		m.editor.DocumentStart()
	}
}
func (m *Model) save() bool {
	if !m.noteLoaded || m.Service == nil {
		m.status = "Load the note successfully before saving"
		return false
	}
	n, e := m.Service.Save(m.editingID, m.editor.Value())
	if e != nil {
		m.status = "Could not save note: " + e.Error()
		return false
	}
	m.note = n
	m.status = "Note saved"
	return true
}
func (m *Model) discard() bool {
	if m.Service == nil {
		return false
	}
	n, e := m.Service.Load(m.editingID)
	if e != nil {
		m.status = "Could not reload note: " + e.Error()
		return false
	}
	m.note = n
	m.editor.SetValue(n.Text)
	m.noteLoaded = true
	m.status = "Changes discarded"
	return true
}
func (m *Model) setMode(mode Mode) tea.Cmd {
	m.mode = mode
	m.editor.Blur()
	m.search.Blur()
	if mode == ModeEdit {
		return m.editor.Focus()
	}
	if mode == ModeSearch {
		return m.search.Focus()
	}
	return nil
}
func (m *Model) request(a action) tea.Cmd {
	if a.kind == ActionSelect {
		r, ok := m.selected()
		if ok && r.ID == a.id {
			m.ensureVisible()
			return m.setMode(ModeList)
		}
	}
	if m.dirty() {
		m.pending = a
		m.returnMode = m.mode
		m.setMode(ModeConfirmUnsaved)
		return nil
	}
	return m.execute(a)
}
func (m *Model) execute(a action) tea.Cmd {
	switch a.kind {
	case ActionSelect:
		for i, r := range m.visible() {
			if r.ID == a.id {
				m.cursor = i
				break
			}
		}
		m.setMode(ModeList)
		m.ensureVisible()
		m.syncNote(false)
	case ActionFilter:
		m.filter = m.filter.Next()
		m.cursor = 0
		m.listOffset = 0
		m.setMode(ModeList)
		m.syncNote(false)
	case ActionSearch:
		return m.setMode(ModeSearch)
	case ActionQuit:
		return tea.Quit
	case ActionDiscard:
		m.syncNote(false)
	case ActionRefresh:
		m.busy = true
		return m.refresh()
	case ActionArchive:
		r, ok := m.selected()
		if ok && r.Status == state.WorkspaceActive {
			m.busy = true
			return func() tea.Msg { p, e := m.Service.Preview(context.Background(), r.ID); return previewed{p, e} }
		}
	case ActionActivate:
		r, ok := m.selected()
		if ok {
			m.busy = true
			return func() tea.Msg {
				if r.Status == state.WorkspaceArchived {
					return finished{m.Service.Restore(context.Background(), r.ID)}
				}
				if r.Status == state.WorkspaceActive && m.Focus != nil {
					return finished{m.Service.Focus(context.Background(), r.HostID, m.Focus)}
				}
				return finished{fmt.Errorf("workspace is %s", r.Status)}
			}
		}
	}
	return nil
}
func (m *Model) confirm(decision Confirmation) tea.Cmd {
	a := m.pending
	switch decision {
	case ConfirmationSave:
		if !m.save() {
			m.pending = action{}
			return m.setMode(m.returnMode)
		}
	case ConfirmationDiscard:
		if !m.discard() {
			m.pending = action{}
			return m.setMode(m.returnMode)
		}
	case ConfirmationCancel:
		m.pending = action{}
		return m.setMode(m.returnMode)
	default:
		return nil
	}
	m.pending = action{}
	m.setMode(m.returnMode)
	return m.execute(a)
}
func (m *Model) updateRows(rows []state.Record) {
	selected, had := m.selected()
	m.records = rows
	m.cursor = min(m.cursor, max(0, len(m.visible())-1))
	if had {
		for i, r := range m.visible() {
			if r.ID == selected.ID {
				m.cursor = i
				break
			}
		}
	}
	// Background updates preserve independent mouse scrolling. Only clamp the
	// offset if the visible list shrinks; explicit navigation reveals selection.
	m.listOffset = max(0, min(m.listOffset, max(0, len(m.visible())-m.listHeight())))
	m.syncNote(true)
}
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case textarea.ClipboardCopied:
		m.copyNotice = "Selection copied"
		m.copyNoticeID++
		id := m.copyNoticeID
		return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return dismissCopyNotice{id} })
	case textarea.ClipboardCopyFailed:
		m.copyNotice = "Could not copy selection"
		m.status = "Could not copy selection: " + v.Err.Error()
		m.copyNoticeID++
		id := m.copyNoticeID
		return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return dismissCopyNotice{id} })
	case dismissCopyNotice:
		if v.id == m.copyNoticeID {
			m.copyNotice = ""
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width = max(1, v.Width)
		m.height = max(1, v.Height)
		m.dragging = false
		m.selecting = false
		m.resize()
		return m, nil
	case pollState:
		if m.busy || m.mode == ModeConfirmArchive || m.mode == ModeConfirmUnsaved || m.Service == nil {
			return m, pollLater()
		}
		return m, func() tea.Msg { r, e := m.Service.StoredRows(); return storedRows{r, e} }
	case storedRows:
		if !m.busy && m.mode != ModeConfirmArchive && m.mode != ModeConfirmUnsaved {
			if v.err != nil {
				m.status = v.err.Error()
			} else {
				m.updateRows(v.records)
			}
		}
		return m, pollLater()
	case refreshed:
		m.busy = false
		if v.err != nil {
			m.status = v.err.Error()
		} else {
			m.updateRows(v.records)
		}
		return m, nil
	case previewed:
		m.busy = false
		if v.err != nil {
			m.status = v.err.Error()
		} else {
			m.preview = v.preview
			m.setMode(ModeConfirmArchive)
		}
		return m, nil
	case finished:
		m.busy = false
		if v.err != nil {
			m.status = v.err.Error()
		} else {
			m.status = "Operation accepted; waiting for sync"
		}
		return m, func() tea.Msg { r, e := m.Service.StoredRows(); return refreshed{r, e} }
	case tea.MouseMsg:
		return m.handleMouse(v)
	case tea.KeyMsg:
		if m.mode == ModeConfirmUnsaved {
			cmd := m.confirm(confirmationFor(v))
			return m, cmd
		}
		if keybind.Matches(v, keys.Exit) {
			cmd := m.request(action{kind: ActionQuit})
			return m, cmd
		}
		if m.busy {
			return m, nil
		}
		if m.mode == ModeConfirmArchive {
			cmd := m.confirmArchive(keybind.Matches(v, keys.ConfirmArchive))
			return m, cmd
		}
		if keybind.Matches(v, keys.Save) {
			m.save()
			return m, nil
		}
		if m.mode == ModeEdit {
			if keybind.Matches(v, keys.LeaveNote) {
				m.setMode(ModeList)
				return m, nil
			}
			var cmd tea.Cmd
			m.editor, cmd = m.editor.Update(v)
			return m, cmd
		}
		if m.mode == ModeSearch {
			if keybind.Matches(v, keys.FinishSearch) {
				m.setMode(ModeList)
				return m, nil
			}
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(v)
			m.cursor = 0
			m.listOffset = 0
			m.syncNote(false)
			return m, cmd
		}
		switch {
		case keybind.Matches(v, keys.Quit):
			cmd := m.request(action{kind: ActionQuit})
			return m, cmd
		case keybind.Matches(v, keys.Up, keys.Down):
			rows := m.visible()
			i := m.cursor
			if keybind.Matches(v, keys.Up) {
				i--
			} else {
				i++
			}
			if len(rows) > 0 {
				i = max(0, min(i, len(rows)-1))
				cmd := m.request(action{kind: ActionSelect, id: rows[i].ID})
				return m, cmd
			}
		case keybind.Matches(v, keys.FocusNote):
			m.syncNote(false)
			if m.noteLoaded {
				cmd := m.setMode(ModeEdit)
				return m, cmd
			}
		case keybind.Matches(v, keys.Filter):
			cmd := m.request(action{kind: ActionFilter})
			return m, cmd
		case keybind.Matches(v, keys.Search):
			cmd := m.request(action{kind: ActionSearch})
			return m, cmd
		case keybind.Matches(v, keys.Refresh):
			cmd := m.request(action{kind: ActionRefresh})
			return m, cmd
		case keybind.Matches(v, keys.Archive):
			cmd := m.request(action{kind: ActionArchive})
			return m, cmd
		case keybind.Matches(v, keys.Activate):
			cmd := m.request(action{kind: ActionActivate})
			return m, cmd
		}
	}
	var cmd tea.Cmd
	if m.mode == ModeEdit {
		m.editor, cmd = m.editor.Update(msg)
	} else if m.mode == ModeSearch {
		m.search, cmd = m.search.Update(msg)
	}
	return m, cmd
}
func paneCount(n *layout.Node) int {
	if n == nil {
		return 0
	}
	if n.Type == layout.NodePane {
		return 1
	}
	return paneCount(n.First) + paneCount(n.Second)
}
func (m *Model) SetRatio() {
	if m.Settings != nil {
		r, e := m.Settings.LoadRatio()
		m.ratio = r
		if e != nil {
			m.status = "Could not load settings: " + e.Error()
		}
	}
	m.resize()
}

func (m *Model) confirmArchive(confirmed bool) tea.Cmd {
	m.setMode(ModeList)
	if !confirmed {
		return nil
	}
	m.busy = true
	preview := m.preview
	return func() tea.Msg { return finished{m.Service.ArchiveConfirmed(context.Background(), preview)} }
}
