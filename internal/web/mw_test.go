package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marvinrabe/goatcounter/internal/analytics"
	"github.com/marvinrabe/goatcounter/internal/enrich"
	"github.com/marvinrabe/goatcounter/internal/testenv"
)

func fmtCSP(h string) string {
	csp := make(map[string][]string)
	for f := range strings.SplitSeq(h, ";") {
		s := strings.Fields(f)
		if len(s) > 1 {
			csp[s[0]] = s[1:]
		}
	}
	keys := make([]string, 0, len(csp))
	l := 0
	for k := range csp {
		keys = append(keys, k)
		l = max(l, len(k))
	}
	slices.Sort(keys)
	var s strings.Builder
	for _, k := range keys {
		s.WriteString(k)
		s.WriteString(strings.Repeat(" ", l-len(k)+1))
		s.WriteString(strings.Join(csp[k], " "))
		s.WriteByte('\n')
	}
	return s.String()
}

func TestAddCSP(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/", `
			connect-src     'self'
			default-src     'none'
			font-src        'self'
			form-action     'self'
			frame-ancestors 'none'
			frame-src       'self'
			img-src         'self' data:
			manifest-src    'self'
			script-src      'self'
			style-src       'self' 'unsafe-inline'
		`},
		{"/count", ``},
	}

	mw := addcsp()(http.NewServeMux())
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			var (
				r  = testenv.NewRequest("GET", tt.path, nil)
				rr = httptest.NewRecorder()
			)

			mw.ServeHTTP(rr, r)

			tt.want = testenv.NormalizeIndent(tt.want)
			have := fmtCSP(rr.Header().Get("Content-Security-Policy"))
			if d := testenv.Diff(have, tt.want); d != "" {
				t.Error(d)
			}
		})
	}
}

func TestRequestContext(t *testing.T) {
	for _, written := range []bool{false, true} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		h := wrapWriter(requestContext(-time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Context().Err() != context.DeadlineExceeded {
				t.Error("request deadline was not applied")
			}
			if written {
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte("already handled"))
			}
		})))
		h.ServeHTTP(w, r)
		code, body := http.StatusGatewayTimeout, "Server timed out"
		if written {
			code, body = http.StatusServiceUnavailable, "already handled"
		}
		if w.Code != code || w.Body.String() != body {
			t.Errorf("written=%t: got %d %q; want %d %q", written, w.Code, w.Body.String(), code, body)
		}
	}
}

func TestSelectSite(t *testing.T) {
	first := analytics.Site{Key: "example.com", LinkDomain: "example.com"}
	second := first
	second.Key, second.LinkDomain = "second.example.com", "second.example.com"
	for _, tt := range []struct {
		name        string
		sites       []analytics.Site
		query       string
		requireName bool
		want        string
	}{
		{"no sites", nil, "", false, ""},
		{"single collector", []analytics.Site{first}, "", true, first.Key},
		{"multiple collector missing name", []analytics.Site{first, second}, "", true, ""},
		{"multiple collector selected", []analytics.Site{first, second}, "?site=SECOND.EXAMPLE.COM", true, second.Key},
		{"dashboard default", []analytics.Site{first, second}, "", false, first.Key},
		{"dashboard selected", []analytics.Site{first, second}, "?site=second.example.com", false, second.Key},
		{"unknown site", []analytics.Site{first, second}, "?site=unknown", false, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := &server{store: &analytics.Store{Sites: tt.sites}}
			r := httptest.NewRequest(http.MethodGet, "/"+tt.query, nil)
			w := httptest.NewRecorder()
			h := s.selectSite(tt.requireName)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := siteFrom(r.Context()).Key; got != tt.want {
					t.Errorf("site = %q; want %q", got, tt.want)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			h.ServeHTTP(w, r)
			code := http.StatusNoContent
			if tt.want == "" {
				code = http.StatusBadRequest
			}
			if w.Code != code {
				t.Errorf("status = %d; want %d", w.Code, code)
			}
		})
	}
}

func BenchmarkAddCSP(b *testing.B) {
	var (
		r  = testenv.NewRequest("GET", "/", nil)
		rr = httptest.NewRecorder()
		mw = addcsp()(http.NewServeMux())
	)
	b.ResetTimer()
	for b.Loop() {
		mw.ServeHTTP(rr, r)
	}
}

func TestRealIP(t *testing.T) {
	for _, tt := range []struct {
		remote, realIP, want string
	}{
		{"192.0.2.1:1234", "", "192.0.2.1"},
		{"10.0.0.1:1234", "198.51.100.7", "198.51.100.7"},
		{"10.0.0.1:1234", " 2001:db8::1 ", "2001:db8::1"},
		{"10.0.0.1:1234", "not-an-ip", "10.0.0.1"},
	} {
		r := httptest.NewRequest("GET", "/count", nil)
		r.RemoteAddr = tt.remote
		r.Header.Set(enrich.IPHeader, tt.realIP)
		// Headers from other CDNs aren't trusted, as bunny.net passes them on.
		r.Header.Set("CF-Connecting-IP", "203.0.113.9")
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
		var have string
		realIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			have = r.RemoteAddr
		})).ServeHTTP(httptest.NewRecorder(), r)
		if have != tt.want {
			t.Errorf("%q, %q: have %q; want %q", tt.remote, tt.realIP, have, tt.want)
		}
	}
}

func BenchmarkRequestContext(b *testing.B) {
	b.Run("without site", func(b *testing.B) {
		var (
			r  = testenv.NewRequest("GET", "/", nil)
			rr = httptest.NewRecorder()
			mw = requestContext(10 * time.Second)(http.NewServeMux())
		)
		b.ResetTimer()
		for b.Loop() {
			mw.ServeHTTP(rr, r)
		}
	})

	b.Run("with site", func(b *testing.B) {
		var (
			s  = &server{store: testenv.Store(b)}
			r  = testenv.NewRequest("GET", "/", nil)
			rr = httptest.NewRecorder()
			mw = requestContext(10 * time.Second)(s.selectSite(false)(http.NewServeMux()))
		)
		r.Host = "testenv.localhost"
		b.ResetTimer()
		for b.Loop() {
			mw.ServeHTTP(rr, r)
		}
	})
}
