package controller

import (
	"bytes"
	"strings"
	"testing"
)

func TestCommandsReportMissingContext(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", "")
	for _, name := range []string{"open", "sync", "tui"} {
		cmd := New().Command()
		cmd.SetArgs([]string{name})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "required environment is missing") {
			t.Fatal(name, err)
		}
	}

}

func TestCommands(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"open", "--help"}, {"sync", "--help"}, {"tui", "--help"}, {"--open"}, {"--sync"}, {"invalid"}, {"sync", "extra"}} {
		c := New().Command()
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
