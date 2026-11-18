package tui

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// CursorWriter keeps the renderer's cursor separate from the IME insertion point.
// The private OSC marker is stripped before output. Save/restore brackets each
// renderer write so its relative cursor movement still starts at its own position.
type CursorWriter struct {
	mu                  sync.Mutex
	out                 io.Writer
	positioned, visible bool
	x, y                int
}

var cursorMarker = regexp.MustCompile("\x1b\\]777;herdr-cursor;(-?[0-9]+);(-?[0-9]+);(true|false)\x07")

func NewCursorWriter(out io.Writer) *CursorWriter { return &CursorWriter{out: out} }
func (w *CursorWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := string(p)
	matches := cursorMarker.FindAllStringSubmatch(s, -1)
	for _, m := range matches {
		w.x, _ = strconv.Atoi(m[1])
		w.y, _ = strconv.Atoi(m[2])
		w.visible = m[3] == "true"
	}
	s = cursorMarker.ReplaceAllString(s, "")
	var b strings.Builder
	if w.positioned {
		b.WriteString("\x1b8")
	}
	b.WriteString(s)
	if strings.Contains(s, "\x1b[?1049l") {
		w.visible = false
		w.positioned = false
	} else if w.visible {
		b.WriteString("\x1b7")
		fmt.Fprintf(&b, "\x1b[%d;%dH\x1b[?25h", w.y+1, w.x+1)
		w.positioned = true
	} else {
		if len(matches) > 0 {
			b.WriteString("\x1b[?25l")
		}
		w.positioned = false
	}
	n, err := io.WriteString(w.out, b.String())
	if err != nil {
		return 0, err
	}
	if n != b.Len() {
		return 0, io.ErrShortWrite
	}
	return len(p), nil
}

// Preserve terminal identity so Bubble Tea can detect dimensions and resize events.
func (w *CursorWriter) Fd() uintptr {
	if f, ok := w.out.(interface{ Fd() uintptr }); ok {
		return f.Fd()
	}
	return ^uintptr(0)
}
func (w *CursorWriter) Read(p []byte) (int, error) {
	if r, ok := w.out.(io.Reader); ok {
		return r.Read(p)
	}
	return 0, io.EOF
}
func (w *CursorWriter) Close() error {
	if c, ok := w.out.(io.Closer); ok {
		return c.Close()
	}
	return nil
}
