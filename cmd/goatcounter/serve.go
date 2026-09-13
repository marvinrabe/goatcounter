package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/handlers"
	"github.com/marvinrabe/goatcounter/internal/cron"
	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/datetime"
	"github.com/marvinrabe/goatcounter/internal/geo"
	"github.com/marvinrabe/goatcounter/internal/geo/geoip2"
	"github.com/marvinrabe/goatcounter/internal/httpx"
	"github.com/marvinrabe/goatcounter/internal/validation"
)

const usageServe = `
Start a HTTP server for this GoatCounter installation.

GoatCounter tracks the sites listed in GOATCOUNTER_SITES. Dashboard access is
configured with GOATCOUNTER_AUTH.

Static files and templates are compiled in the binary and aren't needed to run
GoatCounter. But they're loaded from the filesystem if GoatCounter is started
with -dev.

Environment:

  All of the flags take the defaults from $GOATCOUNTER_«FLAG», where «FLAG» is
  the flag name. The commandline flag will override the environment variable.

  For example:

    GOATCOUNTER_LISTEN=:80
    GOATCOUNTER_STORE_EVERY=60
    GOATCOUNTER_SITES=example.com,foobar.net
    GOATCOUNTER_API_TOKEN=a-long-random-secret
    GOATCOUNTER_AUTH=basic
    GOATCOUNTER_BASIC_AUTH=admin:a-long-random-password

  Additional environment variables:


    GOATCOUNTER_TMPDIR  Alternative way to set TMPDIR; takes precedence over
                        TMPDIR. Mainly intended for cases where TMPDIR can't be
                        used (e.g. when the capability bit is set on Linux).

Flags:

  -db          Local database path or remote libSQL URL.
               Required; no implicit local database.
               Remote example: libsql://your-database.turso.io?authToken=TOKEN
               An empty database is initialized automatically on startup.

  -dbconn      Set maximum number of connections, as max_open,max_idle

               There is no maximum if max_open is -1, and idle connections are
               not retained if max_idle is -1 The default is 4,2.

               Local SQLite files use one connection to avoid
               write contention. Remote databases honor max_open and disable
               idle reuse because remote streams expire.

  -listen      Address to listen on. Default: ":8080". See "goatcounter help
               listen" for detailed documentation.

  -base-path   Path under which GoatCounter is available. Usually GoatCounter
               runs on its own domain or subdomain ("stats.example.com"), but
               in some cases it's useful to run GoatCounter under a path
               ("example.com/stats"), in which case you'll need to set this to
               "/stats".

  -static      Serve static files from a different domain, such as a CDN or
               cookieless domain. Default: not set.

  -geodb       Explicit path to a City or Country mmdb GeoIP database.
               Defaults to the bundled Country database. No files are
               discovered and no downloads are performed during startup.
               Use geodb-update separately to download a newer Cities file.

  -shutdown-timeout
               Total HTTP/worker shutdown deadline in seconds. Default: 25.

  -drain-delay Seconds to keep serving after becoming unready, giving a load
               balancer time to remove this replica. Default: 0; Kubernetes: 5.

  -ratelimit   Limit requests to /count. Syntax: count:num-requests/seconds.
               Default: count:4/1 (4 requests per second).
               Use count:none to disable the collector limit.
               Only the count limit is supported.

  -store-every How often to process durable queued pageviews, in seconds.
               Pageviews are saved before acknowledgement; processing makes them
               visible in the dashboard. The default is 10 seconds.

  -sites       Comma-separated site names accepted by the collector and shown
               in the dashboard selector. The site name itself is stored
               with every data row. Default: example.com.

  -api-token   Bearer token for the JSON API and MCP endpoint at /api. The API
               is disabled when this is empty (the default).

  -auth        Dashboard authentication: public, basic, or oidc. Default:
               public.

  -basic-auth  Comma-separated username:password entries for -auth=basic.

  -oidc-issuer, -oidc-client-id, -oidc-client-secret, -oidc-redirect-url,
  -oidc-session-secret, -oidc-scopes
               OIDC provider and client settings for -auth=oidc. The redirect
               URL must end in /auth/callback. The session secret must contain
               at least 32 bytes.

  -dev         Load assets from disk and use readable text logs.
               Normal operation writes structured JSON logs to stdout.

  -debug       Enable debug logs, including HTTP requests.
  -debug-sql   Log SQL queries.
`

func cmdServe(args []string, ready chan<- struct{}, stop chan struct{}) error {
	f := newFlags("cmdServe")
	var (
		basePath     = f.String("base-path", "", "")
		domainStatic = f.String("static", "", "")
		dbConnect    = f.String("db", defaultDB(), "")
		dbConn       = f.String("dbconn", "4,2", "")
		debugFlag    = f.Bool("debug", false, "")
		debugSQL     = f.Bool("debug-sql", false, "")
		dev          = f.Bool("dev", false, "")
		listen       = f.String("listen", ":8080", "")
		geodbFlag    = f.String("geodb", "", "")
		ratelimit    = f.String("ratelimit", "", "")
		storeEvery   = f.Int("store-every", 10, "")
		sitesFlag    = f.String("sites", "example.com", "")
		apiToken     = f.String("api-token", "", "")
		authMode     = f.String("auth", "public", "")
		basicAuth    = f.String("basic-auth", "", "")
		oidcIssuer   = f.String("oidc-issuer", "", "")
		oidcClientID = f.String("oidc-client-id", "", "")
		oidcSecret   = f.String("oidc-client-secret", "", "")
		oidcRedirect = f.String("oidc-redirect-url", "", "")
		oidcSession  = f.String("oidc-session-secret", "", "")
		oidcScopes   = f.String("oidc-scopes", "", "")
		shutdown     = f.Int("shutdown-timeout", 25, "")
		drain        = f.Int("drain-delay", 0, "")
	)
	if err := parseFlags(f, args, true, false); err != nil {
		return err
	}

	v := validation.New()

	setupLog(*dev, *debugFlag)

	if *dev {
		if err := setupReload(); err != nil {
			return err
		}
	}

	geodb := setupGeo(&v, *geodbFlag)
	ratelimits := setupRatelimits(&v, *ratelimit)
	setupDomains(&v, domainStatic, basePath)

	v.Range("-store-every", int64(*storeEvery), 1, 0)
	v.Range("-shutdown-timeout", int64(*shutdown), 1, 0)
	v.Range("-drain-delay", int64(*drain), 0, int64(*shutdown-1))
	if *dbConnect == "" {
		v.Append("-db", "a database URL or path is required")
	}

	if v.HasErrors() {
		return v
	}

	defer geodb.Close()
	db, ctx, err := connectDB(*dbConnect, *dbConn, *dev)
	if err != nil {
		return err
	}
	if *debugSQL {
		db = database.WithQueryLog(db, os.Stderr)
		ctx = database.WithDB(ctx, db)
	}
	defer closeDB(db)

	ctx = geo.With(ctx, geodb)

	if err := setupTpl(ctx, *dev); err != nil {
		return err
	}

	httpx.ErrPage = handlers.ErrPage

	c := goatcounter.Config(ctx)
	seenSites := make(map[string]bool)
	for _, name := range strings.Split(*sitesFlag, ",") {
		name = strings.TrimSpace(strings.TrimRight(name, "/"))
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seenSites[key] {
			return fmt.Errorf("duplicate site in -sites: %s", name)
		}
		seenSites[key] = true
		s := goatcounter.Site{Key: name, LinkDomain: name}
		s.Defaults()
		c.Sites = append(c.Sites, s)
	}
	if len(c.Sites) == 0 {
		return fmt.Errorf("-sites must contain at least one site")
	}
	c.Timezone, err = datetime.LoadTimezone()
	if err != nil {
		return err
	}
	c.DomainStatic = *domainStatic
	c.Dev = *dev
	c.BasePath = *basePath

	timeout := 60
	auth := handlers.Auth{Mode: handlers.AuthMode(*authMode)}
	switch auth.Mode {
	case handlers.AuthPublic:
	case handlers.AuthBasic:
		auth.BasicUsers, err = handlers.ParseBasicUsers(*basicAuth)
		if err != nil {
			return err
		}
	case handlers.AuthOIDC:
		for name, value := range map[string]string{
			"-oidc-issuer": *oidcIssuer, "-oidc-client-id": *oidcClientID,
			"-oidc-client-secret": *oidcSecret, "-oidc-redirect-url": *oidcRedirect,
			"-oidc-session-secret": *oidcSession,
		} {
			if value == "" {
				return fmt.Errorf("%s is required with -auth=oidc", name)
			}
		}
		if len(*oidcSession) < 32 {
			return fmt.Errorf("-oidc-session-secret must contain at least 32 bytes")
		}
		if err := handlers.ValidateOIDCRedirectURL(*oidcRedirect); err != nil {
			return fmt.Errorf("-oidc-redirect-url: %w", err)
		}
		oidcCtx, cancelOIDC := context.WithTimeout(ctx, 15*time.Second)
		auth.OIDC, err = handlers.NewOIDCAuth(oidcCtx, handlers.OIDCConfig{
			Issuer: *oidcIssuer, ClientID: *oidcClientID, ClientSecret: *oidcSecret,
			RedirectURL: *oidcRedirect, SessionSecret: *oidcSession, BasePath: *basePath,
			Scopes: strings.Split(*oidcScopes, ","),
		})
		cancelOIDC()
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("-auth must be public, basic, or oidc")
	}

	// Set up HTTP handler and servers.
	hosts := map[string]http.Handler{
		"*": handlers.NewBackend(db, *dev, c.DomainStatic, c.BasePath, timeout, ratelimits, *apiToken, auth),
	}
	if *domainStatic != "" {
		// May not be needed, but just in case the DomainStatic isn't an external CDN.
		hosts[hostWithoutPort(*domainStatic)] = handlers.NewStatic(chi.NewRouter(), *dev, c.BasePath)
	}

	server := &http.Server{
		Addr: *listen,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := r.Host
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			handler, ok := hosts[host]
			if !ok {
				handler = hosts["*"]
			}
			handler.ServeHTTP(w, r)
		}),
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second,
		WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second,
		Protocols: func() *http.Protocols {
			p := &http.Protocols{}
			p.SetHTTP1(true)
			p.SetHTTP2(true)
			p.SetUnencryptedHTTP2(true)
			return p
		}(),
	}
	ln, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	sig, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGHUP, syscall.SIGTERM, os.Interrupt)
	defer stopSignals()
	runner := cron.Start(ctx, time.Duration(*storeEvery)*time.Second)
	served := make(chan error, 1)
	go func() { served <- server.Serve(ln) }()
	slog.InfoContext(ctx, "GoatCounter ready",
		"listen", ln.Addr().String(), "timezone", c.Timezone.String(), "dev", *dev)
	ready <- struct{}{}
	var serveErr error
	select {
	case <-sig.Done():
	case <-stop:
	case serveErr = <-served:
	}
	c.Draining.Store(true)
	slog.InfoContext(ctx, "Draining HTTP requests", "delay_seconds", *drain)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(*shutdown)*time.Second)
	defer cancel()
	if *drain > 0 {
		timer := time.NewTimer(time.Duration(*drain) * time.Second)
		select {
		case <-timer.C:
		case <-shutdownCtx.Done():
			timer.Stop()
		}
	}
	shutdownErr := server.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		server.Close()
	}
	workerErr := runner.Stop(shutdownCtx)
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	if shutdownErr != nil {
		return fmt.Errorf("HTTP shutdown: %w", shutdownErr)
	}
	if workerErr != nil {
		return fmt.Errorf("worker shutdown: %w", workerErr)
	}
	slog.InfoContext(ctx, "Shutdown complete; pending pageviews remain in the shared database")
	return nil
}

func defaultDB() string { return "" }

func setupReload() error {
	if !fileExists("db/schema.gotxt") || !fileExists("tpl") || !fileExists("public") {
		return errors.New("-dev flag was given but this doesn't seem like a GoatCounter source directory")
	}
	slog.Info("watching ./tpl for changes")
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
			if err := handlers.LoadTemplates(os.DirFS(dir)); err != nil {
				slog.ErrorContext(context.Background(), err.Error())
			} else {
				slog.Info("reloaded templates")
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

func setupGeo(v *validation.Validator, geodbFlag string) *geoip2.Reader {
	geodb, err := geo.Open(geodbFlag)
	if err != nil {
		v.Append("-geodb", fmt.Sprintf("loading GeoIP database: %s", err))
	}
	return geodb
}

func setupRatelimits(v *validation.Validator, ratelimit string) handlers.Ratelimits {
	h := handlers.NewRatelimits()
	if strings.TrimSpace(ratelimit) == "" {
		return h
	}
	for entry := range strings.SplitSeq(ratelimit, ",") {
		name, spec, _ := strings.Cut(entry, ":")
		if strings.ToLower(strings.TrimSpace(name)) != "count" {
			v.Append("-ratelimit.name", fmt.Sprintf("unknown limit %q; only count is supported", name))
			continue
		}
		if strings.TrimSpace(spec) == "none" {
			h.ClearCount()
			continue
		}

		v2 := validation.New()
		requests, seconds, _ := strings.Cut(spec, "/")
		tokens := v2.Integer("-ratelimit.requests", requests)
		secs := v2.Integer("-ratelimit.seconds", seconds)
		v2.Range("-ratelimit.requests", tokens, 1, 0)
		v2.Range("-ratelimit.seconds", secs, 1, int64((1<<63-1)/time.Second))
		if v2.HasErrors() {
			v.Merge(v2)
			continue
		}
		h.SetCount(uint64(tokens), time.Duration(secs)*time.Second)
	}
	return h
}

func setupDomains(v *validation.Validator, domainStatic, basePath *string) {
	*basePath = strings.Trim(*basePath, "/")
	if *basePath != "" {
		*basePath = "/" + *basePath
	}

	if *domainStatic != "" {
		if p := strings.Index(*domainStatic, ":"); p > -1 {
			v.Domain("-static", (*domainStatic)[:p])
		} else {
			v.Domain("-static", *domainStatic)
		}
	}
}

func setupTpl(ctx context.Context, dev bool) error {
	fsys, err := embeddedOrDir(goatcounter.Templates, "tpl", dev)
	if err != nil {
		return err
	}
	err = handlers.LoadTemplates(fsys)
	if err != nil {
		if !dev {
			return err
		}
		slog.ErrorContext(ctx, err.Error())
	}
	return nil
}

func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }
func hostWithoutPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
