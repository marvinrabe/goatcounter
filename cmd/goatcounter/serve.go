package main

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/handlers"
	"github.com/marvinrabe/goatcounter/internal/bgrun"
	"github.com/marvinrabe/goatcounter/internal/cron"
	"github.com/marvinrabe/goatcounter/internal/geo"
	"github.com/marvinrabe/goatcounter/internal/geo/geoip2"
	"github.com/marvinrabe/goatcounter/internal/log"
	"zgo.at/errors"
	"zgo.at/zhttp"
	"zgo.at/zli"
	"zgo.at/zstd/zfs"
	"zgo.at/zstd/zio"
	"zgo.at/zstd/znet"
	"zgo.at/zstd/zruntime"
	"zgo.at/ztpl"
	"zgo.at/zvalidate"
)

const usageServe = `
Start a HTTP server for this GoatCounter installation.

GoatCounter tracks a single site and serves it on whichever domain you point at
it; the site is created automatically on first run. Create a user to log in
with using "goatcounter db create user -email you@example.com".

Static files and templates are compiled in the binary and aren't needed to run
GoatCounter. But they're loaded from the filesystem if GoatCounter is started
with -dev.

Environment:

  All of the flags take the defaults from $GOATCOUNTER_«FLAG», where «FLAG» is
  the flag name. The commandline flag will override the environment variable.

  For example:

    GOATCOUNTER_LISTEN=:80
    GOATCOUNTER_STORE_EVERY=60
    GOATCOUNTER_AUTOMIGRATE=

  Additional environment variables:


    GOATCOUNTER_TMPDIR  Alternative way to set TMPDIR; takes precedence over
                        TMPDIR. Mainly intended for cases where TMPDIR can't be
                        used (e.g. when the capability bit is set on Linux).

Flags:

  -db          Database connection: "sqlite+<file>".
               See "goatcounter help db" for detailed documentation. Default:
               sqlite+./goatcounter-data/db.sqlite3

  -dbconn      Set maximum number of connections, as max_open,max_idle

               There is no maximum if max_open is -1, and idle connections are
               not retained if max_idle is -1 The default is 4,2.

               SQLite keeps a page cache per connection, so raising this also
               raises memory use; with WAL there is only ever one writer.

  -listen      Address to listen on. Default: "*:8080". See "goatcounter help
               listen" for detailed documentation.

  -public-port Port your site is publicly accessible on. Only needed if it's
               not 80 or 443.

  -base-path   Path under which GoatCounter is available. Usually GoatCounter
               runs on its own domain or subdomain ("stats.example.com"), but
               in some cases it's useful to run GoatCounter under a path
               ("example.com/stats"), in which case you'll need to set this to
               "/stats".

  -automigrate Automatically run all pending migrations on startup.


  -static      Serve static files from a different domain, such as a CDN or
               cookieless domain. Default: not set.

  -geodb       Path to mmdb GeoIP database; can be either the City or Country
               version, but regional information is only recorded with the City
               version.

               GoatCounter will automatically use the first .mmdb file in
               ./goatcounter-data, if any exists. GoatCounter comes with a
               Countries version built-in, and will use that if this flag isn't
               given and there is no file in ./goatcounter-data. You only need
               this if you want to use a newer/different version, or if you
               want to record regions.

               This can also be a MaxMind account ID and license key, in which
               case GoatCounter will automatically download a Cities database
               from MaxMind and update it every week. The format for this is:

                   maxmind:account_id:license[:path]

               :path may be omitted and defaults to goatcounter-data/auto.mmdb.

               For example:

                   -geodb maxmind:123456:abcdef
                   -geodb maxmind:123456:abcdef:/home/goatcounter/cities.mmdb

               Updates are only done on restarts.

  -ratelimit   Set rate limits for various actions; the syntax is
               "name:num-requests/seconds"; multiple values are separated by
               a comma. The defaults are:

                   count:4/1            4 requests / second

               If one of the names is omitted it will fall back to the default
               value; for example "-ratelimit count:8/1" will use the default
               for everything else. Use "none" to disable this ratelimit.

  -store-every How often to persist pageviews to the database, in seconds.
               Higher values will give better performance, but it will take a
               bit longer for pageviews to show. The default is 10 seconds.

  -dev         Start in "dev mode".

  -json        Output logs as JSON instead of aligned text.

  -debug       Modules to debug, comma-separated or 'all' for all modules.
               See "goatcounter help debug" for a list of modules.
`

func cmdServe(f zli.Flags, ready chan<- struct{}, stop chan struct{}) error {
	var (
		port         = f.Int(0, "public-port", "port") // -port is a deprecated alias, for compat with <2.0
		basePath     = f.String("", "base-path")
		domainStatic = f.String("", "static")
		dbConnect    = f.String(defaultDB(), "db")
		dbConn       = f.String("4,2", "dbconn")
		debugFlag    = f.StringList(nil, "debug")
		dev          = f.Bool(false, "dev")
		automigrate  = f.Bool(false, "automigrate")
		listen       = f.String(":8080", "listen")
		geodbFlag    = f.String("", "geodb")
		ratelimit    = f.String("", "ratelimit")
		storeEvery   = f.Int(10, "store-every")
		json         = f.Bool(false, "json")
	)
	if err := f.Parse(zli.FromEnv("GOATCOUNTER")); err != nil {
		return err
	}

	v := zvalidate.New()

	setupLog(dev.Bool(), json.Bool(), debugFlag.StringsSplit(","))

	if dev.Bool() {
		zhttp.DefaultDecoder = zhttp.NewDecoder(true, false) // Log unknown fields
		if err := setupReload(); err != nil {
			return err
		}
	}

	geodb := setupGeo(&v, geodbFlag.String())
	ratelimits := setupRatelimits(&v, ratelimit.String())
	domainCount, urlStatic := setupDomains(&v, dev.Bool(), domainStatic.Pointer(), basePath.Pointer())

	v.Range("-store-every", int64(storeEvery.Int()), 1, 0)
	cron.SetPersistInterval(time.Duration(storeEvery.Int()) * time.Second)

	if v.HasErrors() {
		return v
	}

	db, ctx, err := connectDB(dbConnect.String(), dbConn.String(),
		map[bool][]string{true: {"all"}, false: {"pending"}}[automigrate.Bool()],
		true, dev.Bool())
	if err != nil {
		return err
	}
	defer db.Close()

	ctx = geo.With(ctx, geodb)

	if err := setupTpl(ctx, dev.Bool()); err != nil {
		return err
	}

	zhttp.ErrPage = handlers.ErrPage

	if err := goatcounter.Memstore.Init(db); err != nil {
		return err
	}

	cron.Start(context.WithoutCancel(ctx))

	c := goatcounter.Config(ctx)
	c.Timezone, err = goatcounter.LoadTimezone()
	if err != nil {
		return err
	}
	c.Domain = ""
	c.DomainStatic = domainStatic.String()
	c.DomainCount = domainCount
	c.URLStatic = urlStatic
	c.Dev = dev.Bool()
	c.BasePath = basePath.String()

	if port.Int() > 0 {
		c.Port = fmt.Sprintf(":%d", port.Int())
	}

	timeout := 60

	// Set up HTTP handler and servers.
	hosts := map[string]http.Handler{
		"*": handlers.NewBackend(db, dev.Bool(), c.DomainStatic, c.BasePath, timeout, ratelimits),
	}
	if domainStatic.String() != "" {
		// May not be needed, but just in case the DomainStatic isn't an external CDN.
		hosts[znet.RemovePort(domainStatic.String())] = handlers.NewStatic(chi.NewRouter(), dev.Bool(), c.BasePath)
	}

	ch, err := zhttp.Serve(0, stop, &http.Server{
		Addr:        listen.String(),
		Handler:     zhttp.HostRoute(hosts),
		BaseContext: func(net.Listener) context.Context { return ctx },
		Protocols: func() *http.Protocols {
			p := &http.Protocols{}
			p.SetHTTP1(true)
			p.SetHTTP2(true)
			p.SetUnencryptedHTTP2(true)
			return p
		}(),
	})
	if err != nil {
		return err
	}

	<-ch // Server is set up

	log.Module("startup").Info(ctx, "GoatCounter ready",
		startupAttr(geodb, listen.String(), dev.Bool(), "timezone", c.Timezone.String())...)

	var users goatcounter.Users
	if err := users.List(context.WithoutCancel(ctx)); err != nil {
		return err
	}
	if len(users) == 0 {
		dbFlag := ""
		if dbConnect.String() != defaultDB() {
			dbFlag = `-db="` + strings.ReplaceAll(dbConnect.String(), `"`, `\"`) + `" `
		}
		// Adjust command for Docker or Podman
		cmd := "goatcounter"
		if _, err := os.Stat("/.dockerenv"); err == nil && os.Getenv("HOSTNAME") != "" {
			cmd = "docker exec -it " + os.Getenv("HOSTNAME") + " goatcounter"
		}
		if _, err := os.Stat("/run/.containerenv"); err == nil && os.Getenv("HOSTNAME") != "" {
			cmd = "podman exec -it " + os.Getenv("HOSTNAME") + " goatcounter"
		}
		log.Warnf(ctx, "No users yet; create one with:\n"+
			"    %s db %screate user -email=..", cmd, dbFlag)
	}

	ready <- struct{}{}

	<-ch // Shutdown

	sig := make(chan os.Signal, 1)
	go func() {
		signal.Notify(sig, syscall.SIGHUP, syscall.SIGTERM, os.Interrupt /*SIGINT*/)
		<-sig
		zli.Colorln("One more to kill…", zli.Bold)
		<-sig
		zli.Colorln("Force killing", zli.Bold)
		os.Exit(99)
	}()

	bgrun.RunFunction("shutdown", func() {
		err := cron.TaskPersistAndStat()
		if err != nil {
			log.Error(ctx, err)
		}
		goatcounter.Memstore.StoreSessions(db)
	})

	time.Sleep(200 * time.Millisecond) // Only show message if it doesn't exit in 200ms.

	first := true
	for r := bgrun.Running(); len(r) > 0; r = bgrun.Running() {
		if first {
			log.Info(ctx, "Waiting for background tasks; send HUP, TERM, or INT twice to force kill")
			first = false
		}
		time.Sleep(100 * time.Millisecond)

		zli.Erase()
		fmt.Fprintf(zli.Stdout, "\r%d tasks: ", len(r))
		for i, t := range r {
			if i > 0 {
				fmt.Fprint(zli.Stdout, ", ")
			}
			fmt.Fprintf(zli.Stdout, "%s (%s)", t.Task, time.Since(t.Started).Round(time.Second))
		}
	}
	fmt.Fprintln(zli.Stdout)
	return nil
}

// Keep the per-connection page cache small; the default of 20M is multiplied by
// the number of open connections, which is a lot of memory for a database this
// size.
const defaultDBParams = "?_cache_size=-4000"

func defaultDB() string {
	return "sqlite+./goatcounter-data/db.sqlite3" + defaultDBParams
}

func setupReload() error {
	if !zio.Exists("db/migrate") || !zio.Exists("tpl") || !zio.Exists("public") {
		return errors.New("-dev flag was given but this doesn't seem like a GoatCounter source directory")
	}
	if _, err := exec.LookPath("git"); err == nil {
		rev := ""
		b, ok := debug.ReadBuildInfo()
		if ok {
			for _, s := range b.Settings {
				if s.Key == "vcs.revision" {
					rev = s.Value
				}
			}
		}
		if rev != "" {
			have, err := exec.Command("git", "log", "-n1", "--pretty=format:%H").CombinedOutput()
			if err == nil {
				if h := strings.TrimSpace(string(have)); rev != h {
					log.Errorf(context.Background(),
						"goatcounter was built from revision %s but source directory has revision %s",
						rev[:7], h[:7])
				}
			}
		}
	}

	if _, err := os.Stat("./tpl"); os.IsNotExist(err) {
		return nil
	}

	log.Module("startup").Info(context.Background(), "watching ./tpl for changes")
	go watchTemplates("./tpl")
	return nil
}

// watchTemplates polls dir and re-reads the templates whenever a file in it
// changed. Polling once a second is plenty for a -dev flag, and saves pulling
// in a filesystem notification library.
func watchTemplates(dir string) {
	last := templatesModified(dir)
	for range time.Tick(time.Second) {
		if m := templatesModified(dir); !m.Equal(last) {
			last = m
			if err := ztpl.Reload(dir); err != nil {
				log.Error(context.Background(), err)
			} else {
				log.Module("startup").Info(context.Background(), "reloaded templates")
			}
		}
	}
}

// templatesModified reports the most recent mtime of any file in dir; a
// removed file changes the total too, as the walk then skips it.
func templatesModified(dir string) time.Time {
	var newest time.Time
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // just skip what we can't read
		}
		fi, err := d.Info()
		if err != nil {
			return nil //nolint:nilerr
		}
		if fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
		return nil
	})
	return newest
}

func setupGeo(v *zvalidate.Validator, geodbFlag string) *geoip2.Reader {
	if geodbFlag == "" {
		ls, _ := os.ReadDir("goatcounter-data")
		for _, f := range ls {
			if strings.HasSuffix(f.Name(), ".mmdb") {
				geodbFlag = "goatcounter-data/" + f.Name()
				break
			}
		}
	}
	geodb, err := geo.Open(geodbFlag)
	if err != nil {
		v.Append("-geodb", fmt.Sprintf("loading GeoIP database: %s", err))
	}
	return geodb
}

func setupRatelimits(v *zvalidate.Validator, ratelimit string) handlers.Ratelimits {
	h := handlers.NewRatelimits()
	if ratelimit != "" {
		for r := range strings.SplitSeq(ratelimit, ",") {
			v2 := zvalidate.New()

			name, spec, _ := strings.Cut(r, ":")
			v2.Required("-ratelimit.name", name)
			nn := v2.Include("-ratelimit.name", name, []string{"count", "api", "api2", "api-count", "export", "login"})
			name = nn.(string)

			if strings.TrimSpace(spec) == "none" {
				h.Clear(name)
				continue
			} else {
				reqs, secs, _ := strings.Cut(spec, "/")

				v2.Required("-ratelimit.requests", reqs)
				v2.Required("-ratelimit.seconds", secs)
				r := v2.Integer("-ratelimit.requests", reqs)
				s := v2.Integer("-ratelimit.seconds", secs)
				h.Set(name, int(r), s)
			}
			if v2.HasErrors() {
				v.Merge(v2)
			}
		}
	}
	return h
}

func setupDomains(v *zvalidate.Validator, dev bool, domainStatic, basePath *string) (string, string) {
	*basePath = strings.Trim(*basePath, "/")
	if *basePath != "" {
		*basePath = "/" + *basePath
	}
	zhttp.BasePath = *basePath

	var domainCount, urlStatic string
	if *domainStatic != "" {
		if p := strings.Index(*domainStatic, ":"); p > -1 {
			v.Domain("-static", (*domainStatic)[:p])
		} else {
			v.Domain("-static", *domainStatic)
		}
		urlStatic = "//" + *domainStatic
		domainCount = *domainStatic
	} else {
		urlStatic = *basePath
	}
	return domainCount, urlStatic
}

func setupTpl(ctx context.Context, dev bool) error {
	fsys, err := zfs.EmbedOrDir(goatcounter.Templates, "tpl", dev)
	if err != nil {
		return err
	}
	err = ztpl.Init(fsys)
	if err != nil {
		if !dev {
			return err
		}
		log.Error(ctx, err)
	}
	return nil
}

func startupAttr(geodb *geoip2.Reader, listen string, dev bool, attr ...any) []any {
	md := geodb.DB().Metadata
	return append(attr,
		"listen", listen,
		"dev", dev,
		slog.Group("version",
			"version", goatcounter.Version,
			"go", runtime.Version(),
			"GOOS", runtime.GOOS,
			"GOARCH", runtime.GOARCH,
			"CGO", zruntime.CGO,
			"race", zruntime.Race,
		),
		slog.Group("geoip",
			"path", geodb.DB().Path,
			"build", time.Unix(int64(md.BuildEpoch), 0).UTC().Format("2006-01-02 15:04:05"),
			"type", md.DatabaseType,
			"description", md.Description["en"],
			"nodes", md.NodeCount,
		),
	)
}
