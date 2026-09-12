package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/marvinrabe/goatcounter/internal/log"
	"github.com/sethvargo/go-limiter"
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

	backend{dashTimeout: dashTimeout, apiToken: apiToken}.Mount(r, db, dev, domainStatic, ratelimits, auth)

	NewStatic(r, dev, basePath)

	return root
}

type backend struct {
	dashTimeout int
	apiToken    string
}

func (h backend) Mount(r chi.Router, db zdb.DB, dev bool, domainStatic string, ratelimits Ratelimits, auth Auth) {
	r.Use(
		mware.RealIP(),
		mware.WrapWriter(),
		mware.Unpanic("github.com/marvinrabe/goatcounter/handlers.add"),
		addctx(db, true, h.dashTimeout),
		addcsp(domainStatic),
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

	{
		rr := r.With(mware.Headers(nil))
		rr.Get("/robots.txt", zhttp.HandlerRobots([][]string{{"User-agent: *", "Disallow: /"}}))
		rr.Get("/security.txt", zhttp.Wrap(func(w http.ResponseWriter, r *http.Request) error {
			return zhttp.Text(w, "Contact: support@goatcounter.com")
		}))
		rr.Post("/jserr", zhttp.HandlerJSErr())
		rr.Post("/csp", zhttp.HandlerCSP())

		// Requests from localhost and -dev aren't rate limited in practice, but
		// still go through a store so the X-Rate-Limit headers are set. The
		// store is created once here rather than inside the callback below:
		// memorystore.New starts a purge goroutine that only stops on Close(),
		// so building one per request leaks a goroutine and its ticker.
		//
		// Note RealIP has already stripped the port by this point, so the
		// localhost check below matches a same-host reverse proxy that doesn't
		// set a forwarding header, not just local testing.
		unlimitedTokens := uint64(1 << 14)
		if dev {
			unlimitedTokens = 1 << 30
		}
		unlimited := mustNewMem(unlimitedTokens, 1)

		// 4 pageviews/second should be more than enough.
		rate := rr.With(Ratelimit(true, func(r *http.Request) ([]limiter.Store, string) {
			if dev || r.RemoteAddr == "127.0.0.1" {
				return []limiter.Store{unlimited}, ""
			}
			return []limiter.Store{ratelimits.Count}, ""
		}))
		rate.Get("/count", zhttp.Wrap(h.count))
		rate.Post("/count", zhttp.Wrap(h.count)) // to support navigator.sendBeacon (JS)
	}

	a := r.With(mware.Headers(http.Header{
		"Strict-Transport-Security": []string{"max-age=63072000"}, // 2 years
		"X-Content-Type-Options":    []string{"nosniff"},
		"X-Frame-Options":           []string{}, // Clear default from zhttp
	}))
	auth.Mount(a)
	af := a.With(auth.Middleware)
	af.Get("/", zhttp.Wrap(h.dashboard))
	af.Get("/load-widget", zhttp.Wrap(h.loadWidget))

	// An empty token disables the API completely: no route is registered.
	if h.apiToken != "" {
		a.With(h.bearerAuth).Get("/api", h.api)
		a.With(h.bearerAuth).Post("/api", h.api)
	}
}
