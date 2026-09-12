package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/log"
	"github.com/sethvargo/go-limiter"
	"zgo.at/guru"
	"zgo.at/zdb"
	"zgo.at/zhttp"
	"zgo.at/zhttp/header"
)

// Started is set when the server is started.
var Started time.Time

type statusWriter interface{ Status() int }

func addctx(db zdb.DB, loadSite bool, dashTimeout int) func(http.Handler) http.Handler {
	Started = time.Now()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			path := strings.TrimPrefix(r.URL.Path, goatcounter.Config(ctx).BasePath)

			// Intercept /status here so it works everywhere.
			if path == "/status" {
				if _, err := zdb.Info(r.Context()); err != nil {
					http.Error(w, "database unreachable", http.StatusServiceUnavailable)
					return
				}

				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(200)
				w.Write([]byte("OK"))
				return
			}

			// Add timeout.
			t := 3
			if path == "/" {
				t = dashTimeout + 1
			} else if path == "/api" {
				t = dashTimeout
			} else if strings.HasPrefix(r.URL.Path, "/counter/") {
				t = dashTimeout
			}
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Duration(t)*time.Second)
			defer func() {
				cancel()
				if ctx.Err() == context.DeadlineExceeded {
					if ww, ok := w.(statusWriter); !ok || ww.Status() == 0 {
						w.WriteHeader(http.StatusGatewayTimeout)
						w.Write([]byte("Server timed out"))
					}
				}
			}()

			// Select the configured site. An omitted value selects the first site
			// for normal pages; /count requires an explicit site when more than
			// one is configured.
			ctx = goatcounter.WithHost(ctx, r.Host)
			if loadSite {
				cfg := goatcounter.Config(ctx)
				name := r.URL.Query().Get("site")
				if name == "" && (path != "/count" || len(cfg.Sites) == 1) {
					name = cfg.Sites[0].LinkDomain
				}
				s, ok := cfg.Site(name)
				if !ok {
					zhttp.ErrPage(w, r, guru.New(400, "Unknown or missing site"))
					return
				}
				if err := s.Load(ctx); err != nil {
					zhttp.ErrPage(w, r, err)
					return
				}
				ctx = goatcounter.WithSite(ctx, &s)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeCSP(b *strings.Builder, k, v string) {
	b.WriteString(k)
	b.WriteByte(' ')
	b.WriteString(v)
	b.WriteByte(';')
}

func addcsp(domainStatic string) func(http.Handler) http.Handler {
	ds := []string{header.CSPSourceSelf}
	if domainStatic != "" {
		ds = append(ds, domainStatic)
	}

	var (
		staticDomains = strings.Join(ds, " ")
		wss           = header.CSPSourceSelf + " wss:"
	)

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

			static := staticDomains

			b := new(strings.Builder)
			b.Grow(1024)
			writeCSP(b, header.CSPDefaultSrc, header.CSPSourceNone)
			writeCSP(b, header.CSPFontSrc, static)
			writeCSP(b, header.CSPFormAction, header.CSPSourceSelf)
			writeCSP(b, header.CSPFrameAncestors, header.CSPSourceNone)
			writeCSP(b, header.CSPManifestSrc, static)
			writeCSP(b, header.CSPScriptSrc, static)
			writeCSP(b, header.CSPStyleSrc, static+" 'unsafe-inline'")

			writeCSP(b, header.CSPConnectSrc, wss)
			writeCSP(b, header.CSPImgSrc, static+" data:")
			writeCSP(b, header.CSPFrameSrc, header.CSPSourceSelf)

			w.Header()["Content-Security-Policy"] = []string{b.String()}
			next.ServeHTTP(w, r)
		})
	}
}

func Ratelimit(withUA bool, getStore func(r *http.Request) ([]limiter.Store, string)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			stores, msg := getStore(r)
			if len(stores) == 0 {
				next.ServeHTTP(w, r)
				return
			}
			key := r.RemoteAddr
			if withUA {
				// Add in the User-Agent for some endpoints to reduce the
				// problem of multiple people in the same building hitting the
				// limit.
				key += r.UserAgent()
			}

			// Report either the failing ratelimit or the last one, It's assumed
			// that ratelimits are ordered from longest to shortest.
			var (
				tokens, remaining, reset uint64
				ok                       bool
			)
			for _, store := range stores {
				if store == nil {
					continue
				}
				var err error
				tokens, remaining, reset, ok, err = store.Take(r.Context(), key)
				if err != nil {
					// The memorystore only returns an error if Close() was called.
					// But log just to be sure.
					log.Module("ratelimit").Error(r.Context(), err, "key", key)
					ok = false
				}
				if !ok {
					break
				}
			}

			t := time.Unix(0, int64(reset))
			exp := -time.Since(t)
			retryAfter := strconv.FormatFloat(exp.Seconds(), 'f', 0, 64)
			w.Header().Set("X-Rate-Limit-Limit", strconv.FormatUint(tokens, 10))
			w.Header().Set("X-Rate-Limit-Remaining", strconv.FormatUint(remaining, 10))
			w.Header().Set("X-Rate-Limit-Reset", retryAfter)

			if !ok {
				w.Header().Set("Retry-After", retryAfter)
				w.WriteHeader(http.StatusTooManyRequests)

				if msg == "" {
					msg = fmt.Sprintf("rate limited exceeded; try again in %s", exp)
				}
				if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
					fmt.Fprintf(w, `{"error": %q}`, msg)
				} else {
					fmt.Fprintf(w, "%s\n", msg)
				}
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
