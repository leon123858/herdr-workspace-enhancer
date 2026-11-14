package tui

import (
	keybind "github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

var keys = struct {
	Exit           keybind.Binding
	Quit           keybind.Binding
	Save           keybind.Binding
	Up             keybind.Binding
	Down           keybind.Binding
	FocusNote      keybind.Binding
	LeaveNote      keybind.Binding
	FinishSearch   keybind.Binding
	Filter         keybind.Binding
	Search         keybind.Binding
	Refresh        keybind.Binding
	Archive        keybind.Binding
	Activate       keybind.Binding
	ConfirmArchive keybind.Binding
	ConfirmSave    keybind.Binding
	ConfirmDiscard keybind.Binding
	Cancel         keybind.Binding
}{
	Exit:           keybind.NewBinding(keybind.WithKeys("ctrl+c")),
	Quit:           keybind.NewBinding(keybind.WithKeys("q")),
	Save:           keybind.NewBinding(keybind.WithKeys("ctrl+s")),
	Up:             keybind.NewBinding(keybind.WithKeys("up", "k")),
	Down:           keybind.NewBinding(keybind.WithKeys("down", "j")),
	FocusNote:      keybind.NewBinding(keybind.WithKeys("tab", "e")),
	LeaveNote:      keybind.NewBinding(keybind.WithKeys("esc", "tab")),
	FinishSearch:   keybind.NewBinding(keybind.WithKeys("enter", "esc")),
	Filter:         keybind.NewBinding(keybind.WithKeys("f")),
	Search:         keybind.NewBinding(keybind.WithKeys("/")),
	Refresh:        keybind.NewBinding(keybind.WithKeys("r")),
	Archive:        keybind.NewBinding(keybind.WithKeys("a")),
	Activate:       keybind.NewBinding(keybind.WithKeys("enter")),
	ConfirmArchive: keybind.NewBinding(keybind.WithKeys("y")),
	ConfirmSave:    keybind.NewBinding(keybind.WithKeys("s")),
	ConfirmDiscard: keybind.NewBinding(keybind.WithKeys("d")),
	Cancel:         keybind.NewBinding(keybind.WithKeys("esc", "c")),
}

func confirmationFor(msg tea.KeyMsg) Confirmation {
	switch {
	case keybind.Matches(msg, keys.ConfirmSave):
		return ConfirmationSave
	case keybind.Matches(msg, keys.ConfirmDiscard):
		return ConfirmationDiscard
	case keybind.Matches(msg, keys.Cancel):
		return ConfirmationCancel
	default:
		return ConfirmationNone
	}
}
