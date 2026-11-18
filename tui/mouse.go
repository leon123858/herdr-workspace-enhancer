package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"time"
)

func (m model) handleMouse(v tea.MouseMsg) (tea.Model, tea.Cmd) {
	g := m.geometry()
	press := v.Action == tea.MouseActionPress && v.Button == tea.MouseButtonLeft
	if m.recordDrag != "" {
		m.dragY = v.Y
		if v.Action == tea.MouseActionRelease {
			source := m.recordDrag
			m.recordDrag = ""
			if m.dragActive && g.list.contains(v.X, v.Y) && v.Y >= g.list.y+3 && v.Y < g.list.y+g.list.h-1 && m.dragTarget != "" {
				cmd := m.moveRecord(source, m.dragTarget, m.dragBefore)
				return m, cmd
			}
			return m, nil
		}
		if v.Action == tea.MouseActionMotion {
			m.dragActive = true
			m.setDragTarget()
		}
		return m, nil
	}
	if m.busy {
		return m, nil
	}
	if m.mode == ModeConfirmDelete || m.mode == ModeConfirmUnsaved || m.mode == ModeConfirmArchive {
		if press && v.Y == g.note.y+g.note.h-2 {
			x := v.X - g.note.x - 1
			if m.mode == ModeConfirmUnsaved {
				decision := ConfirmationNone
				if x >= 0 && x < 6 {
					decision = ConfirmationSave
				} else if x >= 7 && x < 16 {
					decision = ConfirmationDiscard
				} else if x >= 17 && x < 25 {
					decision = ConfirmationCancel
				}
				cmd := m.confirm(decision)
				return m, cmd
			}
			if x >= 0 && x < 9 {
				var cmd tea.Cmd
				if m.mode == ModeConfirmDelete {
					cmd = m.confirmDelete(true)
				} else {
					cmd = m.confirmArchive(true)
				}
				return m, cmd
			}
			if x >= 10 && x < 18 {
				var cmd tea.Cmd
				if m.mode == ModeConfirmDelete {
					cmd = m.confirmDelete(false)
				} else {
					cmd = m.confirmArchive(false)
				}
				return m, cmd
			}
		}
		return m, nil
	}
	if m.dragging || g.wide && press && v.X == g.split && v.Y >= g.list.y && v.Y < g.list.y+g.list.h {
		m.dragging = true
		x := max(max(26, (m.width+9)/10), min(min(m.width-39, m.width*9/10), v.X))
		m.ratio = float64(x) / float64(m.width)
		m.resize()
		if v.Action == tea.MouseActionRelease {
			m.dragging = false
			if m.settings != nil {
				if err := m.settings.SaveRatio(m.ratio); err != nil {
					m.status = "Layout changed, but settings were not saved: " + err.Error()
				} else {
					m.status = "Layout saved"
				}
			}
		}
		return m, nil
	}
	// Keep routing an in-progress text selection to its original pane.
	if m.selecting {
		v.X -= g.note.x + 1
		v.Y -= g.note.y + 1
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(v)
		if v.Action == tea.MouseActionRelease {
			m.selecting = false
		}
		return m, cmd
	}
	if g.list.contains(v.X, v.Y) {
		if v.Button == tea.MouseButtonWheelUp || v.Button == tea.MouseButtonWheelDown {
			delta := 3
			if v.Button == tea.MouseButtonWheelUp {
				delta = -3
			}
			m.listOffset = max(0, min(m.listOffset+delta, max(0, len(m.visible())-m.listHeight())))
			return m, nil
		}
		if !press {
			return m, nil
		}
		if v.Y == g.list.y+1 {
			cmd := m.request(action{kind: ActionFilter})
			return m, cmd
		}
		if v.Y == g.list.y+2 {
			cmd := m.request(action{kind: ActionSearch})
			return m, cmd
		}
		i := m.listOffset + v.Y - (g.list.y + 3)
		rows := m.visible()
		if v.Y >= g.list.y+3 && v.Y < g.list.y+g.list.h-1 && i >= 0 && i < len(rows) {
			if v.X == g.list.x+1 && !m.dirty() {
				m.recordDrag = rows[i].ID
				m.dragToken++
				m.orderEpoch++
				m.dragActive = false
				m.dragTarget = ""
				m.dragY = v.Y
				m.status = "Drag to move · Esc cancels"
			}
			cmd := m.request(action{kind: ActionSelect, id: rows[i].ID})
			if m.recordDrag != "" {
				cmd = tea.Batch(cmd, dragLater(m.dragToken))
			}
			return m, cmd
		}
		m.setMode(ModeList)
		return m, nil
	}
	if g.note.contains(v.X, v.Y) {
		if press && v.Y == g.note.y+g.note.h-2 {
			x := v.X - g.note.x - 1
			if x >= 0 && x < 6 {
				m.save()
			} else if x >= 7 && x < 16 && m.dirty() {
				cmd := m.request(action{kind: ActionDiscard})
				return m, cmd
			}
			return m, nil
		}
		if v.Y <= g.note.y || v.Y >= g.note.y+g.note.h-2 {
			return m, nil
		}
		m.syncNote(false)
		if !m.noteLoaded {
			return m, nil
		}
		var focus tea.Cmd
		if press {
			focus = m.setMode(ModeEdit)
			m.selecting = true
		}
		v.X -= g.note.x + 1
		v.Y -= g.note.y + 1
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(v)
		return m, tea.Batch(focus, cmd)
	}
	return m, nil
}

type dragTick struct{ token uint64 }

func dragLater(token uint64) tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return dragTick{token} })
}
func (m *model) scrollDrag() {
	g := m.geometry()
	if m.dragY <= g.list.y+3 {
		m.listOffset = max(0, m.listOffset-1)
	} else if m.dragY >= g.list.y+g.list.h-2 {
		m.listOffset = min(max(0, len(m.visible())-m.listHeight()), m.listOffset+1)
	}
}
func (m *model) setDragTarget() {
	rows := m.visible()
	if len(rows) == 0 {
		return
	}
	g := m.geometry()
	i := m.listOffset + max(0, min(m.listHeight()-1, m.dragY-g.list.y-3))
	i = min(i, len(rows)-1)
	source := 0
	for n, r := range rows {
		if r.ID == m.recordDrag {
			source = n
			break
		}
	}
	m.dragTarget = rows[i].ID
	m.dragBefore = i < source
}
