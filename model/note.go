package model

type Note struct {
	Revision uint64 `json:"revision"`
	Text     string `json:"text"`
}
