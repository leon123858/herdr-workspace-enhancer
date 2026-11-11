// Package tui implements the keyboard-driven workspace manager.
package tui

import (
	"context"
	"fmt"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"herdr-workspace-enhancer/internal/layout"
	"herdr-workspace-enhancer/internal/state"
	"herdr-workspace-enhancer/internal/workspace"
	"strings"
	"time"
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
type storedRows struct {
	records []state.Record
	err     error
}

func pollLater() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return pollState{} })
}

type Model struct {
	Service       *workspace.Service
	Focus         func(context.Context, string) error
	records       []state.Record
	cursor        int
	filter        int
	mode          string
	editor        textarea.Model
	search        textinput.Model
	editingID     string
	note          state.Note
	preview       workspace.ArchivePreview
	status        string
	busy          bool
	width, height int
}

func New(s *workspace.Service, focus func(context.Context, string) error) Model {
	// Herdr panes may have no attached terminal answering background-color queries.
	lipgloss.SetHasDarkBackground(true)
	editor := textarea.New()
	editor.CharLimit = 0
	editor.MaxHeight = 0
	editor.SetWidth(76)
	editor.SetHeight(16)
	search := textinput.New()
	search.Placeholder = "搜尋 workspace"
	search.CharLimit = 200
	m := Model{Service: s, Focus: focus, editor: editor, search: search, width: 80, height: 24}
	if s != nil && s.MutationError != nil {
		m.status = s.MutationError.Error()
	}
	return m
}
func (m Model) refresh() tea.Cmd {
	return func() tea.Msg { records, e := m.Service.Refresh(context.Background()); return refreshed{records, e} }
}
func (m Model) Init() tea.Cmd { return tea.Batch(m.refresh(), pollLater()) }
func (m Model) visible() []state.Record {
	var rows []state.Record
	for _, r := range m.records {
		if m.filter == 1 && r.Status != "active" {
			continue
		}
		if m.filter == 2 && r.Status != "archived" {
			continue
		}
		if !strings.Contains(strings.ToLower(r.Label), strings.ToLower(m.search.Value())) {
			continue
		}
		rows = append(rows, r)
	}
	return rows
}
func (m Model) selected() (state.Record, bool) {
	rows := m.visible()
	if len(rows) == 0 {
		return state.Record{}, false
	}
	return rows[min(m.cursor, len(rows)-1)], true
}
func (m *Model) save() bool {
	n, e := m.Service.Store.Save(m.editingID, m.note.Revision, m.editor.Value())
	if e != nil {
		m.status = e.Error()
		return false
	}
	m.note = n
	m.status = "筆記已儲存"
	return true
}
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case pollState:
		if m.busy || m.mode != "" {
			return m, pollLater()
		}
		return m, func() tea.Msg {
			rows, err := m.Service.StoredRows()
			return storedRows{rows, err}
		}
	case storedRows:
		// Do not replace a note editor or archive confirmation while it is open.
		if !m.busy && m.mode == "" {
			if v.err != nil {
				m.status = v.err.Error()
			} else {
				selected, hadSelection := m.selected()
				m.records = v.records
				m.cursor = min(m.cursor, max(0, len(m.visible())-1))
				if hadSelection {
					for i, row := range m.visible() {
						if row.ID == selected.ID {
							m.cursor = i
							break
						}
					}
				}
			}
		}
		return m, pollLater()
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
		m.editor.SetWidth(max(10, v.Width-4))
		m.editor.SetHeight(max(3, v.Height-8))
		return m, nil
	case previewed:
		m.busy = false
		if v.err != nil {
			m.status = v.err.Error()
		} else {
			m.preview = v.preview
			m.mode = "archive"
		}
		return m, nil
	case refreshed:
		m.busy = false
		if v.err != nil {
			m.status = v.err.Error()
		} else {
			m.records = v.records
			m.cursor = min(m.cursor, max(0, len(m.visible())-1))
		}
		return m, nil
	case finished:
		m.busy = false
		if v.err != nil {
			m.status = v.err.Error()
		} else {
			m.status = "操作完成"
		}
		return m, m.refresh()
	case tea.KeyMsg:
		key := v.String()
		if m.busy {
			return m, nil
		}
		if m.mode == "dirty" {
			switch key {
			case "s":
				if m.save() {
					m.mode = ""
				}
			case "d":
				m.mode = ""
			case "esc":
				m.mode = "edit"
			}
			return m, nil
		}
		if m.mode == "edit" {
			switch key {
			case "ctrl+s":
				m.save()
				return m, nil
			case "esc", "ctrl+c":
				if m.editor.Value() != m.note.Text {
					m.mode = "dirty"
				} else {
					m.mode = ""
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.editor, cmd = m.editor.Update(msg)
			return m, cmd
		}
		if m.mode == "search" {
			if key == "enter" || key == "esc" {
				m.mode = ""
				m.search.Blur()
				return m, nil
			}
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			m.cursor = 0
			return m, cmd
		}
		if m.mode == "archive" {
			m.mode = ""
			if key == "y" {
				preview := m.preview
				m.busy = true
				return m, func() tea.Msg { return finished{m.Service.ArchiveConfirmed(context.Background(), preview)} }
			}
			return m, nil
		}
		switch key {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			m.cursor = max(0, m.cursor-1)
		case "down", "j":
			m.cursor = min(max(0, len(m.visible())-1), m.cursor+1)
		case "tab":
			m.filter = (m.filter + 1) % 3
			m.cursor = 0
		case "/":
			m.mode = "search"
			return m, m.search.Focus()
		case "r":
			m.busy = true
			return m, m.refresh()
		case "e":
			if r, ok := m.selected(); ok {
				n, e := m.Service.Store.Load(r.ID)
				if e != nil {
					m.status = e.Error()
					return m, nil
				}
				m.note = n
				m.editingID = r.ID
				m.editor.SetValue(n.Text)
				m.mode = "edit"
				return m, m.editor.Focus()
			}
		case "a":
			if r, ok := m.selected(); ok && (r.Status == "active" || r.Status == "archiving") {
				m.busy = true
				return m, func() tea.Msg {
					snapshot, e := m.Service.Preview(context.Background(), r.ID)
					return previewed{snapshot, e}
				}
			}
		case "enter":
			if r, ok := m.selected(); ok {
				m.busy = true
				return m, func() tea.Msg {
					if r.Status == "archived" || r.Status == "restoring" {
						return finished{m.Service.Restore(context.Background(), r.ID)}
					}
					if r.Status == "active" {
						return finished{m.Focus(context.Background(), r.HostID)}
					}
					return finished{fmt.Errorf("workspace is %s", r.Status)}
				}
			}
		}
	}
	var cmd tea.Cmd
	switch m.mode {
	case "edit":
		m.editor, cmd = m.editor.Update(msg)
	case "search":
		m.search, cmd = m.search.Update(msg)
		m.cursor = 0
	}
	return m, cmd
}
func (m Model) View() string {
	var b strings.Builder
	b.WriteString("Herdr Workspace Enhancer\n\n")
	if m.mode == "edit" || m.mode == "dirty" {
		b.WriteString(m.editor.View())
		b.WriteString("\nCtrl+S 儲存 · Esc 返回\n")
		if m.mode == "dirty" {
			b.WriteString("尚未儲存：s 儲存並返回 · d 捨棄 · Esc 繼續編輯\n")
		}
	} else {
		fmt.Fprintf(&b, "篩選：%s  %s\n\n", []string{"全部", "啟用中", "已封存"}[m.filter], m.search.View())
		rows := m.visible()
		start := max(0, m.cursor-max(1, m.height-12)+1)
		end := min(len(rows), start+max(1, m.height-12))
		for i := start; i < end; i++ {
			r := rows[i]
			mark := " "
			if i == m.cursor {
				mark = ">"
			}
			fmt.Fprintf(&b, "%s [%s] %s\n", mark, r.Status, r.Label)
		}
		if len(rows) == 0 {
			b.WriteString("沒有符合條件的 workspace\n")
		}
		b.WriteString("\n↑↓ 選取 · Tab 篩選 · / 搜尋 · e 筆記 · a 封存\nEnter 啟用／切換 · r 重新整理 · q 離開\n")
		if m.mode == "archive" {
			for _, tab := range m.preview.Snapshot.Tabs {
				fmt.Fprintf(&b, "\n%s — %d panes", tab.Label, paneCount(tab.Root))
			}
			b.WriteString("\n封存會關閉此 workspace 的所有 tabs、panes 與前景程式。\n重新啟用只重建 shell，不恢復執行中程式。y 確認／其他鍵取消\n")
		}
	}
	if m.busy {
		b.WriteString("\n處理中…\n")
	}
	if m.status != "" {
		b.WriteString("\n" + m.status + "\n")
	}
	return b.String()
}

func paneCount(n *layout.Node) int {
	if n == nil {
		return 0
	}
	if n.Type == "pane" {
		return 1
	}
	return paneCount(n.First) + paneCount(n.Second)
}
