package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/testutil"
)

func TestStaticFiles(t *testing.T) {
	t.Chdir(testutil.ModuleRoot())
	for _, standalone := range []bool{false, true} {
		ctx := goatcounter.NewConfig(context.Background())
		// No database or configured sites: assets must be independent of both.
		var router chi.Router
		if standalone {
			router = NewStatic(chi.NewRouter())
		} else {
			router = NewBackend(nil, "", 10, Ratelimits{}, Auth{Mode: AuthBasic})
		}
		for _, tt := range []struct {
			path, body, cache string
		}{
			{"/robots.txt", "User-agent: *\nDisallow: /\n", "no-cache"},
			{"/security.txt", "Contact: support@goatcounter.com\n", "no-cache"},
			{"/count.js", "", "public, max-age=604800"},
		} {
			path := tt.path
			if standalone {
				path = tt.path // A separate static host also serves unprefixed URLs.
			}
			r := httptest.NewRequest(http.MethodGet, path+"?site=unknown", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != http.StatusOK {
				t.Errorf("standalone=%t %s: got %d; want 200", standalone, path, w.Code)
				continue
			}
			if tt.body != "" && w.Body.String() != tt.body {
				t.Errorf("%s: body = %q; want %q", path, w.Body.String(), tt.body)
			}
			if tt.body != "" && w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
				t.Errorf("%s: Content-Type = %q", path, w.Header().Get("Content-Type"))
			}
			if got := w.Header().Get("Cache-Control"); got != tt.cache {
				t.Errorf("%s: Cache-Control = %q; want %q", path, got, tt.cache)
			}
			if r.URL.Path != path {
				t.Errorf("original request path changed from %q to %q", path, r.URL.Path)
			}
			if tt.path == "/count.js" && w.Header().Get("Cross-Origin-Resource-Policy") != "cross-origin" {
				t.Error("count.js must allow cross-origin loading")
			}
		}
	}
}
