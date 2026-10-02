package web

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/marvinrabe/goatcounter/internal/analytics"
	"github.com/marvinrabe/goatcounter/internal/enrich"
)

type statusWriter interface{ Status() int }

// requestContext sets a deadline for dynamic endpoints.
// Timeouts are chosen when routes are registered, not inferred from URL paths.
func requestContext(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer func() {
				cancel()
				if ctx.Err() == context.DeadlineExceeded {
					if ww, ok := w.(statusWriter); !ok || ww.Status() == 0 {
						w.WriteHeader(http.StatusGatewayTimeout)
						w.Write([]byte("Server timed out"))
					}
				}
			}()

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type siteKey struct{}

// siteFrom gets the site selected for the request by selectSite.
func siteFrom(ctx context.Context) analytics.Site {
	site, _ := ctx.Value(siteKey{}).(analytics.Site)
	return site
}

// selectSite selects the configured site for the request. The collector
// requires an explicit name when multiple sites are configured; pages default
// to the first site.
func (s *server) selectSite(requireName bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sites := s.store.Sites
			name := r.URL.Query().Get("site")
			if name == "" && len(sites) > 0 && (!requireName || len(sites) == 1) {
				name = sites[0].LinkDomain
			}
			site, ok := s.store.Site(name)
			if !ok {
				ErrPage(w, r, httpError(http.StatusBadRequest, "Unknown or missing site"))
				return
			}
			site.Defaults()
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), siteKey{}, site)))
		})
	}
}

func writeCSP(b *strings.Builder, k, v string) {
	b.WriteString(k)
	b.WriteByte(' ')
	b.WriteString(v)
	b.WriteByte(';')
}

func addcsp() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only really needs to run on HTML pages; but best to add it
			// everywhere as a "better safe than sorry" approach. However, the
			// /count gets called so often it makes sense to make an exception
			// for it.
			if r.URL.Path == "/count" {
				next.ServeHTTP(w, r)
				return
			}

			b := new(strings.Builder)
			b.Grow(1024)
			writeCSP(b, "default-src", "'none'")
			writeCSP(b, "font-src", "'self'")
			writeCSP(b, "form-action", "'self'")
			writeCSP(b, "frame-ancestors", "'none'")
			writeCSP(b, "manifest-src", "'self'")
			writeCSP(b, "script-src", "'self'")
			writeCSP(b, "style-src", "'self' 'unsafe-inline'")
			writeCSP(b, "connect-src", "'self'")
			writeCSP(b, "img-src", "'self' data:")
			writeCSP(b, "frame-src", "'self'")

			w.Header()["Content-Security-Policy"] = []string{b.String()}
			next.ServeHTTP(w, r)
		})
	}
}

// wrapWriter tracks whether a response has started while retaining HTTP
// streaming and upgrade support through chi's standard response wrapper.
func wrapWriter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(middleware.NewWrapResponseWriter(w, r.ProtoMajor), r)
	})
}
func securityHeaders(h http.Header) func(http.Handler) http.Handler {
	headers := http.Header{"Strict-Transport-Security": {"max-age=7776000"}, "X-Frame-Options": {"deny"}, "X-Content-Type-Options": {"nosniff"}}
	for k, v := range h {
		headers[k] = v
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for k, v := range headers {
				for _, s := range v {
					w.Header().Add(k, s)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store,no-cache")
		next.ServeHTTP(w, r)
	})
}

// realIP uses the visitor's address from the CDN's IPHeader, or else the
// address of the connection.
func realIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ip, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get(enrich.IPHeader))); err == nil {
			r.RemoteAddr = ip.String()
		} else if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			r.RemoteAddr = h
		}
		next.ServeHTTP(w, r)
	})
}

func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !slog.Default().Enabled(r.Context(), slog.LevelDebug) {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		next.ServeHTTP(w, r)
		if strings.HasSuffix(r.URL.Path, "/count") || strings.HasSuffix(r.URL.Path, "/robots.txt") {
			return
		}
		slog.With("module", "req").DebugContext(r.Context(), "HTTP request", "method", r.Method, "path", r.URL.Path, "elapsed", time.Since(start))
	})
}
