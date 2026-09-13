package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"github.com/marvinrabe/goatcounter/internal/testutil"
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
			connect-src     'self' wss:
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

	for _, base := range []string{"", "/stats"} {
		mw := addcsp("", base)(http.NewServeMux())
		for _, tt := range tests {
			t.Run(base+tt.path, func(t *testing.T) {
				var (
					r  = testutil.NewRequest("GET", base+tt.path, nil)
					rr = httptest.NewRecorder()
				)

				mw.ServeHTTP(rr, r)

				tt.want = testutil.NormalizeIndent(tt.want)
				have := fmtCSP(rr.Header().Get("Content-Security-Policy"))
				if d := testutil.Diff(have, tt.want); d != "" {
					t.Error(d)
				}
			})
		}
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
	ctx := testenv.Context(nil)
	first := goatcounter.Config(ctx).Sites[0]
	second := first
	second.Key, second.LinkDomain = "second.example.com", "second.example.com"
	for _, tt := range []struct {
		name        string
		sites       []goatcounter.Site
		query       string
		requireName bool
		want        string
	}{
		{"no sites", nil, "", false, ""},
		{"single collector", []goatcounter.Site{first}, "", true, first.Key},
		{"multiple collector missing name", []goatcounter.Site{first, second}, "", true, ""},
		{"multiple collector selected", []goatcounter.Site{first, second}, "?site=SECOND.EXAMPLE.COM", true, second.Key},
		{"dashboard default", []goatcounter.Site{first, second}, "", false, first.Key},
		{"dashboard selected", []goatcounter.Site{first, second}, "?site=second.example.com", false, second.Key},
		{"unknown site", []goatcounter.Site{first, second}, "?site=unknown", false, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			goatcounter.Config(ctx).Sites = tt.sites
			r := httptest.NewRequest(http.MethodGet, "/"+tt.query, nil).WithContext(ctx)
			w := httptest.NewRecorder()
			h := selectSite(tt.requireName)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := Site(r.Context()).Key; got != tt.want {
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
		ctx = goatcounter.WithSite(context.Background(), &goatcounter.Site{})
		r   = testutil.NewRequest("GET", "/", nil).WithContext(ctx)
		rr  = httptest.NewRecorder()
		mw  = addcsp("", "")(http.NewServeMux())
	)
	b.ResetTimer()
	for b.Loop() {
		mw.ServeHTTP(rr, r)
	}
}

func BenchmarkRequestContext(b *testing.B) {
	b.Run("without site", func(b *testing.B) {
		var (
			ctx = goatcounter.WithSite(context.Background(), &goatcounter.Site{})
			r   = testutil.NewRequest("GET", "/", nil).WithContext(ctx)
			rr  = httptest.NewRecorder()
			mw  = requestContext(10 * time.Second)(http.NewServeMux())
		)
		b.ResetTimer()
		for b.Loop() {
			mw.ServeHTTP(rr, r)
		}
	})

	b.Run("with site", func(b *testing.B) {
		var (
			ctx = testenv.DB(b)
			r   = testutil.NewRequest("GET", "/", nil).WithContext(ctx)
			rr  = httptest.NewRecorder()
			mw  = requestContext(10 * time.Second)(selectSite(false)(http.NewServeMux()))
		)
		r.Host = "testenv.localhost"
		b.ResetTimer()
		for b.Loop() {
			mw.ServeHTTP(rr, r)
		}
	})
}
