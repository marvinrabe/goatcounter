package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/marvinrabe/goatcounter/internal/testenv"
)

// Make sure usage doesn't contain tabs, as that will mess up formatting in
// terminals.
func TestUsageTabs(t *testing.T) {
	for k, v := range usage {
		if strings.Contains(v, "\t") {
			t.Errorf("%q contains tabs", k)
		}
	}
}

func startTest(t *testing.T) (
	exit *testExit, in *bytes.Buffer, out *bytes.Buffer,
	ctx context.Context, dbc string,
) {
	t.Helper()

	ctx = testenv.DBFile(t)

	exit, in, out = captureCLI(t)
	return exit, in, out, ctx, os.Getenv("TESTENV_CONNECT")
}

func runCmdStop(t *testing.T, exit *testExit, ready chan<- struct{}, stop chan struct{}, cmd string, args ...string) {
	*exit = testExit(cmdMain(append([]string{cmd}, args...), ready, stop))
}

func runCmd(t *testing.T, exit *testExit, cmd string, args ...string) {
	ready := make(chan struct{}, 1)
	stop := make(chan struct{})
	runCmdStop(t, exit, ready, stop, cmd, args...)
	<-ready
}

func wantExit(t *testing.T, exit *testExit, out *bytes.Buffer, want int) {
	t.Helper()
	if int(*exit) != want {
		t.Errorf("wrong exit: %d; want: %d\n%s", *exit, want, out.String())
	}
}

type testExit int

func captureCLI(t *testing.T) (*testExit, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	oldOut, oldErr := stdout, stderr
	in, out := new(bytes.Buffer), new(bytes.Buffer)
	stdout, stderr = out, out
	t.Cleanup(func() { stdout, stderr = oldOut, oldErr })
	exit := testExit(-1)
	return &exit, in, out
}
