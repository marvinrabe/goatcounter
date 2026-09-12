package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	_ "time/tzdata"

	"github.com/marvinrabe/goatcounter"
	libsqldriver "github.com/marvinrabe/goatcounter/internal/dbdriver/libsql"
	"github.com/marvinrabe/goatcounter/internal/log"
	"zgo.at/errors"
	"zgo.at/json"
	"zgo.at/zdb"
	"zgo.at/zli"
	"zgo.at/zstd/zfs"
	"zgo.at/zstd/zruntime"
	"zgo.at/zstd/zslice"
)

func init() {
	errors.Package = "github.com/marvinrabe/goatcounter"
}

type command func(f zli.Flags, ready chan<- struct{}, stop chan struct{}) error

func main() {
	// Linux doesn't allow some environment variables to be set if any
	// capability bits (such as cap_net_bind_service) are set, so also read from
	// GOATCOUNTER_TMPDIR
	if v, ok := os.LookupEnv("GOATCOUNTER_TMPDIR"); ok {
		os.Setenv("TMPDIR", v)
		os.Unsetenv("GOATCOUNTER_TMPDIR")
	}

	var (
		f     = zli.NewFlags(os.Args)
		ready = make(chan struct{}, 1)
		stop  = make(chan struct{}, 1)
	)
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	cmdMain(f, ready, stop)
}

var mainDone sync.WaitGroup

func cmdMain(f zli.Flags, ready chan<- struct{}, stop chan struct{}) {
	mainDone.Add(1)
	defer mainDone.Done()

	cmd, err := f.ShiftCommand("help", "version", "serve", "healthcheck")
	if zslice.ContainsAny(f.Args, "-h", "-help", "--help") {
		f.Args = append([]string{cmd}, f.Args...)
		cmd = "help"
	}
	if err != nil && !errors.Is(err, zli.ErrCommandNoneGiven{}) {
		zli.Errorf(usage[""])
		zli.Errorf("%s", err)
		zli.Exit(1)
		return
	}

	var run command
	switch cmd {
	default:
		zli.Errorf(usage[""])
		zli.Errorf("unknown command: %q", cmd)
		zli.Exit(1)
	case "", "help":
		run = cmdHelp
	case "version":
		var (
			jsonFlag = f.Bool(false, "json")
		)
		if err := f.Parse(); err != nil {
			zli.F(err)
		}
		if jsonFlag.Bool() {
			j, err := json.MarshalIndent(map[string]any{
				"version": goatcounter.Version,
				"go":      runtime.Version(),
				"GOOS":    runtime.GOOS,
				"GOARCH":  runtime.GOARCH,
				"race":    zruntime.Race,
				"cgo":     zruntime.CGO,
			}, "", "  ")
			if err != nil {
				panic(err)
			}
			fmt.Println(string(j))
		} else {
			fmt.Printf("version=%s; go=%s; GOOS=%s; GOARCH=%s; race=%t; cgo=%t\n",
				goatcounter.Version, runtime.Version(), runtime.GOOS, runtime.GOARCH,
				zruntime.Race, zruntime.CGO)
		}
		zli.Exit(0)
		return

	case "healthcheck":
		run = runHealthcheck
	case "serve":
		run = cmdServe
	}

	err = run(f, ready, stop)
	if err != nil {
		if !log.HasDebug("cli-trace") {
			for {
				var s *errors.StackErr
				if !errors.As(err, &s) {
					break
				}
				err = s.Unwrap()
			}
		}

		c := 1
		var stErr interface {
			Code() int
			Error() string
		}
		if errors.As(err, &stErr) {
			c = stErr.Code()
			if c > 255 { // HTTP error.
				c = 1
			}
		}

		if c == 0 {
			if err.Error() != "" {
				fmt.Fprintln(zli.Stdout, err.Error())
			}
			zli.Exit(0)
		}
		zli.Errorf(err)
		zli.Exit(c)
		return
	}
	zli.Exit(0)
}

func connectDB(connect, dbConn string, dev bool) (zdb.DB, context.Context, error) {
	connect = normalizeDBConnect(connect)

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

	fsys, err := zfs.EmbedOrDir(goatcounter.DB, "db", dev)
	if err != nil {
		return nil, nil, err
	}

	db, err := libsqldriver.Open(context.Background(), zdb.ConnectOptions{
		Connect:      connect,
		Files:        fsys,
		Create:       true,
		MaxOpenConns: open,
		MaxIdleConns: idle,
	})

	if err != nil {
		return nil, nil, err
	}

	if log.HasDebug("sql-query") {
		db = zdb.NewLogDB(db, os.Stderr, zdb.DumpQuery|zdb.DumpLocation, "")
	} else if log.HasDebug("sql-result") {
		db = zdb.NewLogDB(db, os.Stderr, zdb.DumpQuery|zdb.DumpLocation|zdb.DumpResult, "")
	}
	return db, goatcounter.NewContext(context.Background(), db), nil
}

func normalizeDBConnect(connect string) string {
	// Keep standard libSQL URLs usable at the command line while adding the
	// driver separator zdb expects.
	if strings.HasPrefix(connect, "libsql://") || strings.HasPrefix(connect, "http://") || strings.HasPrefix(connect, "https://") {
		return "libsql+" + connect
	}
	return connect
}

func setupLog(dev, asJSON bool, debug []string) {
	o := &slog.HandlerOptions{
		// Our log package takes care of suppressing debug logs.
		Level:     slog.LevelDebug,
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == "module" || a.Key == "_err" {
				return slog.Attr{}
			}
			return a
		},
	}
	var handler slog.Handler
	if asJSON {
		handler = slog.NewJSONHandler(os.Stdout, o)
	} else {
		if !dev {
			// Shorter, syslog-ish timestamps rather than full RFC 3339.
			replace := o.ReplaceAttr
			o.ReplaceAttr = func(groups []string, a slog.Attr) slog.Attr {
				if len(groups) == 0 && a.Key == slog.TimeKey {
					a.Value = slog.StringValue(a.Value.Time().Format("Jan _2 15:04:05"))
				}
				return replace(groups, a)
			}
		}
		handler = slog.NewTextHandler(os.Stdout, o)
	}

	log.SetDebug(debug)
	slog.SetDefault(slog.New(handler))
}
