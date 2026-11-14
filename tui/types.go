package tui

// Mode describes the active interaction state. Its zero value focuses the list.
type Mode string

const (
	ModeList           Mode = ""
	ModeEdit           Mode = "edit"
	ModeSearch         Mode = "search"
	ModeConfirmUnsaved Mode = "dirty"
	ModeConfirmArchive Mode = "archive"
)

type ActionKind string

const (
	ActionNone     ActionKind = ""
	ActionSelect   ActionKind = "select"
	ActionFilter   ActionKind = "filter"
	ActionSearch   ActionKind = "search"
	ActionQuit     ActionKind = "quit"
	ActionDiscard  ActionKind = "discard"
	ActionRefresh  ActionKind = "refresh"
	ActionArchive  ActionKind = "archive"
	ActionActivate ActionKind = "activate"
)

type Filter uint8

const (
	FilterAll Filter = iota
	FilterActive
	FilterArchived
	filterCount
)

func (f Filter) Next() Filter { return (f + 1) % filterCount }
func (f Filter) String() string {
	switch f {
	case FilterActive:
		return "Active"
	case FilterArchived:
		return "Archived"
	default:
		return "All"
	}
}

// Confirmation is independent of whether the user clicks a button or presses a key.
type Confirmation uint8

const (
	ConfirmationNone Confirmation = iota
	ConfirmationSave
	ConfirmationDiscard
	ConfirmationCancel
)
