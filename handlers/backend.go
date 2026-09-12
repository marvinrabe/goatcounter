package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/marvinrabe/goatcounter/internal/log"
	"zgo.at/guru"
	"zgo.at/zdb"
	"zgo.at/zhttp"
	"zgo.at/zhttp/mware"
)

func NewBackend(db zdb.DB, dev bool,
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

func (h backend) Mount(r chi.Router, db zdb.DB, dev bool, domainStatic, basePath string, ratelimits Ratelimits, auth Auth) {
	r.Use(
		mware.RealIP(),
		mware.WrapWriter(),
		mware.Unpanic("github.com/marvinrabe/goatcounter/handlers.add"),
		addcsp(domainStatic, basePath),
		middleware.RedirectSlashes,
		mware.NoStore(),
		middleware.Compress(5))
	if log.HasDebug("req") {
		opt := &mware.RequestLogOptions{}
		if !dev {
			opt.Host = true
			opt.TimeFmt = time.DateTime
		}
		r.Use(mware.RequestLog(opt, nil, "/count", "/robots.txt"))
	}

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		zhttp.ErrPage(w, r, guru.New(404, T(r.Context(), "error/not-found|Not Found")))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		zhttp.ErrPage(w, r, guru.New(405, "Method Not Allowed"))
	})

	// Health checks do not require a site or dashboard authentication.
	health := r.With(requestContext(3 * time.Second))
	health.Get("/status", status(db))
	health.Head("/status", status(db))

	{
		rr := r.With(requestContext(3*time.Second), mware.Headers(nil))
		rr.Post("/jserr", zhttp.HandlerJSErr())
		rr.Post("/csp", zhttp.HandlerCSP())

		rate := rr.With(ratelimits.countMiddleware(dev), selectSite(true))
		rate.Get("/count", zhttp.Wrap(h.count))
		rate.Post("/count", zhttp.Wrap(h.count)) // to support navigator.sendBeacon (JS)
	}

	a := r.With(mware.Headers(http.Header{
		"Strict-Transport-Security": []string{"max-age=63072000"}, // 2 years
		"X-Content-Type-Options":    []string{"nosniff"},
		"X-Frame-Options":           []string{}, // Clear default from zhttp
	}))
	auth.Mount(a.With(requestContext(3 * time.Second)))

	// Both the dashboard and its widget requests can run expensive queries.
	// Authenticate before loading any site data.
	af := a.With(requestContext(time.Duration(h.dashTimeout+1)*time.Second), auth.Middleware, selectSite(false))
	af.Get("/", zhttp.Wrap(h.dashboard))
	af.Get("/load-widget", zhttp.Wrap(h.loadWidget))

	// An empty token disables the API completely: no route is registered.
	if h.apiToken != "" {
		// API actions select their own sites from their arguments.
		api := a.With(requestContext(time.Duration(h.dashTimeout)*time.Second), h.bearerAuth)
		api.Get("/api", h.api)
		api.Post("/api", h.api)
	}
}
