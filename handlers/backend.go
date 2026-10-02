package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/marvinrabe/goatcounter/internal/database"
)

func NewBackend(db database.DB, dashTimeout int, ratelimits Ratelimits, auth Auth) chi.Router {
	r := chi.NewRouter()
	backend{dashTimeout: dashTimeout}.Mount(r, db, ratelimits, auth)
	NewStatic(r)
	return r
}

type backend struct {
	dashTimeout int
}

func (h backend) Mount(r chi.Router, db database.DB, ratelimits Ratelimits, auth Auth) {
	r.Use(
		realIP,
		wrapWriter,
		middleware.Recoverer,
		addcsp(),
		middleware.RedirectSlashes,
		noStore,
		middleware.Compress(5))
	r.Use(requestLog)

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		ErrPage(w, r, httpError(404, "Not Found"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		ErrPage(w, r, httpError(405, "Method Not Allowed"))
	})

	// Health checks do not require a site or dashboard authentication.
	health := r.With(requestContext(3 * time.Second))
	health.Get("/status", status(db))
	health.Head("/status", status(db))

	r.With(requestContext(3*time.Second), securityHeaders(nil), ratelimits.countMiddleware(), selectSite(true)).
		Post("/count", wrap(h.count))

	a := r.With(securityHeaders(http.Header{
		"Strict-Transport-Security": []string{"max-age=63072000"}, // 2 years
		"X-Content-Type-Options":    []string{"nosniff"},
		"X-Frame-Options":           []string{}, // Clear the default frame header
	}))
	auth.Mount(a.With(requestContext(3 * time.Second)))

	// Both the dashboard and its widget requests can run expensive queries.
	// Authenticate before loading any site data.
	af := a.With(requestContext(time.Duration(h.dashTimeout+1)*time.Second), auth.Middleware, selectSite(false))
	af.Get("/", wrap(h.dashboard))
	af.Get("/load-widget", wrap(h.loadWidget))
}
