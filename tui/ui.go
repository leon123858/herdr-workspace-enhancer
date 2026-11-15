package tui

import (
	"context"

	state "herdr-workspace-enhancer/model"
	"herdr-workspace-enhancer/service"
	textarea "herdr-workspace-enhancer/tui/noteeditor"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"
)

// UI is the lifecycle contract handed to Bubble Tea. Application code creates
// it with NewUI; the framework calls these methods after Program.Run starts.
type UI interface {
	// Init is called once by Bubble Tea to schedule initial work.
	Init() tea.Cmd
	// Update is called by Bubble Tea for each input or command-result message.
	Update(tea.Msg) (tea.Model, tea.Cmd)
	// View is called by Bubble Tea to render the current state as terminal text.
	View() string
}

var _ UI = model{}
var _ tea.Model = (UI)(nil)

// NewUI is the application-side initializer, not a framework lifecycle method.
// It constructs the model and loads layout settings before handing it to Bubble
// Tea. It does not call Init; Program.Run owns that lifecycle call.
func NewUI(s *service.Service, focus func(context.Context, string) error, settings *service.SettingsService) UI {
	lipgloss.SetHasDarkBackground(true)
	editor := textarea.New()
	editor.CharLimit = 0
	editor.MaxHeight = 0
	editor.MaxWidth = 0
	search := textinput.New()
	search.Placeholder = "Search workspaces"
	search.CharLimit = 200
	m := model{service: s, focus: focus, editor: editor, search: search, ratio: state.DefaultWorkspaceRatio, width: 80, height: 24}
	if s != nil && s.MutationError != nil {
		m.status = s.MutationError.Error()
	}
	m.resize()
	m.settings = settings
	m.setRatio()
	return m
}
