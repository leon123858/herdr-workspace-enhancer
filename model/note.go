package model

// Note is saved with last-writer-wins semantics.
type Note struct {
	Text string `json:"text"`
}
