package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/httpx"
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

// selectSite selects the configured site without reading the database. The collector
// requires an explicit name when multiple sites are configured; pages default
// to the first site.
func selectSite(requireName bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			cfg := goatcounter.Config(ctx)
			name := r.URL.Query().Get("site")
			if name == "" && len(cfg.Sites) > 0 && (!requireName || len(cfg.Sites) == 1) {
				name = cfg.Sites[0].LinkDomain
			}
			s, ok := cfg.Site(name)
			if !ok {
				httpx.ErrPage(w, r, httpx.Error(http.StatusBadRequest, "Unknown or missing site"))
				return
			}
			s.Defaults()
			next.ServeHTTP(w, r.WithContext(goatcounter.WithSite(ctx, &s)))
		})
	}
}

func writeCSP(b *strings.Builder, k, v string) {
	b.WriteString(k)
	b.WriteByte(' ')
	b.WriteString(v)
	b.WriteByte(';')
}

func addcsp(domainStatic, basePath string) func(http.Handler) http.Handler {
	ds := []string{"'self'"}
	if domainStatic != "" {
		ds = append(ds, domainStatic)
	}

	var (
		staticDomains = strings.Join(ds, " ")
		wss           = "'self'" + " wss:"
	)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only really needs to run on HTML pages; but best to add it
			// everywhere as a "better safe than sorry" approach. However, the
			// /count gets called so often it makes sense to make an exception
			// for it.
			if r.URL.Path == basePath+"/count" {
				next.ServeHTTP(w, r)
				return
			}

			static := staticDomains

			b := new(strings.Builder)
			b.Grow(1024)
			writeCSP(b, "default-src", "'none'")
			writeCSP(b, "font-src", static)
			writeCSP(b, "form-action", "'self'")
			writeCSP(b, "frame-ancestors", "'none'")
			writeCSP(b, "manifest-src", static)
			writeCSP(b, "script-src", static)
			writeCSP(b, "style-src", static+" 'unsafe-inline'")

			writeCSP(b, "connect-src", wss)
			writeCSP(b, "img-src", static+" data:")
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
func realIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		public := func(s string) bool {
			ip, err := netip.ParseAddr(strings.TrimSpace(s))
			return err == nil && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified()
		}
		ip := ""
		for _, h := range []string{"CF-Connecting-IP", "Fly-Client-IP", "X-Azure-SocketIP", "X-Real-IP"} {
			if v := r.Header.Get(h); public(v) {
				ip = strings.TrimSpace(v)
				break
			}
		}
		if ip == "" {
			values := r.Header.Values("X-Forwarded-For")
			if len(values) > 0 {
				parts := strings.Split(values[len(values)-1], ",")
				for i := len(parts) - 1; i >= 0; i-- {
					if public(parts[i]) {
						ip = strings.TrimSpace(parts[i])
						break
					}
				}
			}
		}
		if ip == "" {
			ip = r.RemoteAddr
			if h, _, err := net.SplitHostPort(ip); err == nil {
				ip = h
			}
		}
		r.RemoteAddr = ip
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

func clientReport(w http.ResponseWriter, r *http.Request) {
	var report json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&report); err != nil {
		http.Error(w, "invalid report", http.StatusBadRequest)
		return
	}
	slog.With("module", "client").InfoContext(r.Context(), "Browser report", "path", r.URL.Path, "report", string(report))
	w.WriteHeader(http.StatusNoContent)
}
