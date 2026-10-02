package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/database"
	libsqldriver "github.com/marvinrabe/goatcounter/internal/dbdriver/libsql"
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

func connectDB(connect, dbConn string) (database.DB, context.Context, error) {
	var open, idle int
	if dbConn != "" {
		openS, idleS, ok := strings.Cut(dbConn, ",")
		if !ok {
			return nil, nil, errors.New("-dbconn flag: must be as max_open,max_idle")
		}
		var err error
		open, err = strconv.Atoi(openS)
		if err != nil {
			return nil, nil, fmt.Errorf("-dbconn flag: %w", err)
		}
		idle, err = strconv.Atoi(idleS)
		if err != nil {
			return nil, nil, fmt.Errorf("-dbconn flag: %w", err)
		}
	}

	fsys, err := fs.Sub(goatcounter.DB, "db")
	if err != nil {
		return nil, nil, err
	}

	connectCtx, cancelConnect := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelConnect()
	db, err := libsqldriver.Open(connectCtx, database.ConnectOptions{
		Connect:      connect,
		Files:        fsys,
		Create:       true,
		MaxOpenConns: open,
		MaxIdleConns: idle,
	})

	if err != nil {
		return nil, nil, err
	}

	return db, goatcounter.NewContext(context.Background(), db), nil
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
