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

  -debug       Enable debug logs, including HTTP requests and SQL queries.

The dashboard timezone is set with TZ, e.g. TZ=Europe/Berlin; default UTC.
`

// Shutdown deadline for in-flight requests; below the usual 30 second grace
// period before a container is killed.
const shutdownTimeout = 25 * time.Second

func cmdServe(args []string, ready chan<- struct{}, stop chan struct{}) error {
	f := newFlags("cmdServe")
	var (
		dbConnect    = f.String("db", "", "")
		debug        = f.Bool("debug", false, "")
		listen       = f.String("listen", ":8080", "")
		siteList     = f.String("sites", "example.com", "")
		authMode     = f.String("auth", "public", "")
		basicAuth    = f.String("basic-auth", "", "")
		oidcIssuer   = f.String("oidc-issuer", "", "")
		oidcClientID = f.String("oidc-client-id", "", "")
		oidcSecret   = f.String("oidc-client-secret", "", "")
		oidcSession  = f.String("oidc-session-secret", "", "")
	)
	if err := parseFlags(f, args, true, false); err != nil {
		return err
	}

	setupLog(*debug)

	if *dbConnect == "" {
		return errors.New("-db: a database URL or path is required")
	}
	db, ctx, err := connectDB(*dbConnect)
	if err != nil {
		return err
	}
	if *debug {
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
	for _, name := range strings.Split(*siteList, ",") {
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

	server := &http.Server{
		Addr:              *listen,
		Handler:           handlers.NewBackend(db, timeout, handlers.NewRatelimits(), auth),
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
	updates, stopUpdates := context.WithCancel(ctx)
	defer stopUpdates()
	go enrich.Update(updates)
	var serveErr error
	select {
	case <-sig.Done():
	case <-stop:
	case serveErr = <-served:
	}
	slog.InfoContext(ctx, "Shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
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
