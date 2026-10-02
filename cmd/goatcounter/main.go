package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/marvinrabe/goatcounter/internal/analytics"
	"github.com/marvinrabe/goatcounter/internal/database"
)

type command func(args []string, ready chan<- struct{}, stop chan struct{}) error

var stdout io.Writer = os.Stdout
var stderr io.Writer = os.Stderr
var mainDone sync.WaitGroup

func main() {
	if v, ok := os.LookupEnv("GOATCOUNTER_TMPDIR"); ok {
		os.Setenv("TMPDIR", v)
		os.Unsetenv("GOATCOUNTER_TMPDIR")
	}
	setupLog(false)
	os.Exit(cmdMain(os.Args[1:], make(chan struct{}, 1), make(chan struct{})))
}

func cmdMain(args []string, ready chan<- struct{}, stop chan struct{}) int {
	mainDone.Add(1)
	defer mainDone.Done()
	defer func() {
		select {
		case ready <- struct{}{}:
		default:
		}
	}()
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	var run command
	switch cmd {
	case "serve":
		run = cmdServe
	case "healthcheck":
		run = runHealthcheck
	default:
		fmt.Fprintf(stderr, "unknown command: %q\n%s", cmd, usage)
		return 1
	}
	for _, a := range args {
		if a == "-h" || a == "-help" || a == "--help" {
			fmt.Fprintln(stdout, strings.TrimSpace(usage))
			return 0
		}
	}
	if err := run(args, ready, stop); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

const usage = `
Usage: goatcounter [serve|healthcheck] [flags]

  serve        Start the HTTP server (the default).
  healthcheck  Check that a running instance is healthy; for Docker HEALTHCHECK.
` + usageServe + cmdHealthcheck

func connectDB(connect string) (database.DB, context.Context, error) {
	connectCtx, cancelConnect := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelConnect()
	db, err := database.Open(connectCtx, database.ConnectOptions{
		Connect: connect,
		Schema:  database.Schema,
		Create:  true,
	})

	if err != nil {
		return nil, nil, err
	}

	return db, analytics.NewContext(context.Background(), db), nil
}

func setupLog(debug bool) {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	o := &slog.HandlerOptions{Level: level, AddSource: true}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, o)))
}

// Bound final cleanup even if a driver is still closing a connection.
func closeDB(db database.DB) {
	done := make(chan struct{})
	go func() { db.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		slog.InfoContext(context.Background(), "Database close timed out")
	}
}
