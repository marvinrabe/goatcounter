package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/marvinrabe/goatcounter"
	"zgo.at/guru"
	"zgo.at/zhttp"
	"zgo.at/zhttp/header"
)

type statusWriter interface{ Status() int }

// requestContext sets a deadline and the request host for dynamic endpoints.
// Timeouts are chosen when routes are registered, not inferred from URL paths.
func requestContext(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := goatcounter.WithHost(r.Context(), r.Host)
			ctx, cancel := context.WithTimeout(ctx, timeout)
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
				zhttp.ErrPage(w, r, guru.New(http.StatusBadRequest, "Unknown or missing site"))
				return
			}
			s.Defaults(ctx)
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
			if r.URL.Path == basePath+"/count" {
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
