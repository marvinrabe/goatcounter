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
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/handlers"
	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/dataset"
	"github.com/marvinrabe/goatcounter/internal/datetime"
	"github.com/marvinrabe/goatcounter/internal/enrich"
	"github.com/marvinrabe/goatcounter/internal/geo/geoip2"
	"github.com/marvinrabe/goatcounter/internal/httpx"
	"github.com/marvinrabe/goatcounter/internal/validation"
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
  -oidc-issuer, -oidc-client-id, -oidc-client-secret, -oidc-redirect-url,
  -oidc-session-secret, -oidc-scopes
               OIDC settings for -auth=oidc. The redirect URL must be
               /auth/callback on the GoatCounter domain. The session secret
               must be at least 32 bytes.

  -listen      Address to listen on. Default: ":8080". Plain HTTP only; TLS is
               terminated by the proxy in front.
  -static      Serve static files from a different domain. Default: not set.
  -country-header
               Request header with the visitor's country code, set by the CDN
               in front, e.g. CDN-RequestCountryCode for bunny.net or
               CF-IPCountry for Cloudflare. Only set it if all traffic goes
               through that CDN, as anyone can send the header. Default: not
               set.
  -geodb       Path to a Country or City mmdb GeoIP database, for the country
               without -country-header. Default: not set.
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
		geodbFlag    = f.String("geodb", "", "")
		countryHdr   = f.String("country-header", "", "")
		dataUpdates  = f.Bool("data-updates", true, "")
		ratelimit    = f.String("ratelimit", "", "")
		sitesFlag    = f.String("sites", "example.com", "")
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

	setupLog(*debugFlag)

	geodb := setupGeo(&v, *geodbFlag)
	ratelimits := setupRatelimits(&v, *ratelimit)
	setupDomains(&v, *domainStatic)

	v.Range("-shutdown-timeout", int64(*shutdown), 1, 0)
	v.Range("-drain-delay", int64(*drain), 0, int64(*shutdown-1))
	if *dbConnect == "" {
		v.Append("-db", "a database URL or path is required")
	}

	if v.HasErrors() {
		return v
	}

	if geodb != nil {
		defer geodb.Close()
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
	c.Geo = &enrich.Geo{Header: strings.TrimSpace(*countryHdr), DB: geodb}

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
			RedirectURL: *oidcRedirect, SessionSecret: *oidcSession,
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
		go dataset.Run(updates, enrich.Datasets()...)
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

func setupGeo(v *validation.Validator, geodbFlag string) *geoip2.Reader {
	if geodbFlag == "" {
		return nil
	}
	geodb, err := enrich.OpenGeoDB(geodbFlag)
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

func setupDomains(v *validation.Validator, domainStatic string) {
	if domainStatic != "" {
		v.Domain("-static", hostWithoutPort(domainStatic))
	}
}

func hostWithoutPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
