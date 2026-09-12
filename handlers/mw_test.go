package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"zgo.at/zstd/zmap"
	"zgo.at/zstd/ztest"
)

func fmtCSP(h string) string {
	csp := make(map[string][]string)
	for f := range strings.SplitSeq(h, ";") {
		s := strings.Fields(f)
		if len(s) > 1 {
			csp[s[0]] = s[1:]
		}
	}
	keys, l := zmap.LongestKey(csp)
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

	mw := addcsp("")(http.NewServeMux())
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			var (
				r  = ztest.NewRequest("GET", tt.path, nil)
				rr = httptest.NewRecorder()
			)

			mw.ServeHTTP(rr, r)

			tt.want = ztest.NormalizeIndent(tt.want)
			have := fmtCSP(rr.Header().Get("Content-Security-Policy"))
			if d := ztest.Diff(have, tt.want); d != "" {
				t.Error(d)
			}
		})
	}
}

func BenchmarkAddCSP(b *testing.B) {
	var (
		ctx = goatcounter.WithSite(context.Background(), &goatcounter.Site{})
		r   = ztest.NewRequest("GET", "/", nil).WithContext(ctx)
		rr  = httptest.NewRecorder()
		mw  = addcsp("")(http.NewServeMux())
	)
	b.ResetTimer()
	for b.Loop() {
		mw.ServeHTTP(rr, r)
	}
}

func BenchmarkAddCtx(b *testing.B) {
	b.Run("loadsite=false", func(b *testing.B) {
		var (
			ctx = goatcounter.WithSite(context.Background(), &goatcounter.Site{})
			r   = ztest.NewRequest("GET", "/", nil).WithContext(ctx)
			rr  = httptest.NewRecorder()
			mw  = addctx(nil, true, 10)(http.NewServeMux())
		)
		b.ResetTimer()
		for b.Loop() {
			mw.ServeHTTP(rr, r)
		}
	})

	b.Run("loadsite=true", func(b *testing.B) {
		var (
			ctx = testenv.DB(b)
			r   = ztest.NewRequest("GET", "/", nil).WithContext(ctx)
			rr  = httptest.NewRecorder()
			mw  = addctx(nil, true, 10)(http.NewServeMux())
		)
		r.Host = "testenv.localhost"
		b.ResetTimer()
		for b.Loop() {
			mw.ServeHTTP(rr, r)
		}
	})
}
