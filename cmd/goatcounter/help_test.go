package main

import (
	"strings"
	"testing"
)

func TestHelp(t *testing.T) {
	exit, _, out := captureCLI(t)

	{
		runCmd(t, exit, "help", "serve")
		wantExit(t, exit, out, 0)
		if !strings.Contains(out.String(), "Required; no implicit local database.") {
			t.Error()
		}
		out.Reset()
	}

	{
		runCmd(t, exit, "help", "all")
		wantExit(t, exit, out, 0)
		if !strings.Contains(out.String(), `Help for "healthcheck"`) || strings.Contains(out.String(), `Help for "db"`) || strings.Contains(out.String(), `Help for "version"`) {
			t.Error()
		}
		out.Reset()
	}
}

func TestRemovedCommands(t *testing.T) {
	for _, cmd := range []string{"db", "database", "create", "version"} {
		t.Run(cmd, func(t *testing.T) {
			exit, _, out := captureCLI(t)
			runCmdStop(t, exit, make(chan struct{}, 1), make(chan struct{}), cmd)
			wantExit(t, exit, out, 1)
		})
	}
}
