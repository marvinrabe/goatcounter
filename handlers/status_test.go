package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/marvinrabe/goatcounter"
	libsqldriver "github.com/marvinrabe/goatcounter/internal/dbdriver/libsql"
	"zgo.at/zdb"
)

func TestStatus(t *testing.T) {
	for _, base := range []string{"", "/stats"} {
		t.Run("base="+base, func(t *testing.T) {
			// A health check needs a reachable database, but no site or schema.
			db, err := libsqldriver.Open(context.Background(), zdb.ConnectOptions{
				Connect: libsqldriver.FileConnect(filepath.Join(t.TempDir(), "status.db")),
				Create:  true,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			ctx := goatcounter.NewContext(context.Background(), db)
			goatcounter.Config(ctx).BasePath = base
			router := NewBackend(db, false, "", base, 10, Ratelimits{}, "", Auth{Mode: AuthBasic})

			check := func(method string, code int, body string) {
				t.Helper()
				r := httptest.NewRequest(method, base+"/status?site=unknown", nil).WithContext(ctx)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				if w.Code != code || w.Body.String() != body {
					t.Errorf("%s /status: got %d %q; want %d %q", method, w.Code, w.Body.String(), code, body)
				}
				if got := w.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
					t.Errorf("Content-Type = %q", got)
				}
				if got := w.Header().Get("Cache-Control"); got != "no-store,no-cache" {
					t.Errorf("Cache-Control = %q", got)
				}
			}
			check(http.MethodGet, http.StatusOK, "OK")
			check(http.MethodHead, http.StatusOK, "")
			goatcounter.Config(ctx).Draining.Store(true)
			check(http.MethodGet, http.StatusServiceUnavailable, "draining\n")
			live := httptest.NewRecorder()
			router.ServeHTTP(live, httptest.NewRequest(http.MethodGet, base+"/live", nil).WithContext(ctx))
			if live.Code != http.StatusNotFound {
				t.Errorf("removed /live route = %d; want 404", live.Code)
			}
			goatcounter.Config(ctx).Draining.Store(false)

			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, base+"/status", nil).WithContext(ctx))
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("POST /status: got %d; want 405", w.Code)
			}

			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			check(http.MethodGet, http.StatusServiceUnavailable, "database unreachable\n")
		})
	}
}
