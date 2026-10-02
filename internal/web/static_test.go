package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marvinrabe/goatcounter/internal/analytics"
)

func TestStaticFiles(t *testing.T) {
	// No database or configured sites: assets must be independent of both.
	ctx := analytics.NewConfig(context.Background())
	router := New(nil, 10, Ratelimits{}, Auth{Mode: AuthBasic})
	for _, tt := range []struct {
		path, body, cache string
	}{
		{"/robots.txt", "User-agent: *\nDisallow: /\n", "no-cache"},
		{"/count.js", "", "public, max-age=604800"},
	} {
		r := httptest.NewRequest(http.MethodGet, tt.path+"?site=unknown", nil).WithContext(ctx)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("%s: got %d; want 200", tt.path, w.Code)
			continue
		}
		if tt.body != "" && w.Body.String() != tt.body {
			t.Errorf("%s: body = %q; want %q", tt.path, w.Body.String(), tt.body)
		}
		if tt.body != "" && w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
			t.Errorf("%s: Content-Type = %q", tt.path, w.Header().Get("Content-Type"))
		}
		if got := w.Header().Get("Cache-Control"); got != tt.cache {
			t.Errorf("%s: Cache-Control = %q; want %q", tt.path, got, tt.cache)
		}
		if tt.path == "/count.js" && w.Header().Get("Cross-Origin-Resource-Policy") != "cross-origin" {
			t.Error("count.js must allow cross-origin loading")
		}
	}
}
