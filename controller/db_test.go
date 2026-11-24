package controller

import (
	"bytes"
	"herdr-workspace-enhancer/repository/state"
	"strings"
	"testing"
)

func TestDBCommands(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", root)
	t.Setenv("HERDR_SOCKET_PATH", "test")
	s, err := state.Open(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	for _, tc := range []struct {
		args []string
		want string
		fail bool
	}{
		{[]string{"db", "version"}, "2", false},
		{[]string{"db", "migrate"}, "", true},
		{[]string{"db", "migrate", "--to", "3"}, "", true},
		{[]string{"db", "migrate", "--to", "0"}, "Backup:", false},
		{[]string{"db", "version"}, "0", false},
		{[]string{"db", "migrate", "--to", "2"}, "Database version: 2", false},
	} {
		c := New()
		cmd := c.Command()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs(tc.args)
		err := cmd.Execute()
		c.Shutdown()
		if (err != nil) != tc.fail || !strings.Contains(out.String(), tc.want) {
			t.Fatal(tc, err, out.String())
		}
	}
}
