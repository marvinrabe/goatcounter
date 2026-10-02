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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/handlers"
	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/datetime"
	"github.com/marvinrabe/goatcounter/internal/enrich"
)

const usageServe = `
serve flags (all can also be set as GOATCOUNTER_«FLAG», e.g. GOATCOUNTER_DB):

  -db          Local database path or remote libSQL URL. Required.
               Remote example: libsql://your-database.example?authToken=TOKEN
               An empty database is initialized automatically on startup.

  -dbconn      Maximum connections as max_open,max_idle. Default: 4,2.
               Local SQLite files always use one connection. Remote databases
               don't keep idle connections, as remote streams expire.

  -sites       Comma-separated site names accepted by the collector and shown
               in the dashboard selector. Default: example.com.

  -auth        Dashboard authentication: public, basic, or oidc. Default: public.
  -basic-auth  Comma-separated username:password entries for -auth=basic.
  -oidc-issuer, -oidc-client-id, -oidc-client-secret, -oidc-session-secret
               OIDC settings for -auth=oidc. Register
               https://«your domain»/auth/callback as the redirect URL. The
               session secret must be at least 32 bytes.

  -listen      Address to listen on. Default: ":8080". Plain HTTP only; TLS is
               terminated by the proxy in front.
  -static      Serve static files from a different domain. Default: not set.
  -data-updates
               Update the bot and referrer spam lists from upstream once a
               day. Default: true. Without it, or when an update fails, the
               lists built into GoatCounter are used.
  -ratelimit   Limit requests to /count as count:requests/seconds, or
               count:none. Default: count:4/1.

  -shutdown-timeout
               Total shutdown deadline in seconds. Default: 25.
  -drain-delay Seconds to keep serving after becoming unready. Default: 0.

  -debug       Enable debug logs, including HTTP requests.
  -debug-sql   Log SQL queries.

The dashboard timezone is set with TZ, e.g. TZ=Europe/Berlin; default UTC.
`

func cmdServe(args []string, ready chan<- struct{}, stop chan struct{}) error {
	f := newFlags("cmdServe")
	var (
		domainStatic = f.String("static", "", "")
		dbConnect    = f.String("db", defaultDB(), "")
		dbConn       = f.String("dbconn", "4,2", "")
		debugFlag    = f.Bool("debug", false, "")
		debugSQL     = f.Bool("debug-sql", false, "")
		listen       = f.String("listen", ":8080", "")
		dataUpdates  = f.Bool("data-updates", true, "")
		ratelimit    = f.String("ratelimit", "", "")
		sitesFlag    = f.String("sites", "example.com", "")
		authMode     = f.String("auth", "public", "")
		basicAuth    = f.String("basic-auth", "", "")
		oidcIssuer   = f.String("oidc-issuer", "", "")
		oidcClientID = f.String("oidc-client-id", "", "")
		oidcSecret   = f.String("oidc-client-secret", "", "")
		oidcSession  = f.String("oidc-session-secret", "", "")
		shutdown     = f.Int("shutdown-timeout", 25, "")
		drain        = f.Int("drain-delay", 0, "")
	)
	if err := parseFlags(f, args, true, false); err != nil {
		return err
	}

	setupLog(*debugFlag)

	ratelimits, ratelimitErr := setupRatelimits(*ratelimit)
	errs := []error{ratelimitErr}
	if *domainStatic != "" && !validDomain(hostWithoutPort(*domainStatic)) {
		errs = append(errs, errors.New("-static: must be a valid domain"))
	}
	if *shutdown < 1 {
		errs = append(errs, errors.New("-shutdown-timeout: must be 1 or higher"))
	}
	if *drain < 0 || *drain >= max(*shutdown, 1) {
		errs = append(errs, errors.New("-drain-delay: must be 0 or higher, and lower than -shutdown-timeout"))
	}
	if *dbConnect == "" {
		errs = append(errs, errors.New("-db: a database URL or path is required"))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}

	db, ctx, err := connectDB(*dbConnect, *dbConn)
	if err != nil {
		return err
	}
	if *debugSQL {
		db = database.WithQueryLog(db, os.Stderr)
		ctx = database.WithDB(ctx, db)
	}
	defer closeDB(db)

	tpl, err := fs.Sub(goatcounter.Templates, "tpl")
	if err != nil {
		return err
	}
	if err := handlers.LoadTemplates(tpl); err != nil {
		return err
	}

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
			"-oidc-client-secret": *oidcSecret, "-oidc-session-secret": *oidcSession,
		} {
			if value == "" {
				return fmt.Errorf("%s is required with -auth=oidc", name)
			}
		}
		if len(*oidcSession) < 32 {
			return fmt.Errorf("-oidc-session-secret must contain at least 32 bytes")
		}
		oidcCtx, cancelOIDC := context.WithTimeout(ctx, 15*time.Second)
		auth.OIDC, err = handlers.NewOIDCAuth(oidcCtx, handlers.OIDCConfig{
			Issuer: *oidcIssuer, ClientID: *oidcClientID, ClientSecret: *oidcSecret,
			SessionSecret: *oidcSession,
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
		"*": handlers.NewBackend(db, c.DomainStatic, timeout, ratelimits, auth),
	}
	if *domainStatic != "" {
		// May not be needed, but just in case the DomainStatic isn't an external CDN.
		hosts[hostWithoutPort(*domainStatic)] = handlers.NewStatic(chi.NewRouter())
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
	served := make(chan error, 1)
	go func() { served <- server.Serve(ln) }()
	slog.InfoContext(ctx, "GoatCounter ready",
		"listen", ln.Addr().String(), "timezone", c.Timezone.String())
	ready <- struct{}{}
	if *dataUpdates {
		updates, stopUpdates := context.WithCancel(ctx)
		defer stopUpdates()
		go enrich.Update(updates)
	}
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
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	if shutdownErr != nil {
		return fmt.Errorf("HTTP shutdown: %w", shutdownErr)
	}
	slog.InfoContext(ctx, "Shutdown complete")
	return nil
}

func defaultDB() string { return "" }

func setupRatelimits(ratelimit string) (handlers.Ratelimits, error) {
	h := handlers.NewRatelimits()
	if strings.TrimSpace(ratelimit) == "" {
		return h, nil
	}
	var errs []error
	for entry := range strings.SplitSeq(ratelimit, ",") {
		name, spec, _ := strings.Cut(entry, ":")
		if strings.ToLower(strings.TrimSpace(name)) != "count" {
			errs = append(errs, fmt.Errorf("-ratelimit: unknown limit %q; only count is supported", name))
			continue
		}
		if strings.TrimSpace(spec) == "none" {
			h.ClearCount()
			continue
		}

		requests, seconds, _ := strings.Cut(spec, "/")
		tokens, err1 := strconv.ParseUint(strings.TrimSpace(requests), 10, 64)
		secs, err2 := strconv.ParseInt(strings.TrimSpace(seconds), 10, 64)
		if err1 != nil || err2 != nil || tokens < 1 || secs < 1 || secs > int64((1<<63-1)/time.Second) {
			errs = append(errs, fmt.Errorf("-ratelimit: %q must be count:requests/seconds with whole numbers of 1 or higher", entry))
			continue
		}
		h.SetCount(tokens, time.Duration(secs)*time.Second)
	}
	return h, errors.Join(errs...)
}

func validDomain(host string) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func hostWithoutPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
