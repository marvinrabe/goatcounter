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

var (
	// basicAuth authenticates the request with HTTP basic auth; the username
	// is ignored and the password is checked against all users' bcrypt
	// hashes.
	basicAuth = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Parse the form for POST bodies; the old auth middleware did
			// this as part of the CSRF check, and handlers rely on r.Form.
			if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
				r.ParseMultipartForm(32 << 20)
			} else {
				r.ParseForm()
			}

			_, pass, ok := r.BasicAuth()
			if ok {
				var users goatcounter.Users
				err := users.List(ctx)
				if err == nil {
					for _, u := range users {
						ok, err := u.CorrectPassword(pass)
						if err != nil {
							log.Error(ctx, err)
							continue
						}
						if ok {
							next.ServeHTTP(w, r.WithContext(goatcounter.WithUser(ctx, &u)))
							return
						}
					}
				} else if !zdb.ErrNoRows(err) {
					log.Error(ctx, err)
				}
			}

			w.Header().Set("WWW-Authenticate", `Basic realm="GoatCounter"`)
			zhttp.ErrPage(w, r, guru.New(401, "Authentication required"))
		})
	}

	loggedIn = basicAuth

	loggedInOrPublic = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s := Site(r.Context())
			if s.Settings.IsPublic() {
				next.ServeHTTP(w, r)
				return
			}
			if a := r.URL.Query().Get("access-token"); s.Settings.CanView(a) {
				// Set cookie for auth and redirect. This prevents accidental
				// leaking of the secret by copy/pasting the URL, screenshots, etc.
				http.SetCookie(w, &http.Cookie{
					Name:     "access-token",
					Value:    a,
					Path:     "/",
					HttpOnly: true,
					Secure:   zhttp.IsSecure(r),
					SameSite: http.SameSiteLaxMode,
				})
				hide := ""
				if r.URL.Query().Get("hideui") != "" {
					hide += "?hideui=1"
				}
				next.ServeHTTP(w, r)
				return
			}
			if c, err := r.Cookie("access-token"); err == nil && s.Settings.CanView(c.Value) {
				next.ServeHTTP(w, r)
				return
			}

			basicAuth(next).ServeHTTP(w, r)
		})
	}
)

type statusWriter interface{ Status() int }

func addctx(db zdb.DB, loadSite bool, dashTimeout int) func(http.Handler) http.Handler {
	Started = time.Now()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Intercept /status here so it works everywhere.
			if r.URL.Path == "/status" {
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
			if r.URL.Path == "/" {
				t = dashTimeout + 1
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

			// There's only ever one site; load it (creating it on first run).
			ctx = goatcounter.WithHost(ctx, r.Host)
			if loadSite {
				var s goatcounter.Site
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
