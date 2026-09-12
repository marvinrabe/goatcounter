package main

import (
	"strings"
	"testing"

	"zgo.at/zli"
)

func TestHelp(t *testing.T) {
	exit, _, out := zli.Test(t)

	{
		runCmd(t, exit, "help", "serve")
		wantExit(t, exit, out, 0)
		if !strings.Contains(out.String(), "libsql+file:/data/goatcounter.db") {
			t.Error()
		}
		out.Reset()
	}

	{
		runCmd(t, exit, "help", "all")
		wantExit(t, exit, out, 0)
		if !strings.Contains(out.String(), `Help for "healthcheck"`) || strings.Contains(out.String(), `Help for "db"`) {
			t.Error()
		}
		out.Reset()
	}
}

func TestRemovedDBCommands(t *testing.T) {
	for _, cmd := range []string{"db", "database", "create"} {
		t.Run(cmd, func(t *testing.T) {
			exit, _, out := zli.Test(t)
			runCmdStop(t, exit, make(chan struct{}, 1), make(chan struct{}), cmd)
			wantExit(t, exit, out, 1)
		})
	}
}
