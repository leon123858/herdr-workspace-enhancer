// Package layout converts Herdr's rectangular pane layout into a split tree.
package layout

import (
	"fmt"
	"math"
	"os"
)

type Rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}
type Pane struct {
	TerminalID string `json:"terminal_id,omitempty"`
	ID         string `json:"pane_id"`
	Rect       Rect   `json:"rect"`
	CWD        string `json:"cwd"`
	Label      string `json:"label"`
}
type Split struct {
	Direction string  `json:"direction"`
	Ratio     float64 `json:"ratio"`
	Rect      Rect    `json:"rect"`
}

type NodeType string

const (
	NodePane  NodeType = "pane"
	NodeSplit NodeType = "split"
)

type Node struct {
	SourcePaneID     string   `json:"source_pane_id,omitempty"`
	SourceTerminalID string   `json:"source_terminal_id,omitempty"`
	Type             NodeType `json:"type"`
	CWD              string   `json:"cwd,omitempty"`
	Label            string   `json:"label,omitempty"`
	Direction        string   `json:"direction,omitempty"`
	Ratio            float64  `json:"ratio,omitempty"`
	First            *Node    `json:"first,omitempty"`
	Second           *Node    `json:"second,omitempty"`
}

// Tree accepts only non-overlapping slicing layouts. Invalid or incomplete
// snapshots fail before any workspace can be closed.
func Tree(panes []Pane) (*Node, error) {
	if len(panes) == 0 {
		return nil, fmt.Errorf("layout has no panes")
	}
	for i, p := range panes {
		if p.Rect.Width <= 0 || p.Rect.Height <= 0 {
			return nil, fmt.Errorf("pane %s has no visible area; unzoom and resize before archiving", p.ID)
		}
		for _, q := range panes[:i] {
			if p.Rect.X < q.Rect.X+q.Rect.Width && q.Rect.X < p.Rect.X+p.Rect.Width && p.Rect.Y < q.Rect.Y+q.Rect.Height && q.Rect.Y < p.Rect.Y+p.Rect.Height {
				return nil, fmt.Errorf("overlapping panes")
			}
		}
	}
	return build(panes, nil)
}
func ExactTree(panes []Pane, splits []Split) (*Node, error) {
	if _, err := Tree(panes); err != nil {
		return nil, err
	}
	if len(splits) != len(panes)-1 {
		return nil, fmt.Errorf("incomplete split metadata")
	}
	return build(panes, splits)
}
func build(ps []Pane, splits []Split) (*Node, error) {
	if len(ps) == 1 {
		return &Node{Type: NodePane, CWD: ps[0].CWD, Label: ps[0].Label, SourcePaneID: ps[0].ID, SourceTerminalID: ps[0].TerminalID}, nil
	}
	minX, minY, maxX, maxY := ps[0].Rect.X, ps[0].Rect.Y, 0, 0
	for _, p := range ps {
		r := p.Rect
		minX = min(minX, r.X)
		minY = min(minY, r.Y)
		maxX = max(maxX, r.X+r.Width)
		maxY = max(maxY, r.Y+r.Height)
	}
	for _, vertical := range []bool{true, false} {
		for _, candidate := range ps {
			cut := candidate.Rect.X + candidate.Rect.Width
			if !vertical {
				cut = candidate.Rect.Y + candidate.Rect.Height
			}
			var a, b []Pane
			valid := true
			for _, p := range ps {
				start, end := p.Rect.X, p.Rect.X+p.Rect.Width
				if !vertical {
					start, end = p.Rect.Y, p.Rect.Y+p.Rect.Height
				}
				if end <= cut {
					a = append(a, p)
				} else if start >= cut {
					b = append(b, p)
				} else {
					valid = false
					break
				}
			}
			if !valid || len(a) == 0 || len(b) == 0 {
				continue
			}
			var exact *Split
			if splits != nil {
				direction := "right"
				origin, size := minX, maxX-minX
				if !vertical {
					direction = "down"
					origin, size = minY, maxY-minY
				}
				for i := range splits {
					s := &splits[i]
					if s.Rect == (Rect{minX, minY, maxX - minX, maxY - minY}) && s.Direction == direction && math.Abs(float64(cut-origin)-s.Ratio*float64(size)) <= 1 {
						exact = s
						break
					}
				}
				if exact == nil {
					continue
				}
			}
			first, e := build(a, splits)
			if e != nil {
				continue
			}
			second, e := build(b, splits)
			if e != nil {
				continue
			}
			direction, ratio := "right", float64(cut-minX)/float64(maxX-minX)
			if !vertical {
				direction = "down"
				ratio = float64(cut-minY) / float64(maxY-minY)
			}
			if exact != nil {
				ratio = exact.Ratio
			}
			return &Node{Type: NodeSplit, Direction: direction, Ratio: ratio, First: first, Second: second}, nil
		}
	}
	return nil, fmt.Errorf("layout cannot be represented as a split tree")
}
func (n *Node) Validate(checkDirectories bool) error {
	if n == nil {
		return fmt.Errorf("missing layout node")
	}
	switch n.Type {
	case NodePane:
		if n.CWD == "" {
			return fmt.Errorf("pane directory is unknown")
		}
		if checkDirectories {
			info, e := os.Stat(n.CWD)
			if e != nil {
				return e
			}
			if !info.IsDir() {
				return fmt.Errorf("not a directory: %s", n.CWD)
			}
		}
	case NodeSplit:
		if (n.Direction != "right" && n.Direction != "down") || math.IsNaN(n.Ratio) || n.Ratio <= 0 || n.Ratio >= 1 {
			return fmt.Errorf("invalid split")
		}
		if e := n.First.Validate(checkDirectories); e != nil {
			return e
		}
		return n.Second.Validate(checkDirectories)
	default:
		return fmt.Errorf("unknown node type %q", n.Type)
	}
	return nil
}
