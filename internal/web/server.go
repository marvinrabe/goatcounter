package web

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/marvinrabe/goatcounter/internal/analytics"
)

// The dashboard's queries are cancelled after this.
const dashTimeout = 60 * time.Second

// New creates the handler for the collector, the dashboard, and the assets.
func New(store *analytics.Store, ratelimits Ratelimits, auth Auth) chi.Router {
	r := chi.NewRouter()
	s := &server{store: store}
	s.routes(r, ratelimits, auth)
	static(r)
	return r
}

type server struct {
	store *analytics.Store
}

func (s *server) routes(r chi.Router, ratelimits Ratelimits, auth Auth) {
	r.Use(
		realIP,
		wrapWriter,
		middleware.Recoverer,
		contentSecurityPolicy(),
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
	health.Get("/status", status(s.store.DB))
	health.Head("/status", status(s.store.DB))

	r.With(requestContext(3*time.Second), securityHeaders(nil), ratelimits.countMiddleware(), s.selectSite(true)).
		Post("/count", wrap(s.count))

	a := r.With(securityHeaders(http.Header{
		"Strict-Transport-Security": []string{"max-age=63072000"}, // 2 years
		"X-Content-Type-Options":    []string{"nosniff"},
		"X-Frame-Options":           []string{}, // Clear the default frame header
	}))
	auth.Mount(a.With(requestContext(3 * time.Second)))

	// Both the dashboard and its widget requests can run expensive queries.
	// Authenticate before loading any site data.
	af := a.With(requestContext(dashTimeout+time.Second), auth.Middleware, s.selectSite(false))
	af.Get("/", wrap(s.dashboard))
	af.Get("/load-widget", wrap(s.loadWidget))
}
