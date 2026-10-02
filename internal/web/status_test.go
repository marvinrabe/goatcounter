package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/marvinrabe/goatcounter/internal/analytics"
	"github.com/marvinrabe/goatcounter/internal/database"
)

func TestStatus(t *testing.T) {
	// A health check needs a reachable database, but no site or schema.
	db, err := database.Open(context.Background(), database.ConnectOptions{
		Connect: database.FileConnect(filepath.Join(t.TempDir(), "status.db")),
		Create:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store := &analytics.Store{DB: db}
	router := New(store, Ratelimits{}, Auth{Mode: AuthBasic})

	check := func(method string, code int, body string) {
		t.Helper()
		r := httptest.NewRequest(method, "/status?site=unknown", nil)
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
	live := httptest.NewRecorder()
	router.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/live", nil))
	if live.Code != http.StatusNotFound {
		t.Errorf("removed /live route = %d; want 404", live.Code)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/status", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /status: got %d; want 405", w.Code)
	}

	// A successful check is reused for a minute, so probes don't keep
	// the database busy; a new router has no recent check.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	check(http.MethodGet, http.StatusOK, "OK")
	router = New(store, Ratelimits{}, Auth{Mode: AuthBasic})
	check(http.MethodGet, http.StatusServiceUnavailable, "database unreachable\n")
}
