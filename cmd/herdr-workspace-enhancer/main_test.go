package main

import (
	"bytes"
	"testing"
)

func TestCommands(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"open", "--help"}, {"sync", "--help"}, {"tui", "--help"}, {"--open"}, {"--sync"}, {"invalid"}, {"sync", "extra"}} {
		c := newCommand()
		c.SetArgs(args)
		var out bytes.Buffer
		c.SetOut(&out)
		c.SetErr(&out)
		e := c.Execute()
		want := len(args) > 0 && (args[0] == "--open" || args[0] == "--sync" || args[0] == "invalid" || (args[0] == "sync" && args[1] == "extra"))
		if (e != nil) != want {
			t.Fatalf("%v: %v", args, e)
		}
	}
}
