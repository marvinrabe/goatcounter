package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/httpx"
)

func NewBackend(db database.DB, dev bool,
	domainStatic string, basePath string, dashTimeout int, ratelimits Ratelimits, apiToken string, auth Auth,
) chi.Router {

	root := chi.NewRouter()
	r := root
	if basePath != "" {
		r = chi.NewRouter()
		root.Mount(basePath, r)
	}

	backend{dashTimeout: dashTimeout, apiToken: apiToken}.Mount(r, db, dev, domainStatic, basePath, ratelimits, auth)

	NewStatic(r, dev, basePath)

	return root
}

type backend struct {
	dashTimeout int
	apiToken    string
}

func (h backend) Mount(r chi.Router, db database.DB, dev bool, domainStatic, basePath string, ratelimits Ratelimits, auth Auth) {
	r.Use(
		realIP,
		wrapWriter,
		middleware.Recoverer,
		addcsp(domainStatic, basePath),
		middleware.RedirectSlashes,
		noStore,
		middleware.Compress(5))
	r.Use(requestLog)

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.ErrPage(w, r, httpx.Error(404, "Not Found"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.ErrPage(w, r, httpx.Error(405, "Method Not Allowed"))
	})

	// Health checks do not require a site or dashboard authentication.
	health := r.With(requestContext(3 * time.Second))
	health.Get("/status", status(db))
	health.Head("/status", status(db))

	{
		rr := r.With(requestContext(3*time.Second), securityHeaders(nil))
		rr.Post("/jserr", clientReport)
		rr.Post("/csp", clientReport)

		rate := rr.With(ratelimits.countMiddleware(dev), selectSite(true))
		rate.Get("/count", httpx.Wrap(h.count))
		rate.Post("/count", httpx.Wrap(h.count)) // to support navigator.sendBeacon (JS)
	}

	a := r.With(securityHeaders(http.Header{
		"Strict-Transport-Security": []string{"max-age=63072000"}, // 2 years
		"X-Content-Type-Options":    []string{"nosniff"},
		"X-Frame-Options":           []string{}, // Clear the default frame header
	}))
	auth.Mount(a.With(requestContext(3 * time.Second)))

	// Both the dashboard and its widget requests can run expensive queries.
	// Authenticate before loading any site data.
	af := a.With(requestContext(time.Duration(h.dashTimeout+1)*time.Second), auth.Middleware, selectSite(false))
	af.Get("/", httpx.Wrap(h.dashboard))
	af.Get("/load-widget", httpx.Wrap(h.loadWidget))

	// An empty token disables the API completely: no route is registered.
	if h.apiToken != "" {
		// API actions select their own sites from their arguments.
		api := a.With(requestContext(time.Duration(h.dashTimeout)*time.Second), h.bearerAuth)
		api.Get("/api", h.api)
		api.Post("/api", h.api)
	}
}
