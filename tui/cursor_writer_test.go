package tui

import (
	"bytes"
	"strings"
	"testing"
)

func TestCursorWriterPreservesRendererPosition(t *testing.T) {
	var out bytes.Buffer
	w := NewCursorWriter(&out)
	frame := "frame\x1b]777;herdr-cursor;4;6;true\a\x1b[20;1H"
	if n, e := w.Write([]byte(frame)); e != nil || n != len(frame) {
		t.Fatal(n, e)
	}
	if strings.Contains(out.String(), "herdr-cursor") || !strings.HasSuffix(out.String(), "\x1b7\x1b[7;5H\x1b[?25h") {
		t.Fatal(out.String())
	}
	out.Reset()
	w.Write([]byte("next"))
	if !strings.HasPrefix(out.String(), "\x1b8next") {
		t.Fatal(out.String())
	}
	out.Reset()
	w.Write([]byte("\x1b]777;herdr-cursor;0;0;false\a"))
	if !strings.HasSuffix(out.String(), "\x1b[?25l") {
		t.Fatal(out.String())
	}
	out.Reset()
	w.Write([]byte("\x1b[?1049l\x1b[?25h"))
	if !strings.HasSuffix(out.String(), "\x1b[?25h") {
		t.Fatal(out.String())
	}
}
